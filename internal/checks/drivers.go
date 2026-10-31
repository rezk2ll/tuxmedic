package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Drivers = doctor.Check{
	ID:       "drivers",
	Name:     "Drivers & graphics",
	Category: "Devices",
	Question: "Is some hardware missing a driver, or is my graphics card not working?",
	Run:      runDrivers,
}

type pciDev struct {
	slot, class, vendor, device, driver string
}

var vendors = map[string]string{
	"0x8086": "Intel", "0x10de": "NVIDIA", "0x1002": "AMD", "0x1022": "AMD", "0x14e4": "Broadcom",
	"0x10ec": "Realtek", "0x168c": "Qualcomm Atheros", "0x17cb": "Qualcomm", "0x14c3": "MediaTek", "0x1969": "Qualcomm Atheros",
}

func (d pciDev) kind() string {
	switch {
	case strings.HasPrefix(d.class, "0x0280"):
		return "Wi-Fi card"
	case strings.HasPrefix(d.class, "0x02"):
		return "network card"
	case strings.HasPrefix(d.class, "0x03"):
		return "graphics card"
	case strings.HasPrefix(d.class, "0x0403"):
		return "sound card"
	case strings.HasPrefix(d.class, "0x0480"):
		return "camera or media controller"
	case strings.HasPrefix(d.class, "0x0d11"):
		return "Bluetooth controller"
	}
	return ""
}

func (d pciDev) name() string {
	v := vendors[d.vendor]
	if v == "" {
		v = "Unknown maker"
	}
	return fmt.Sprintf("%s %s (%s:%s)", v, d.kind(), strings.TrimPrefix(d.vendor, "0x"), strings.TrimPrefix(d.device, "0x"))
}

func pciDevices(s sys.System) []pciDev {
	dirs, _ := s.Glob("/sys/bus/pci/devices/*")
	var out []pciDev
	for _, d := range dirs {
		dev := pciDev{
			slot:   filepath.Base(d),
			class:  readTrim(s, d+"/class"),
			vendor: readTrim(s, d+"/vendor"),
			device: readTrim(s, d+"/device"),
		}
		if b, err := s.ReadFile(d + "/uevent"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(l, "DRIVER="); ok {
					dev.driver = v
				}
			}
		}
		out = append(out, dev)
	}
	return out
}

var firmwareRE = regexp.MustCompile(`Direct firmware load for (\S+) failed|firmware: failed to load (\S+)|failed to load firmware (\S+)|(\S+): firmware: failed`)

func runDrivers(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	var fs []doctor.Finding
	devs := pciDevices(s)

	var gpus []pciDev
	for _, d := range devs {
		k := d.kind()
		if k == "" {
			continue
		}
		if k == "graphics card" {
			gpus = append(gpus, d)
		}
		if d.driver != "" {
			continue
		}
		if d.vendor == "0x10de" && k == "graphics card" {
			continue // reported with the NVIDIA diagnosis below
		}
		fs = append(fs, missingDriver(d, h))
	}

	fs = append(fs, nvidia(ctx, s, h, gpus)...)
	fs = append(fs, missingFirmware(ctx, s, h)...)

	if len(fs) == 0 {
		names := make([]string, 0, len(gpus))
		for _, g := range gpus {
			names = append(names, fmt.Sprintf("%s (%s)", vendors[g.vendor], g.driver))
		}
		t := "All your hardware has a driver"
		if len(names) > 0 {
			t += "; graphics: " + strings.Join(names, ", ")
		}
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: t})
	}
	return fs
}

