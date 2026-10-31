package checks

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Memory = doctor.Check{
	ID:       "memory",
	Name:     "Memory",
	Category: "Performance",
	Question: "Why is my computer slow or freezing when many apps are open?",
	Run:      runMemory,
}

func meminfo(s sys.System) map[string]uint64 {
	b, err := s.ReadFile("/proc/meminfo")
	if err != nil {
		return nil
	}
	m := map[string]uint64{}
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, _ := strconv.ParseUint(f[0], 10, 64)
		m[k] = n * 1024
	}
	return m
}

// pressure returns the share of the last 5 minutes (0 to 100) some task spent
// waiting on memory. It is the most direct measure of "the system feels stuck".
func pressure(s sys.System, res string) (float64, bool) {
	b, err := s.ReadFile("/proc/pressure/" + res)
	if err != nil {
		return 0, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "some ") {
			continue
		}
		for _, f := range strings.Fields(line) {
			if v, ok := strings.CutPrefix(f, "avg300="); ok {
				p, err := strconv.ParseFloat(v, 64)
				return p, err == nil
			}
		}
	}
	return 0, false
}

func runMemory(_ context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	m := meminfo(s)
	total, avail := m["MemTotal"], m["MemAvailable"]
	if total == 0 {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Couldn't read memory usage"}}
	}
	swapTotal, swapFree := m["SwapTotal"], m["SwapFree"]
	usedPct := 100 - float64(avail)/float64(total)*100
	var fs []doctor.Finding

	top := topMemory(s, 3)
	hogs := ""
	if len(top) > 0 {
		hogs = " The biggest users right now: " + strings.Join(top, ", ") + "."
	}
	psi, hasPSI := pressure(s, "memory")

	switch {
	case usedPct > 92 || hasPSI && psi > 10:
		fs = append(fs, doctor.Finding{
			Status: doctor.Fail,
			Title:  fmt.Sprintf("Memory is nearly full: %s of %s in use", humanBytes(total-avail), humanBytes(total)),
			Explain: "The system is spending time juggling memory instead of running your apps, which makes everything slow and can freeze the desktop." +
				hogs,
			Fixes: []doctor.Fix{{Title: "Close the biggest apps, or browser tabs you don't need"}},
		})
	case usedPct > 80:
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   fmt.Sprintf("Memory is getting full: %.0f%% in use", usedPct),
			Explain: "Opening more apps may make the system slow." + hogs,
		})
	default:
		fs = append(fs, doctor.Finding{
			Status:   doctor.OK,
			Title:    fmt.Sprintf("Plenty of free memory: %s of %s available", humanBytes(avail), humanBytes(total)),
			Evidence: top,
		})
	}
	if hasPSI && psi > 1 && psi <= 10 {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   fmt.Sprintf("Apps waited on memory %.0f%% of the last 5 minutes", psi),
			Explain: "This is when the system feels sluggish: programs pause while Linux frees up memory.",
		})
	}

	zram := zramDevices(s)
	switch {
	case swapTotal == 0 && len(zram) == 0 && total <= 16<<30:
		fs = append(fs, doctor.Finding{
			Status: doctor.Warn,
			Title:  "There is no swap or zram, so running out of memory freezes the system",
			Explain: "Swap gives Linux somewhere to put memory it isn't using right now. Without it, a busy moment can freeze the desktop instead of slowing it down. " +
				"zram is compressed swap in memory: fast and it doesn't need a disk partition.",
			Fixes: zramFixes(h),
		})
	case swapTotal > 0 && float64(swapFree)/float64(swapTotal) < 0.1:
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   fmt.Sprintf("Swap is %.0f%% full", 100-float64(swapFree)/float64(swapTotal)*100),
			Explain: "Once swap is full too, Linux has to force-close programs to free memory.",
		})
	}
	return fs
}

func zramDevices(s sys.System) []string {
	z, _ := s.Glob("/sys/block/zram*")
	var active []string
	for _, d := range z {
		if b, _ := s.ReadFile(d + "/disksize"); strings.TrimSpace(string(b)) != "0" && len(b) > 0 {
			active = append(active, d)
		}
	}
	return active
}

func zramFixes(h *doctor.Host) []doctor.Fix {
	switch h.PackageMgr {
	case "apt":
		return []doctor.Fix{{Title: "Turn on zram", Command: "sudo apt install zram-tools", NeedsRoot: true}}
	case "dnf":
		return []doctor.Fix{{Title: "Turn on zram", Command: "sudo dnf install zram-generator-defaults && sudo systemctl daemon-reload && sudo systemctl start /dev/zram0", NeedsRoot: true}}
	case "pacman":
		return []doctor.Fix{{Title: "Turn on zram", Command: "sudo pacman -S zram-generator", NeedsRoot: true, URL: "https://wiki.archlinux.org/title/Zram"}}
	}
	return []doctor.Fix{{Title: "Set up zram or a swap file", URL: "https://wiki.archlinux.org/title/Zram"}}
}

// topMemory lists the processes using the most memory, grouped by name so
// twenty browser processes show up as one browser.
func topMemory(s sys.System, n int) []string {
	files, _ := s.Glob("/proc/[0-9]*/status")
	usage := map[string]uint64{}
	for _, f := range files {
		b, err := s.ReadFile(f)
		if err != nil {
			continue
		}
		var name string
		var rss uint64
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "Name:"); ok {
				name = strings.TrimSpace(v)
			}
			if v, ok := strings.CutPrefix(line, "VmRSS:"); ok {
				fs := strings.Fields(v)
				if len(fs) > 0 {
					kb, _ := strconv.ParseUint(fs[0], 10, 64)
					rss = kb * 1024
				}
			}
		}
		if name != "" && rss > 0 {
			usage[appName(name)] += rss
		}
	}
	type kv struct {
		k string
		v uint64
	}
	var all []kv
	for k, v := range usage {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	var out []string
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, fmt.Sprintf("%s (%s)", all[i].k, humanBytes(all[i].v)))
	}
	return out
}

var appAliases = map[string]string{
	"chrome": "Google Chrome", "chromium": "Chromium", "firefox": "Firefox", "firefox-bin": "Firefox",
	"Isolated Web Co": "Firefox", "Web Content": "Firefox", "WebExtensions": "Firefox", "code": "VS Code",
	"electron": "Electron apps", "slack": "Slack", "Discord": "Discord", "thunderbird": "Thunderbird",
	"java": "Java apps", "qemu-system-x86": "Virtual machines", "gnome-shell": "GNOME Shell",
	"plasmashell": "KDE Plasma", "kwin_wayland": "KDE Plasma", "dockerd": "Docker", "containerd": "Docker",
}

func appName(comm string) string {
	if a, ok := appAliases[comm]; ok {
		return a
	}
	return comm
}
