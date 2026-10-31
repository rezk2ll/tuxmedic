package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	var fs []doctor.Finding

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

	if len(fs) == 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "No pending restart and the package manager is healthy"})
	}
	return fs
}

// stagedDeployment reports whether rpm-ostree has a newer deployment than the
// booted one, which it lists first, and its version as " (41.20261030.0)".
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
