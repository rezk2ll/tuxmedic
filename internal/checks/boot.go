package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Boot = doctor.Check{
	ID:       "boot",
	Name:     "Startup & services",
	Category: "System",
	Question: "Why does my computer take so long to start, or what failed at startup?",
	Run:      runBoot,
}

type unit struct {
	Unit        string `json:"unit"`
	Description string `json:"description"`
}

func runBoot(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !h.Systemd || !s.Has("systemctl") {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Startup analysis needs systemd, which this system doesn't use"}}
	}
	var fs []doctor.Finding
	fs = append(fs, failedUnits(ctx, s, false)...)
	fs = append(fs, failedUnits(ctx, s, true)...)
	fs = append(fs, bootTime(ctx, s)...)
	fs = append(fs, missingMounts(s)...)
	fs = append(fs, staleResume(s, h)...)
	if len(fs) == 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "Everything started correctly"})
	}
	return fs
}

func failedUnits(ctx context.Context, s sys.System, user bool) []doctor.Finding {
	args := []string{"--failed", "--output=json", "--no-pager", "--plain"}
	scope, journalFlag := "system", ""
	if user {
		args = append([]string{"--user"}, args...)
		scope, journalFlag = "your session's", "--user "
	}
	out, err := s.Run(ctx, "systemctl", args...)
	if err != nil {
		return nil
	}
	var us []unit
	if json.Unmarshal(out, &us) != nil || len(us) == 0 {
		return nil
	}
	names := make([]string, 0, len(us))
	for _, u := range us {
		names = append(names, firstNonEmpty(u.Description, u.Unit))
	}
	sctl := "systemctl "
	if user {
		sctl = "systemctl --user "
	}
	f := doctor.Finding{
		Status: doctor.Warn,
		Title:  fmt.Sprintf("%s failed to start: %s", plural(len(us), "background service", "background services"), strings.Join(lastN(names, 3), ", ")),
		Explain: fmt.Sprintf("These %s services stopped with an error. Often it's harmless (a feature you don't use), "+
			"but if something you rely on is missing, its log says why.", scope),
		Fixes: []doctor.Fix{
			{Title: "Read why " + us[0].Unit + " failed", Command: "journalctl " + journalFlag + "-b -u " + us[0].Unit + " -n 30 --no-pager"},
			{Title: "Try starting it again", Command: sctlSudo(user) + sctl + "restart " + us[0].Unit, NeedsRoot: !user},
		},
	}
	for _, u := range us {
		f.Evidence = append(f.Evidence, u.Unit+": "+u.Description)
	}
	return []doctor.Finding{f}
}

func sctlSudo(user bool) string {
	if user {
		return ""
	}
	return "sudo "
}

var analyzeRE = regexp.MustCompile(`([\d.]+(?:min|ms|s))\s+\((\w+)\)`)

func bootTime(ctx context.Context, s sys.System) []doctor.Finding {
	out, err := s.Run(ctx, "systemd-analyze", "time")
	if err != nil {
		return nil
	}
	parts := map[string]float64{}
	total := 0.0
	for _, m := range analyzeRE.FindAllStringSubmatch(string(out), -1) {
		sec := parseSpan(m[1])
		parts[m[2]] = sec
		total += sec
	}
	if total < 45 {
		return nil
	}
	f := doctor.Finding{
		Status: doctor.Warn,
		Title:  fmt.Sprintf("Startup takes %s", humanSeconds(total)),
		Explain: fmt.Sprintf("Firmware: %s, kernel: %s, services: %s. ",
			humanSeconds(parts["firmware"]+parts["loader"]), humanSeconds(parts["kernel"]+parts["initrd"]), humanSeconds(parts["userspace"])),
	}
	if parts["firmware"] > total/2 {
		f.Explain += "Most of the time passes before Linux even starts, in the computer's firmware. Turning on \"fast boot\" in the BIOS settings can help."
		return []doctor.Finding{f}
	}
	slow := slowUnits(ctx, s)
	if len(slow) > 0 {
		f.Explain += "The slowest services: " + strings.Join(slow, ", ") + "."
	}
	for _, u := range slow {
		if strings.HasPrefix(u, "NetworkManager-wait-online") || strings.HasPrefix(u, "systemd-networkd-wait-online") {
			f.Fixes = append(f.Fixes, doctor.Fix{
				Title:     "Don't wait for the network at startup (safe on desktops and laptops)",
				Command:   "sudo systemctl disable " + strings.Fields(u)[0],
				NeedsRoot: true,
			})
		}
	}
	f.Fixes = append(f.Fixes, doctor.Fix{Title: "See the full chain of what delayed startup", Command: "systemd-analyze critical-chain"})
	return []doctor.Finding{f}
}

