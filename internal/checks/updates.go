package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Updates = doctor.Check{
	ID:       "updates",
	Name:     "Updates & packages",
	Category: "System",
	Question: "Why won't updates install, or does my computer need a restart?",
	Run:      runUpdates,
}

func runUpdates(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	fs := endOfLife(s)

	modules := "/usr/lib/modules"
	if !s.Exists(modules) {
		modules = "/lib/modules"
	}
	if h.Kernel != "" && s.Exists(modules) && !s.Exists(modules+"/"+h.Kernel) && !h.Container {
		fs = append(fs, doctor.Finding{
			Status: doctor.Warn,
			Title:  "A new kernel was installed: restart to finish the update",
			Explain: "The kernel you're running was removed by the update. Until you restart, new USB devices, VPNs and some drivers can fail to load, " +
				"because their files now only exist for the new kernel.",
			Fixes: []doctor.Fix{{Title: "Restart the computer"}},
		})
	} else if reboot, pkgs := rebootRequired(ctx, s); reboot {
		f := doctor.Finding{
			Status:  doctor.Info,
			Title:   "Restart to finish installing updates",
			Explain: "Some updates, usually the kernel or core libraries, only take effect after a restart.",
		}
		if len(pkgs) > 0 {
			f.Title += " (" + strings.Join(lastN(pkgs, 3), ", ") + ")"
			f.Evidence = pkgs
		}
		fs = append(fs, f)
	}

	switch h.PackageMgr {
	case "apt":
		fs = append(fs, aptHealth(s)...)
	case "pacman":
		fs = append(fs, pacmanHealth(s)...)
	case "rpm-ostree":
		if v, ok := stagedDeployment(ctx, s); ok {
			fs = append(fs, doctor.Finding{
				Status:  doctor.Info,
				Title:   "An update is ready: restart to use it" + v,
				Explain: "This system installs updates as a complete new version next to the current one. Nothing changes until you restart, and the old version stays in the boot menu in case the new one has a problem.",
				Fixes:   []doctor.Fix{{Title: "Restart the computer"}},
			})
		}
	}

	if dirs, _ := s.Glob(modules + "/*"); len(dirs) > 6 && h.Like("debian", "ubuntu") {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Info,
			Title:   fmt.Sprintf("%d kernels are kept on disk", len(dirs)),
			Explain: "Old kernels pile up and fill the boot partition. You only need the current one and one spare.",
			Fixes:   []doctor.Fix{{Title: "Remove old kernels", Command: "sudo apt autoremove --purge", NeedsRoot: true}},
		})
	}

	if h.PackageMgr == "apt" {
		fs = append(fs, deadAptSources(s)...)
	}
	if h.PackageMgr == "pacman" {
		fs = append(fs, staleArch(s, h)...)
	}
	fs = append(fs, flathubOff(ctx, s)...)

	if len(fs) == 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "No pending restart and the package manager is healthy"})
	}
	return fs
}

// stagedDeployment reports whether rpm-ostree has a newer deployment than the
// booted one, which it lists first, and its version as " (41.20261030.0)".
// endOfLife reads the release dates distro-info-data ships on Ubuntu,
// Debian and their derivatives, so no date is hard-coded here. Mint,
// elementary and Pop!_OS follow the Ubuntu release in UBUNTU_CODENAME.
func endOfLife(s sys.System) []doctor.Finding {
	osr, err := s.ReadFile("/etc/os-release")
	if err != nil {
		return nil
	}
	kv := parseKV(string(osr))
	// SUPPORT_END is the standard os-release field; Fedora sets it.
	if end, err := time.Parse("2006-01-02", kv["SUPPORT_END"]); err == nil {
		if s.Now().Before(end) {
			return nil
		}
		return []doctor.Finding{{
			Status:   doctor.Warn,
			Title:    firstNonEmpty(kv["PRETTY_NAME"], kv["NAME"], "This release") + " no longer gets security updates",
			Explain:  eolExplain(end),
			Fixes:    []doctor.Fix{{Title: "Upgrade to the next release with your distro's upgrade tool"}},
			Evidence: []string{"/etc/os-release: SUPPORT_END=" + kv["SUPPORT_END"]},
		}}
	}
	table, series, name := "ubuntu", kv["UBUNTU_CODENAME"], "Ubuntu"
	if series == "" {
		if kv["ID"] != "debian" {
			return nil
		}
		table, series, name = "debian", kv["VERSION_CODENAME"], "Debian"
	}
	csv, err := s.ReadFile("/usr/share/distro-info/" + table + ".csv")
	if err != nil || series == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(csv)), "\n")
	cols := strings.Split(lines[0], ",")
	for _, l := range lines[1:] {
		row := strings.Split(l, ",")
		if len(row) < 6 || row[2] != series {
			continue
		}
		get := func(col string) time.Time {
			if i := slices.Index(cols, col); i >= 0 && i < len(row) {
				t, _ := time.Parse("2006-01-02", row[i])
				return t
			}
			return time.Time{}
		}
		end, extended, extName := get("eol"), get("eol-esm"), "Ubuntu Pro (free for personal use)"
		if table == "debian" {
			extended, extName = get("eol-lts"), "Debian LTS"
		}
		now := s.Now()
		if end.IsZero() || now.Before(end) {
			return nil
		}
		release := name + " " + row[0] + " (" + row[1] + ")"
		f := doctor.Finding{
			Status:   doctor.Warn,
			Title:    release + " no longer gets security updates",
			Explain:  eolExplain(end),
			Fixes:    []doctor.Fix{{Title: "Upgrade to the next release", URL: map[string]string{"ubuntu": "https://help.ubuntu.com/community/UpgradeNotes", "debian": "https://www.debian.org/releases/"}[table]}},
			Evidence: []string{"/usr/share/distro-info/" + table + ".csv: " + l},
		}
		if now.Before(extended) {
			f.Status = doctor.Info
			f.Title = release + " is past its normal support"
			f.Explain = "Its regular updates ended on " + end.Format("2 January 2006") + ". " + extName + " keeps it patched until " +
				extended.Format("2 January 2006") + ", but plan the upgrade to a newer release before then."
		}
		return []doctor.Finding{f}
	}
	return nil
}

