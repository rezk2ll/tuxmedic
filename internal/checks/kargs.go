package checks

import (
	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

// kernelArgFix adds a kernel parameter with the tool this system's boot setup
// uses. Every way ends with a restart, so the title says so.
func kernelArgFix(s sys.System, h *doctor.Host, arg string) doctor.Fix {
	title := "Add the kernel setting " + arg + ", then restart"
	cmd := ""
	switch {
	case h.PackageMgr == "rpm-ostree":
		cmd = "sudo rpm-ostree kargs --append=" + arg
	case h.PackageMgr == "nix":
		return doctor.Fix{Title: `Add "` + arg + `" to boot.kernelParams in your NixOS configuration, rebuild and restart`}
	case s.Has("grubby"):
		cmd = "sudo grubby --update-kernel=ALL --args=" + arg
	case s.Has("kernelstub"):
		cmd = "sudo kernelstub -a " + arg
	case s.Has("update-grub"):
		cmd = `sudo sed -i 's/^GRUB_CMDLINE_LINUX_DEFAULT="/&` + arg + ` /' /etc/default/grub && sudo update-grub`
	case s.Has("grub-mkconfig"):
		cmd = `sudo sed -i 's/^GRUB_CMDLINE_LINUX_DEFAULT="/&` + arg + ` /' /etc/default/grub && sudo grub-mkconfig -o /boot/grub/grub.cfg`
	case s.Has("grub2-mkconfig"):
		cmd = `sudo sed -i 's/^GRUB_CMDLINE_LINUX_DEFAULT="/&` + arg + ` /' /etc/default/grub && sudo grub2-mkconfig -o /boot/grub2/grub.cfg`
	case s.Exists("/etc/kernel/cmdline"):
		return doctor.Fix{Title: "Add " + arg + " to /etc/kernel/cmdline, run sudo kernel-install add-all (or your distro's equivalent) and restart"}
	default:
		return doctor.Fix{Title: "Add " + arg + " to the kernel command line in your boot loader's settings, then restart", URL: "https://wiki.archlinux.org/title/Kernel_parameters"}
	}
	return doctor.Fix{Title: title, Command: cmd, NeedsRoot: true}
}

func kernelArgRemoveFix(s sys.System, h *doctor.Host, arg string) doctor.Fix {
	title := "Remove the kernel setting " + arg + ", then restart"
	grub := `sudo sed -i '/^GRUB_CMDLINE_LINUX/s/ *\b` + arg + `\b//' /etc/default/grub && `
	cmd := ""
	switch {
	case h.PackageMgr == "rpm-ostree":
		cmd = "sudo rpm-ostree kargs --delete=" + arg
	case h.PackageMgr == "nix":
		return doctor.Fix{Title: `Remove "` + arg + `" from boot.kernelParams in your NixOS configuration, rebuild and restart`}
	case s.Has("grubby"):
		cmd = "sudo grubby --update-kernel=ALL --remove-args=" + arg
	case s.Has("kernelstub"):
		cmd = "sudo kernelstub -d " + arg
	case s.Has("update-grub"):
		cmd = grub + "sudo update-grub"
	case s.Has("grub-mkconfig"):
		cmd = grub + "sudo grub-mkconfig -o /boot/grub/grub.cfg"
	case s.Has("grub2-mkconfig"):
		cmd = grub + "sudo grub2-mkconfig -o /boot/grub2/grub.cfg"
	default:
		return doctor.Fix{Title: "Remove " + arg + " from the kernel command line in your boot loader's settings, then restart", URL: "https://wiki.archlinux.org/title/Kernel_parameters"}
	}
	return doctor.Fix{Title: title, Command: cmd, NeedsRoot: true}
}