func slowUnits(ctx context.Context, s sys.System) []string {
	out, err := s.Run(ctx, "systemd-analyze", "blame", "--no-pager")
	if err != nil {
		return nil
	}
	var slow []string
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		name := f[len(f)-1]
		sec := parseSpan(strings.Join(f[:len(f)-1], ""))
		if sec >= 5 {
			slow = append(slow, fmt.Sprintf("%s (%s)", strings.TrimSuffix(name, ".service"), humanSeconds(sec)))
		}
		if len(slow) == 3 {
			break
		}
	}
	return slow
}

// parseSpan reads systemd time spans like "1min 2.5s", "830ms", "12.1s".
var spanRE = regexp.MustCompile(`([\d.]+)(min|ms|s|h)`)

func parseSpan(s string) float64 {
	total := 0.0
	for _, m := range spanRE.FindAllStringSubmatch(s, -1) {
		v, _ := strconv.ParseFloat(m[1], 64)
		switch m[2] {
		case "h":
			total += v * 3600
		case "min":
			total += v * 60
		case "s":
			total += v
		case "ms":
			total += v / 1000
		}
	}
	return total
}

func humanSeconds(sec float64) string {
	if sec >= 60 {
		return fmt.Sprintf("%dmin %02.0fs", int(sec)/60, sec-float64(int(sec)/60*60))
	}
	if sec < 1 {
		return fmt.Sprintf("%.0f ms", sec*1000)
	}
	return fmt.Sprintf("%.1f s", sec)
}

// diskPath turns UUID=, PARTUUID= and LABEL= specs into their /dev/disk
// link, and returns "" for anything else.
func diskPath(spec string) string {
	for _, k := range []struct{ prefix, dir string }{{"UUID=", "by-uuid"}, {"PARTUUID=", "by-partuuid"}, {"LABEL=", "by-label"}} {
		if v, ok := strings.CutPrefix(spec, k.prefix); ok {
			return "/dev/disk/" + k.dir + "/" + strings.Trim(v, `"`)
		}
	}
	return ""
}

// staleResume catches a resume device (where hibernation is read back
// from) that no longer exists, usually a swap partition removed or
// reformatted. The initramfs waits for it at every startup.
func staleResume(s sys.System, h *doctor.Host) []doctor.Finding {
	for _, a := range strings.Fields(readTrim(s, "/proc/cmdline")) {
		v, ok := strings.CutPrefix(a, "resume=")
		if !ok || v == "none" || v == "" {
			continue
		}
		if p := diskPath(v); p != "" && !s.Exists(p) {
			fix := kernelArgRemoveFix(s, h, a)
			if strings.Contains(a, "/") {
				fix = doctor.Fix{Title: "Remove " + a + " from the kernel command line in your boot loader's settings, then restart"}
			}
			return []doctor.Finding{staleResumeFinding("kernel setting "+a, fix)}
		}
	}
	const conf = "/etc/initramfs-tools/conf.d/resume"
	if v, ok := strings.CutPrefix(readTrim(s, conf), "RESUME="); ok && v != "none" {
		if p := diskPath(v); p != "" && !s.Exists(p) {
			return []doctor.Finding{staleResumeFinding(conf+": RESUME="+v, doctor.Fix{
				Title:     "Stop waiting for it",
				Command:   "echo RESUME=none | sudo tee " + conf + " && sudo update-initramfs -u",
				NeedsRoot: true,
			})}
		}
	}
	return nil
}

func staleResumeFinding(evidence string, fix doctor.Fix) doctor.Finding {
	return doctor.Finding{
		Status: doctor.Warn,
		Title:  "Startup waits for a hibernation disk that isn't there",
		Explain: "The system is set to look for a saved hibernation on a swap disk that no longer exists, often after a reinstall or a resized partition. " +
			"It waits for that disk at every startup before giving up.",
		Fixes:    []doctor.Fix{fix},
		Evidence: []string{evidence},
	}
}

// missingMounts finds /etc/fstab entries pointing at disks that no longer
// exist, the classic cause of a 90-second "A start job is running" at boot.
func missingMounts(s sys.System) []doctor.Finding {
	b, err := s.ReadFile("/etc/fstab")
	if err != nil {
		return nil
	}
	var missing []string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || strings.HasPrefix(f[0], "#") || strings.Contains(f[3], "nofail") || strings.Contains(f[3], "noauto") {
			continue
		}
		if path := diskPath(f[0]); path != "" && !s.Exists(path) {
			missing = append(missing, fmt.Sprintf("%s (%s)", f[1], f[0]))
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Warn,
		Title:  "Startup waits for a disk that isn't there: " + strings.Join(missing, ", "),
		Explain: "/etc/fstab tells the system to mount a disk that no longer exists, often an old swap partition or a removed drive. " +
			"Each one can add a 90-second wait at startup, or drop you into emergency mode.",
		Fixes: []doctor.Fix{
			{Title: "Edit /etc/fstab and remove or comment out that line, or add nofail to its options", Command: "sudoedit /etc/fstab", NeedsRoot: true},
		},
		Evidence: missing,
	}}
}
