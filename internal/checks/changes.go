package checks

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Changes = doctor.Check{
	ID:       "changes",
	Name:     "Recent changes",
	Category: "System",
	Question: "It worked last week. What changed since then?",
	Run:      runChanges,
}

const changeWindow = 14 * 24 * time.Hour

type pkgChange struct {
	pkg, from, to string
	at            time.Time
}

type changeArea struct {
	label    string // "The Linux kernel"
	symptoms string
	match    func(pkg string) bool
}

func prefixes(ps ...string) func(string) bool {
	return func(n string) bool {
		for _, p := range ps {
			if strings.HasPrefix(n, p) {
				return true
			}
		}
		return false
	}
}

// areas are the parts of the system whose updates most often break a desktop
// that worked the day before, in the order they are worth suspecting.
var areas = []changeArea{
	{"The Linux kernel", "Wi-Fi, sleep, sound, the screen or the touchpad",
		func(n string) bool {
			return n == "linux" || n == "kernel" || prefixes("linux-image", "linux-lts", "linux-zen", "linux-hardened", "kernel-core", "kernel-default")(n)
		}},
	{"Graphics drivers", "a flickering or black screen, crashing games or a slow desktop",
		func(n string) bool {
			return strings.Contains(n, "nvidia") || prefixes("mesa", "libgl1-mesa", "xserver-xorg-video", "vulkan-", "libdrm")(n)
		}},
	{"Device firmware", "Wi-Fi, Bluetooth, graphics or sleep",
		func(n string) bool {
			return strings.Contains(n, "firmware") || strings.Contains(n, "microcode") || strings.HasSuffix(n, "-ucode")
		}},
	{"The sound system", "no sound or a missing microphone",
		prefixes("pipewire", "wireplumber", "pulseaudio", "alsa-ucm", "alsa-lib", "libasound")},
	{"Networking", "Wi-Fi or VPN connections",
		prefixes("network-manager", "networkmanager", "wpasupplicant", "wpa_supplicant", "iwd")},
	{"Bluetooth", "devices that won't pair or connect", prefixes("bluez")},
	{"The desktop", "login problems, a frozen desktop or display settings",
		prefixes("gnome-shell", "mutter", "plasma-workspace", "kwin", "xorg-server", "xserver-xorg-core", "xwayland", "gdm", "sddm", "lightdm")},
	{"systemd", "startup, sleep or login", func(n string) bool { return n == "systemd" }},
}

func runChanges(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	changes, ok := packageHistory(ctx, s, h)
	if !ok {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "tuxmedic can't read the update history of " + pmName(h) + " yet"}}
	}
	now := s.Now()
	var recent []pkgChange
	for _, c := range changes {
		if now.Sub(c.at) <= changeWindow && !c.at.After(now.Add(time.Hour)) {
			recent = append(recent, c)
		}
	}
	if len(recent) == 0 {
		return []doctor.Finding{{Status: doctor.OK, Title: "Nothing was installed or updated in the last 2 weeks"}}
	}
	sort.SliceStable(recent, func(i, j int) bool { return recent[i].at.After(recent[j].at) })

	type areaHits struct {
		a      changeArea
		hits   []pkgChange
		latest pkgChange
	}
	var found []areaHits
	for _, a := range areas {
		var hits []pkgChange
		for _, c := range recent {
			// Removing an old kernel is routine cleanup, not a change to the running system.
			if a.label == "The Linux kernel" && c.to == "" {
				continue
			}
			if a.match(strings.ToLower(c.pkg)) {
				hits = append(hits, c)
			}
		}
		if len(hits) == 0 {
			continue
		}
		latest := hits[0]
		for _, c := range hits {
			if c.at.Equal(latest.at) && c.to != "" && (latest.to == "" || len(c.pkg) < len(latest.pkg)) {
				latest = c
			}
		}
		found = append(found, areaHits{a, hits, latest})
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].latest.at.After(found[j].latest.at) })

	var fs []doctor.Finding
	for _, x := range found[:min(3, len(found))] {
		a, hits, latest := x.a, x.hits, x.latest
		f := doctor.Finding{
			Status:   doctor.Info,
			Title:    fmt.Sprintf("%s changed %s (%s)", a.label, when(latest.at, now), describeChange(latest)),
			Explain:  "If you've had trouble with " + a.symptoms + " since then, this update is the first thing to suspect.",
			Evidence: changeLines(hits, now),
		}
		if fx, ok := undoFix(h, a, latest); ok {
			f.Fixes = []doctor.Fix{fx}
		}
		fs = append(fs, f)
	}
	fs = append(fs, doctor.Finding{
		Status:   doctor.OK,
		Title:    fmt.Sprintf("%s installed, updated or removed in the last 2 weeks", plural(len(recent), "package", "packages")),
		Evidence: changeLines(recent, now),
	})
	return fs
}

