package checks

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Printer = doctor.Check{
	ID:       "printer",
	Name:     "Printers",
	Category: "Devices",
	Question: "Why can't I print, or why doesn't my printer show up?",
	Run:      runPrinter,
}

type printer struct {
	name, state string // idle, printing, disabled
}

func runPrinter(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !s.Has("lpstat") {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Printing (CUPS) isn't installed on this computer"}}
	}
	if out, _ := s.Run(ctx, "lpstat", "-r"); !strings.Contains(string(out), "scheduler is running") {
		if h.Container {
			return []doctor.Finding{{Status: doctor.Skipped, Title: "No printing service in this container"}}
		}
		fix := doctor.Fix{Title: "Start it, and start it at every boot", Command: "sudo systemctl enable --now cups", NeedsRoot: true}
		if !h.Systemd {
			fix = doctor.Fix{Title: "Start it", Command: "sudo rc-service cupsd start", NeedsRoot: true}
			if h.PackageMgr == "xbps" {
				fix.Command = "sudo ln -s /etc/sv/cupsd /var/service/"
			}
		}
		return []doctor.Finding{{
			Status:  doctor.Warn,
			Title:   "The printing service isn't running",
			Explain: "Every printer goes through the CUPS service. While it is stopped, apps can't find printers and nothing prints.",
			Fixes:   []doctor.Fix{fix},
		}}
	}

	out, _ := s.Run(ctx, "lpstat", "-p")
	printers := parsePrinters(string(out))
	jobs, _ := s.Run(ctx, "lpstat", "-o")
	waiting := waitingJobs(string(jobs))

	if len(printers) == 0 {
		f := doctor.Finding{
			Status: doctor.Info,
			Title:  "No printer is set up",
			Explain: "Most printers from the last ten years print without installing a driver. Open your printer settings and add it; " +
				"network printers show up on their own when they're on the same network.",
		}
		if usb := ippUSBPrinters(s); len(usb) > 0 && !s.Has("ipp-usb") && !s.Exists("/usr/sbin/ipp-usb") {
			return []doctor.Finding{{
				Status: doctor.Warn,
				Title:  "Your USB printer needs ipp-usb to work without a driver: " + joinAnd(usb),
				Explain: "This printer speaks the same driverless printing protocol over USB as network printers do, but Linux only finds it that way through the small ipp-usb service. " +
					"Without it, the printer doesn't show up, or only with an old driver that may not work.",
				Fixes: []doctor.Fix{installFix(h, "ipp-usb"), {Title: "Then unplug the printer and plug it back in"}},
			}}
		}
		if found := discovered(ctx, s); len(found) > 0 {
			f.Title = "A printer on your network is ready to add: " + joinAnd(found)
			f.Explain = "It was found on your network and works without a driver. Add it in your printer settings, or print to it directly from the print dialog."
		} else if h.Systemd {
			if a, _ := s.Run(ctx, "systemctl", "is-active", "avahi-daemon"); strings.TrimSpace(string(a)) != "active" {
				f.Explain += " Finding network printers needs the Avahi service, which isn't running."
				f.Fixes = []doctor.Fix{{Title: "Start Avahi so network printers show up", Command: "sudo systemctl enable --now avahi-daemon", NeedsRoot: true}}
			}
		}
		return []doctor.Finding{f}
	}

	var fs []doctor.Finding
	var ready []string
	for _, p := range printers {
		n := waiting[p.name]
		switch {
		case p.state == "disabled":
			f := doctor.Finding{
				Status:  doctor.Warn,
				Title:   "The printer " + p.name + " is paused",
				Explain: "CUPS pauses a printer after an error, like the printer being off or out of reach when you printed. It stays paused until someone resumes it, even after the problem is gone.",
				Fixes:   []doctor.Fix{{Title: "Resume it", Command: "sudo cupsenable " + p.name, NeedsRoot: true}},
			}
			if n > 0 {
				f.Title += fmt.Sprintf(" with %s waiting", plural(n, "print job", "print jobs"))
			}
			fs = append(fs, f)
		case n > 0 && p.state != "printing":
			fs = append(fs, doctor.Finding{
				Status:  doctor.Warn,
				Title:   fmt.Sprintf("%s waiting for %s, which isn't printing", plural(n, "print job is", "print jobs are"), p.name),
				Explain: "A stuck job blocks the ones behind it. Check that the printer is on and connected, then cancel the jobs and print again.",
				Fixes:   []doctor.Fix{{Title: "Cancel the waiting jobs", Command: "cancel -a " + p.name}},
			})
		default:
			ready = append(ready, p.name)
		}
	}
	if len(ready) > 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "Ready to print: " + joinAnd(ready)})
	}
	return fs
}

// ippUSBPrinters names the plugged-in USB devices with an IPP-over-USB
// interface: class 7 (printer), subclass 1, protocol 4.
func ippUSBPrinters(s sys.System) []string {
	ifs, _ := s.Glob("/sys/bus/usb/devices/*:*/bInterfaceProtocol")
	var out []string
	for _, p := range ifs {
		d := strings.TrimSuffix(p, "/bInterfaceProtocol")
		if readTrim(s, p) != "04" || readTrim(s, d+"/bInterfaceClass") != "07" || readTrim(s, d+"/bInterfaceSubClass") != "01" {
			continue
		}
		dev, _, _ := strings.Cut(d, ":")
		name := strings.TrimSpace(readTrim(s, dev+"/manufacturer") + " " + readTrim(s, dev+"/product"))
		if name == "" {
			name = "a USB printer"
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// parsePrinters reads `lpstat -p` lines like
// "printer Office is idle.  enabled since ..." or "printer Office disabled since ...".
func parsePrinters(out string) []printer {
	var ps []printer
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "printer" {
			continue
		}
		p := printer{name: f[1], state: "idle"}
		switch {
		case strings.Contains(line, "disabled"):
			p.state = "disabled"
		case strings.Contains(line, "now printing"):
			p.state = "printing"
		}
		ps = append(ps, p)
	}
	return ps
}

// waitingJobs counts jobs per printer from `lpstat -o`, whose job IDs are
// "<printer>-<number>".
func waitingJobs(out string) map[string]int {
	n := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if i := strings.LastIndex(f[0], "-"); i > 0 {
			n[f[0][:i]]++
		}
	}
	return n
}

// discovered lists driverless printers CUPS sees on the network but that
// aren't set up yet.
func discovered(ctx context.Context, s sys.System) []string {
	out, err := s.Run(ctx, "lpstat", "-e")
	if err != nil {
		return nil
	}
	var names []string
	for _, n := range strings.Fields(string(out)) {
		names = append(names, n)
		if len(names) == 3 {
			break
		}
	}
	return names
}