func missingDriver(d pciDev, h *doctor.Host) doctor.Finding {
	f := doctor.Finding{
		Status:   doctor.Fail,
		Title:    fmt.Sprintf("Your %s has no driver", d.kind()),
		Explain:  fmt.Sprintf("Linux sees the %s, but no driver is controlling it, so it doesn't work.", d.name()),
		Evidence: []string{fmt.Sprintf("PCI %s class %s id %s:%s, no driver", d.slot, d.class, d.vendor, d.device)},
	}
	switch {
	case d.vendor == "0x14e4" && d.kind() == "Wi-Fi card":
		f.Explain += " Many Broadcom Wi-Fi chips need Broadcom's own driver, which distros don't install by default."
		switch h.PackageMgr {
		case "apt":
			f.Fixes = append(f.Fixes, doctor.Fix{Title: "Install the Broadcom driver", Command: "sudo apt install bcmwl-kernel-source", NeedsRoot: true})
		case "dnf":
			f.Fixes = append(f.Fixes, doctor.Fix{Title: "Install the Broadcom driver from RPM Fusion", Command: "sudo dnf install broadcom-wl", NeedsRoot: true})
		case "rpm-ostree":
			f.Fixes = append(f.Fixes, doctor.Fix{Title: "Add the Broadcom driver from RPM Fusion, then restart", Command: "sudo rpm-ostree install broadcom-wl", NeedsRoot: true})
		case "pacman":
			f.Fixes = append(f.Fixes, doctor.Fix{Title: "Install the Broadcom driver", Command: "sudo pacman -S broadcom-wl-dkms", NeedsRoot: true})
		}
	case d.vendor == "0x8086" && d.kind() == "camera or media controller":
		f.Title = "Your laptop's camera needs a driver Linux doesn't ship yet"
		f.Explain = "Recent Intel laptops use a new kind of camera (Intel IPU). Support depends on your distro and kernel version; it's improving quickly."
		f.Fixes = []doctor.Fix{{Title: "Check your distro's notes on Intel IPU6/IPU7 cameras", URL: "https://wiki.archlinux.org/title/Webcam_setup"}}
	default:
		f.Fixes = append(f.Fixes,
			installFix(h, "linux-firmware"),
			doctor.Fix{Title: "Install pending updates; newer kernels add drivers for newer hardware"},
		)
	}
	return f
}

var nvidiaKeyRE = regexp.MustCompile(`(?i)Key was rejected by service|Loading of unsigned module|Required key not available|module verification failed`)