func pmName(h *doctor.Host) string {
	if h.PackageMgr == "" {
		return "this system"
	}
	return h.PackageMgr
}

func describeChange(c pkgChange) string {
	switch {
	case c.from != "" && c.to != "":
		return fmt.Sprintf("%s %s → %s", c.pkg, c.from, c.to)
	case c.to != "":
		return c.pkg + " " + c.to
	case c.from != "":
		return c.pkg + " removed"
	}
	return c.pkg
}

func changeLines(cs []pkgChange, now time.Time) []string {
	out := make([]string, 0, min(len(cs), 12))
	for i, c := range cs {
		if i == 12 {
			out = append(out, fmt.Sprintf("… and %d more", len(cs)-12))
			break
		}
		out = append(out, when(c.at, now)+": "+describeChange(c))
	}
	return out
}

func undoFix(h *doctor.Host, a changeArea, c pkgChange) (doctor.Fix, bool) {
	if a.label == "The Linux kernel" && h.PackageMgr != "rpm-ostree" {
		return doctor.Fix{Title: "To test the previous kernel: restart, open the boot menu (hold Shift or press Esc), choose Advanced options and pick the older version"}, true
	}
	return downgradeFix(h, c)
}

// crashAreas are the areas whose updates can take the whole computer down.
var crashAreas = map[string]bool{
	"The Linux kernel": true, "Graphics drivers": true, "Device firmware": true, "The desktop": true, "systemd": true,
}

// suspectUpdate finds the update most likely to have started crashes first
// seen at first: the latest install or upgrade in a crash-prone area in the
// three days before.
func suspectUpdate(changes []pkgChange, first time.Time) (changeArea, pkgChange, bool) {
	var best pkgChange
	var bestArea changeArea
	found := false
	for _, c := range changes {
		if c.to == "" || !c.at.Before(first) || first.Sub(c.at) > 72*time.Hour {
			continue
		}
		if found && (c.at.Before(best.at) || c.at.Equal(best.at) && len(c.pkg) >= len(best.pkg)) {
			continue
		}
		for _, a := range areas {
			if crashAreas[a.label] && a.match(strings.ToLower(c.pkg)) {
				best, bestArea, found = c, a, true
				break
			}
		}
	}
	return bestArea, best, found
}

func downgradeFix(h *doctor.Host, c pkgChange) (doctor.Fix, bool) {
	switch h.PackageMgr {
	case "dnf":
		return doctor.Fix{Title: "Go back to the previous version of " + c.pkg, Command: "sudo dnf downgrade " + c.pkg, NeedsRoot: true}, true
	case "rpm-ostree":
		return doctor.Fix{Title: "Go back to the whole system as it was before the last update, then restart", Command: "sudo rpm-ostree rollback", NeedsRoot: true}, true
	case "transactional-update":
		return doctor.Fix{Title: "Restart and pick the previous snapshot in the boot menu; if it works, make it stay with transactional-update rollback"}, true
	case "pacman":
		if c.from != "" {
			return doctor.Fix{
				Title:     "Go back to " + c.pkg + " " + c.from + " from the package cache",
				Command:   fmt.Sprintf("sudo pacman -U /var/cache/pacman/pkg/%s-%s-*.pkg.tar.zst", c.pkg, c.from),
				NeedsRoot: true,
			}, true
		}
	case "apt":
		if c.from != "" {
			return doctor.Fix{
				Title:     "Go back to " + c.pkg + " " + c.from + ", if your distro still offers it",
				Command:   fmt.Sprintf("sudo apt install %s=%s", c.pkg, c.from),
				NeedsRoot: true,
			}, true
		}
	}
	return doctor.Fix{}, false
}

