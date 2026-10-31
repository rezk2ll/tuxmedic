package checks

import (
	"context"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Games = doctor.Check{
	ID:       "games",
	Name:     "Games & Steam",
	Category: "Performance",
	Question: "Why won't my Steam games start, or my controller work?",
	Run:      runGames,
}

func gpuDriver(s sys.System) string {
	cards, _ := s.Glob("/sys/class/drm/card[0-9]*/device/uevent")
	drv := ""
	for _, c := range cards {
		b, _ := s.ReadFile(c)
		d := parseKV(string(b))["DRIVER"]
		// A dedicated card wins over the integrated one.
		if d == "nvidia" || d == "amdgpu" && drv != "nvidia" || drv == "" {
			drv = d
		}
	}
	return drv
}

func runGames(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	native := s.Has("steam")
	flat := false
	if s.Has("flatpak") {
		out, _ := s.Run(ctx, "flatpak", "list", "--app", "--columns=application")
		flat = strings.Contains(string(out), "com.valvesoftware.Steam")
	}
	if !native && !flat {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Steam isn't installed"}}
	}
	gpu := gpuDriver(s)
	var fs []doctor.Finding

	if native {
		icds, _ := s.Glob("/usr/share/vulkan/icd.d/*.json")
		etc, _ := s.Glob("/etc/vulkan/icd.d/*.json")
		if len(icds)+len(etc) == 0 {
			fs = append(fs, doctor.Finding{
				Status:  doctor.Fail,
				Title:   "There's no Vulkan driver, so most Windows games won't start",
				Explain: "Steam runs Windows games through Proton, which needs Vulkan. Without a Vulkan driver for your graphics card, those games quit right away or never open.",
				Fixes:   []doctor.Fix{vulkanFix(h, gpu, false)},
			})
		} else if !has32bitDriver(s) {
			fs = append(fs, doctor.Finding{
				Status:  doctor.Warn,
				Title:   "Steam is missing the 32-bit graphics driver",
				Explain: "Steam itself and many games are 32-bit programs, and they need the 32-bit version of your graphics driver next to the normal one. Without it Steam can fail to open, or games start with a black window.",
				Fixes:   []doctor.Fix{vulkanFix(h, gpu, true)},
			})
		}
	}

	if flat && !steamRules(s) {
		f := doctor.Finding{
			Status:  doctor.Info,
			Title:   "If Steam doesn't see your controller: the controller rules aren't installed",
			Explain: "Steam from Flathub can't add the rules that let it use controllers and VR headsets. They have to be installed on the system, in a package usually called steam-devices.",
			Fixes:   []doctor.Fix{installFix(h, "steam-devices")},
		}
		if h.PackageMgr == "pacman" {
			f.Fixes = []doctor.Fix{{Title: "Install the game-devices-udev rules from the AUR", URL: "https://aur.archlinux.org/packages/game-devices-udev"}}
		}
		fs = append(fs, f)
	}

	if len(fs) == 0 {
		how := "Steam"
		if flat && !native {
			how = "Steam (Flatpak)"
		}
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: how + " has the drivers it needs"})
	}
	return fs
}

// has32bitDriver looks for a 32-bit Vulkan or NVIDIA GL driver in the
// directories Debian, Arch and Fedora/openSUSE keep 32-bit libraries in.
func has32bitDriver(s sys.System) bool {
	dirs := []string{"/usr/lib/i386-linux-gnu", "/usr/lib32"}
	if s.Exists("/usr/lib64") {
		dirs = append(dirs, "/usr/lib")
	}
	for _, d := range dirs {
		for _, p := range []string{"/libvulkan_*.so", "/libGLX_nvidia.so.0"} {
			if hits, _ := s.Glob(d + p); len(hits) > 0 {
				return true
			}
		}
	}
	return false
}

func steamRules(s sys.System) bool {
	for _, d := range []string{"/usr/lib/udev/rules.d", "/lib/udev/rules.d", "/etc/udev/rules.d"} {
		if hits, _ := s.Glob(d + "/*steam*"); len(hits) > 0 {
			return true
		}
	}
	return false
}

func vulkanFix(h *doctor.Host, gpu string, lib32 bool) doctor.Fix {
	title := "Install the Vulkan driver"
	if lib32 {
		title = "Install the 32-bit graphics driver"
	}
	if gpu == "nvidia" {
		return doctor.Fix{Title: title + ": the 32-bit package of your NVIDIA driver (on Arch lib32-nvidia-utils, on Debian and Ubuntu the :i386 package of libnvidia-gl)"}
	}
	switch h.PackageMgr {
	case "apt":
		if lib32 {
			return doctor.Fix{Title: title, Command: "sudo dpkg --add-architecture i386 && sudo apt update && sudo apt install mesa-vulkan-drivers:i386 libgl1-mesa-dri:i386", NeedsRoot: true}
		}
		return doctor.Fix{Title: title, Command: "sudo apt install mesa-vulkan-drivers", NeedsRoot: true}
	case "dnf":
		if lib32 {
			return doctor.Fix{Title: title, Command: "sudo dnf install mesa-vulkan-drivers.i686 mesa-dri-drivers.i686", NeedsRoot: true}
		}
		return doctor.Fix{Title: title, Command: "sudo dnf install mesa-vulkan-drivers", NeedsRoot: true}
	case "pacman":
		pkg := "vulkan-radeon"
		if gpu == "i915" || gpu == "xe" {
			pkg = "vulkan-intel"
		}
		if lib32 {
			return doctor.Fix{Title: title + " (turn on the multilib repository in /etc/pacman.conf first)", Command: "sudo pacman -Syu lib32-" + pkg, NeedsRoot: true, URL: "https://wiki.archlinux.org/title/Official_repositories#multilib"}
		}
		return doctor.Fix{Title: title, Command: "sudo pacman -S " + pkg, NeedsRoot: true}
	}
	return doctor.Fix{Title: title + " for your graphics card with your package manager"}
}