func eolExplain(end time.Time) string {
	return "Its support ended on " + end.Format("2 January 2006") + ". Security holes found since then stay open, and new apps " +
		"and drivers increasingly don't install. Upgrading to a newer release keeps your files and apps."
}

// flathubOff catches a Flatpak setup where Flathub, where nearly all
// Flatpak apps live, is missing, switched off, or filtered down to a short
// list (Fedora's default before it opened up).
func flathubOff(ctx context.Context, s sys.System) []doctor.Finding {
	if !s.Has("flatpak") {
		return nil
	}
	out, err := s.Run(ctx, "flatpak", "remotes", "--columns=name,options")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var opts []string
	found := false
	for _, l := range lines {
		name, o, _ := strings.Cut(l, "\t")
		if name != "flathub" {
			continue
		}
		found = true
		if !strings.Contains(o, "disabled") && !strings.Contains(o, "filtered") {
			return nil
		}
		opts = append(opts, o)
	}
	const add = "flatpak remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo"
	if !found {
		if len(lines) == 0 || lines[0] == "" {
			return nil
		}
		return []doctor.Finding{{
			Status:   doctor.Info,
			Title:    "Flatpak is set up without Flathub, where most Flatpak apps are",
			Explain:  "Your software center only shows apps from the sources listed below. Flathub has thousands more, including many popular apps.",
			Fixes:    []doctor.Fix{{Title: "Add Flathub", Command: add}},
			Evidence: lines,
		}}
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  "Flathub is switched off or limited to a short list of apps",
		Explain: "Flathub is where most Flatpak apps are, but here it's disabled or filtered, so your software center " +
			"shows only a few of them and some apps you look for aren't there.",
		Fixes:    []doctor.Fix{{Title: "Turn on all of Flathub", Command: "sudo flatpak remote-modify --enable --no-filter flathub", NeedsRoot: true}},
		Evidence: []string{"flathub: " + strings.Join(opts, "; ")},
	}}
}

// staleArch catches an Arch system that hasn't had a full upgrade in months.
// Its keyring is then older than the keys that signed today's packages, and
// the next upgrade fails with "signature is unknown trust".
func staleArch(s sys.System, h *doctor.Host) []doctor.Finding {
	var last time.Time
	for _, l := range strings.Split(readTrim(s, "/var/log/pacman.log"), "\n") {
		if !strings.Contains(l, "starting full system upgrade") || !strings.HasPrefix(l, "[") {
			continue
		}
		stamp, _, _ := strings.Cut(l[1:], "]")
		for _, layout := range []string{"2006-01-02T15:04:05-0700", "2006-01-02 15:04"} {
			if t, err := time.Parse(layout, stamp); err == nil {
				last = t
				break
			}
		}
	}
	if last.IsZero() {
		return nil
	}
	days := int(s.Now().Sub(last).Hours() / 24)
	if days < 90 {
		return nil
	}
	keyrings := "archlinux-keyring"
	switch {
	case h.ID == "manjaro" || h.Like("manjaro"):
		keyrings += " manjaro-keyring"
	case h.ID == "endeavouros":
		keyrings += " endeavouros-keyring"
	case h.ID == "cachyos":
		keyrings += " cachyos-keyring"
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  fmt.Sprintf("This system hasn't been fully updated in %d days", days),
		Explain: "Arch-based systems move fast. After a long gap the next update often stops with \"signature is unknown trust\" or " +
			"\"invalid or corrupted package\", because the list of trusted signing keys is older than the packages. Updating that list first avoids it.",
		Fixes: []doctor.Fix{
			{Title: "Update the signing keys first, then everything", Command: "sudo pacman -Sy --needed " + keyrings + " && sudo pacman -Su", NeedsRoot: true},
			{Title: "Read the news for steps needed by updates you skipped", URL: "https://archlinux.org/news/"},
		},
		Evidence: []string{"last full upgrade: " + last.Format("2006-01-02")},
	}}
}