func packageHistory(ctx context.Context, s sys.System, h *doctor.Host) ([]pkgChange, bool) {
	switch {
	case h.PackageMgr == "apt" || s.Exists("/var/log/apt/history.log"):
		b, err := s.ReadFile("/var/log/apt/history.log")
		if err != nil {
			return nil, false
		}
		return parseAptHistory(string(b)), true
	case h.PackageMgr == "pacman" || s.Exists("/var/log/pacman.log"):
		b, err := s.ReadFile("/var/log/pacman.log")
		if err != nil {
			return nil, false
		}
		return parsePacmanLog(string(b)), true
	case s.Has("rpm"):
		out, err := s.Run(ctx, "rpm", "-qa", "--last")
		if err != nil {
			return nil, false
		}
		return parseRPMLast(string(out)), true
	}
	return nil, false
}

var aptPkgRE = regexp.MustCompile(`([A-Za-z0-9][A-Za-z0-9.+\-]*)(?::[a-z0-9]+)? \(([^)]*)\)`)

func parseAptHistory(log string) []pkgChange {
	var out []pkgChange
	for _, block := range strings.Split(log, "\n\n") {
		var at time.Time
		for _, line := range strings.Split(block, "\n") {
			key, val, ok := strings.Cut(line, ": ")
			if !ok {
				continue
			}
			switch key {
			case "Start-Date":
				at, _ = time.ParseInLocation("2006-01-02  15:04:05", strings.TrimSpace(val), time.Local)
			case "Install", "Upgrade", "Downgrade", "Remove", "Purge", "Reinstall":
				for _, m := range aptPkgRE.FindAllStringSubmatch(val, -1) {
					vs := strings.Split(m[2], ", ")
					c := pkgChange{pkg: m[1], at: at}
					switch key {
					case "Upgrade", "Downgrade":
						c.from = vs[0]
						if len(vs) > 1 {
							c.to = vs[1]
						}
					case "Remove", "Purge":
						c.from = vs[0]
					default:
						c.to = vs[0]
					}
					out = append(out, c)
				}
			}
		}
	}
	return out
}

var pacmanRE = regexp.MustCompile(`^\[([^\]]+)\] \[ALPM\] (installed|upgraded|downgraded|removed|reinstalled) (\S+) \(([^)]*)\)`)

func parsePacmanLog(log string) []pkgChange {
	var out []pkgChange
	for _, line := range strings.Split(log, "\n") {
		m := pacmanRE.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		at, err := time.Parse("2006-01-02T15:04:05-0700", m[1])
		if err != nil {
			at, _ = time.ParseInLocation("2006-01-02 15:04", m[1], time.Local)
		}
		c := pkgChange{pkg: m[3], at: at}
		switch m[2] {
		case "upgraded", "downgraded":
			c.from, c.to, _ = strings.Cut(m[4], " -> ")
		case "removed":
			c.from = m[4]
		default:
			c.to = m[4]
		}
		out = append(out, c)
	}
	return out
}

var spacesRE = regexp.MustCompile(`\s{2,}`)

// parseRPMLast reads `rpm -qa --last`: "name-version-release.arch  Tue Aug 25 06:47:40 2026".
func parseRPMLast(out string) []pkgChange {
	var cs []pkgChange
	for _, line := range strings.Split(out, "\n") {
		f := spacesRE.Split(strings.TrimSpace(line), 2)
		if len(f) != 2 {
			continue
		}
		at, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", f[1], time.Local)
		if err != nil {
			continue
		}
		nevra := f[0]
		parts := strings.Split(nevra, "-")
		if len(parts) < 3 || strings.HasPrefix(nevra, "gpg-pubkey") {
			continue
		}
		name := strings.Join(parts[:len(parts)-2], "-")
		ver := parts[len(parts)-2] + "-" + strings.TrimSuffix(parts[len(parts)-1], archSuffix(parts[len(parts)-1]))
		cs = append(cs, pkgChange{pkg: name, to: ver, at: at})
	}
	return cs
}

func archSuffix(rel string) string {
	if i := strings.LastIndex(rel, "."); i >= 0 {
		return rel[i:]
	}
	return ""
}