// nvidia correlates the GPU, the loaded driver, Secure Boot and the kernel log
// into one diagnosis, because each fact alone doesn't tell the user anything.
func nvidia(ctx context.Context, s sys.System, h *doctor.Host, gpus []pciDev) []doctor.Finding {
	var nv *pciDev
	for i := range gpus {
		if gpus[i].vendor == "0x10de" {
			nv = &gpus[i]
		}
	}
	if nv == nil {
		return nil
	}
	installed := s.Has("nvidia-smi") || s.Exists("/usr/lib/modules/"+h.Kernel+"/updates/dkms") || s.Exists("/etc/modprobe.d/nvidia.conf")
	loaded := s.Exists("/proc/driver/nvidia/version")
	if loaded {
		var fs []doctor.Finding
		if h.Session == "wayland" && readTrim(s, "/sys/module/nvidia_drm/parameters/modeset") == "N" {
			fs = append(fs, doctor.Finding{
				Status:  doctor.Warn,
				Title:   "NVIDIA kernel mode setting is off, which Wayland needs",
				Explain: "Without it you may get a black screen, a fallback to X11, or no external monitors.",
				Fixes: []doctor.Fix{
					kernelArgFix(s, h, "nvidia-drm.modeset=1"),
					{Title: "Read more about NVIDIA mode setting", URL: "https://wiki.archlinux.org/title/NVIDIA#DRM_kernel_mode_setting"},
				},
			})
		}
		return fs
	}

	keyRejected := false
	if es, err := journal(ctx, s, "-k", "-b", "0", "-p", "0..5"); err == nil {
		for _, e := range es {
			if strings.Contains(strings.ToLower(e.Message), "nvidia") && nvidiaKeyRE.MatchString(e.Message) {
				keyRejected = true
			}
		}
	}

	switch {
	case h.SecureBoot == "enabled" && (keyRejected || installed):
		f := doctor.Finding{
			Status: doctor.Fail,
			Title:  "Secure Boot is blocking the NVIDIA driver",
			Explain: "The NVIDIA driver is installed, but Secure Boot only lets drivers signed with a trusted key start, and this one isn't trusted yet. " +
				"So the system falls back to a basic driver: slow graphics, no games, sometimes no external monitor.",
		}
		switch {
		case h.Like("fedora"):
			f.Fixes = []doctor.Fix{
				{Title: "Create and enroll the signing key", Command: "sudo kmodgenca -a && sudo mokutil --import /etc/pki/akmods/certs/public_key.der", NeedsRoot: true},
				{Title: "Restart, choose \"Enroll MOK\" on the blue screen, and type the password you just set"},
			}
		case h.Like("ubuntu", "debian"):
			f.Fixes = []doctor.Fix{
				{Title: "Enroll the driver's signing key", Command: "sudo update-secureboot-policy --enroll-key", NeedsRoot: true},
				{Title: "Restart, choose \"Enroll MOK\" on the blue screen, and type the password you set"},
			}
		default:
			f.Fixes = []doctor.Fix{{Title: "Sign the NVIDIA module or turn off Secure Boot in the firmware settings", URL: "https://wiki.archlinux.org/title/NVIDIA#Secure_Boot"}}
		}
		return []doctor.Finding{f}
	case installed:
		return []doctor.Finding{{
			Status: doctor.Fail,
			Title:  "The NVIDIA driver is installed but not running",
			Explain: "Usually the driver wasn't rebuilt for the kernel you're running now, which happens after a kernel update. " +
				"On Fedora it may still be building in the background for a few minutes after an update.",
			Fixes: []doctor.Fix{
				{Title: "See whether the module was built for this kernel", Command: "dkms status"},
				{Title: "Restart once the update has finished"},
			},
		}}
	case nv.driver == "nouveau":
		return []doctor.Finding{{
			Status:  doctor.Info,
			Title:   "Your NVIDIA card uses the open-source nouveau driver",
			Explain: "It's fine for the desktop, but slow for games and video editing. NVIDIA's own driver is much faster.",
			Fixes:   []doctor.Fix{{Title: "Install NVIDIA's driver from your distro's driver tool or software center"}},
		}}
	case nv.driver == "":
		return []doctor.Finding{{
			Status:  doctor.Warn,
			Title:   "Your NVIDIA graphics card has no driver",
			Explain: "On laptops with two graphics chips, this means the NVIDIA one is unused, and monitors wired to it won't work.",
			Fixes:   []doctor.Fix{{Title: "Install NVIDIA's driver from your distro's driver tool or software center"}},
		}}
	}
	return nil
}

func missingFirmware(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	es, err := journal(ctx, s, "-k", "-b", "0")
	if err != nil {
		return nil
	}
	files := map[string]int{}
	var lines []string
	for _, e := range es {
		m := firmwareRE.FindStringSubmatch(e.Message)
		if m == nil {
			continue
		}
		name := firstNonEmpty(m[1:]...)
		// Drivers probe optional firmware files and fall back silently; only
		// count files that failed for good (error -2 = file missing).
		if strings.Contains(e.Message, "failed with error -2") || !strings.Contains(e.Message, "Direct firmware load") {
			files[name]++
			lines = append(lines, firstLineOf(e.Message))
		}
	}
	if len(files) == 0 {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Warn,
		Title:  fmt.Sprintf("%s couldn't be loaded: %s", plural(len(files), "firmware file", "firmware files"), topNames(files, 3)),
		Explain: "Some hardware needs a small program (firmware) from the disk to work. If a device misbehaves, this is the likely reason. " +
			"Some drivers try optional files and carry on without them, so this isn't always a problem.",
		Fixes:    []doctor.Fix{installFix(h, "linux-firmware")},
		Evidence: lastN(lines, 5),
	}}
}