func stagedDeployment(ctx context.Context, s sys.System) (string, bool) {
	out, err := s.Run(ctx, "rpm-ostree", "status", "--json")
	var st struct {
		Deployments []struct {
			Booted  bool   `json:"booted"`
			Version string `json:"version"`
		} `json:"deployments"`
	}
	if err != nil || json.Unmarshal(out, &st) != nil || len(st.Deployments) < 2 || st.Deployments[0].Booted {
		return "", false
	}
	if v := st.Deployments[0].Version; v != "" {
		return " (" + v + ")", true
	}
	return "", true
}

func rebootRequired(ctx context.Context, s sys.System) (bool, []string) {
	if s.Exists("/run/reboot-required") || s.Exists("/var/run/reboot-required") {
		var pkgs []string
		for _, p := range []string{"/run/reboot-required.pkgs", "/var/run/reboot-required.pkgs"} {
			if b, err := s.ReadFile(p); err == nil {
				for _, l := range strings.Fields(string(b)) {
					pkgs = appendUnique(pkgs, l)
				}
				break
			}
		}
		return true, pkgs
	}
	if s.Has("needs-restarting") {
		// Exit status 1 means a reboot is needed.
		if _, err := s.Run(ctx, "needs-restarting", "-r"); err != nil && !strings.Contains(err.Error(), "not found") {
			return true, nil
		}
	}
	return false, nil
}

func aptHealth(s sys.System) []doctor.Finding {
	var fs []doctor.Finding
	if pending, _ := s.Glob("/var/lib/dpkg/updates/*"); len(pending) > 0 {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Fail,
			Title:   "An update was interrupted halfway",
			Explain: "Package installation stopped before it finished, maybe because of a crash or the computer switching off. Until it's completed, no other update can install.",
			Fixes:   []doctor.Fix{{Title: "Finish the interrupted update", Command: "sudo dpkg --configure -a", NeedsRoot: true}},
		})
	}
	if holder := running(s, "apt", "apt-get", "dpkg", "unattended-upgr", "aptd", "packagekitd"); holder != "" {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Info,
			Title:   "Another update is running right now (" + holder + ")",
			Explain: "If you see \"Could not get lock\", this is why: only one program can install packages at a time. Wait for it to finish. Don't delete the lock files.",
		})
	}
	return fs
}

func pacmanHealth(s sys.System) []doctor.Finding {
	if !s.Exists("/var/lib/pacman/db.lck") || running(s, "pacman", "yay", "paru", "pamac", "packagekitd") != "" {
		return nil
	}
	return []doctor.Finding{{
		Status:  doctor.Fail,
		Title:   "pacman is locked by an update that's no longer running",
		Explain: "A previous pacman run was interrupted and left its lock file behind, so every new update fails with \"unable to lock database\".",
		Fixes:   []doctor.Fix{{Title: "Remove the stale lock", Command: "sudo rm /var/lib/pacman/db.lck", NeedsRoot: true}},
	}}
}

// running returns the first of the named processes that is alive.
func running(s sys.System, names ...string) string {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	comms, _ := s.Glob("/proc/[0-9]*/comm")
	for _, c := range comms {
		n := readTrim(s, c)
		if !want[n] {
			continue
		}
		// Ubuntu keeps unattended-upgrade-shutdown running all the time; it
		// only waits for shutdown and holds no lock.
		if cmd, _ := s.ReadFile(strings.TrimSuffix(c, "comm") + "cmdline"); strings.Contains(string(cmd), "unattended-upgrade-shutdown") {
			continue
		}
		return n
	}
	return ""
}

func appendUnique(ss []string, v string) []string {
	for _, s := range ss {
		if s == v {
			return ss
		}
	}
	return append(ss, v)
}
