package checks

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Drives = doctor.Check{
	ID:       "drives",
	Name:     "USB drives & Windows disks",
	Category: "Devices",
	Question: "Why won't my USB stick or Windows drive open, or why is it read-only?",
	Run:      runDrives,
}

// flexBool reads lsblk's booleans, which older util-linux prints as "0"/"1".
type flexBool bool

func (b *flexBool) UnmarshalJSON(d []byte) error {
	s := strings.Trim(string(d), `"`)
	*b = s == "true" || s == "1"
	return nil
}

type blockDev struct {
	Path       string     `json:"path"`
	Name       string     `json:"name"`
	FSType     string     `json:"fstype"`
	Mountpoint string     `json:"mountpoint"`
	Tran       string     `json:"tran"`
	Hotplug    flexBool   `json:"hotplug"`
	Label      string     `json:"label"`
	Size       string     `json:"size"`
	Type       string     `json:"type"`
	Children   []blockDev `json:"children"`
}

func (d blockDev) display() string {
	if d.Label != "" {
		return `"` + d.Label + `"`
	}
	return d.Size + " " + d.path()
}

func (d blockDev) path() string {
	if d.Path != "" {
		return d.Path
	}
	return "/dev/" + d.Name
}

func runDrives(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	out, err := s.Run(ctx, "lsblk", "-J", "-o", "NAME,PATH,FSTYPE,MOUNTPOINT,TRAN,HOTPLUG,LABEL,SIZE,TYPE")
	var tree struct {
		Devices []blockDev `json:"blockdevices"`
	}
	if err != nil || json.Unmarshal(out, &tree) != nil {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Couldn't list the drives (lsblk is missing)"}}
	}
	readOnly := roMounts(s)

	var fs []doctor.Finding
	var ready []string
	removable := 0
	for _, disk := range tree.Devices {
		if disk.Type != "disk" {
			continue
		}
		usb := disk.Tran == "usb" || bool(disk.Hotplug)
		if usb {
			removable++
		}
		parts := disk.Children
		if len(parts) == 0 {
			parts = []blockDev{disk}
		}
		for _, p := range parts {
			if !usb && p.FSType != "ntfs" {
				continue
			}
			switch {
			case (p.FSType == "ntfs" || p.FSType == "exfat") && !canMount(s, h, p.FSType):
				fs = append(fs, noFSDriver(h, p))
			case p.FSType == "ntfs" && p.Mountpoint != "" && readOnly[p.Mountpoint]:
				fs = append(fs, doctor.Finding{
					Status: doctor.Warn,
					Title:  "The drive " + p.display() + " opened read-only",
					Explain: "Windows didn't shut it down fully. Windows' Fast Startup and hibernation leave NTFS drives marked as in use, " +
						"and Linux then only lets you read them so the files Windows still has open don't get damaged.",
					Fixes: []doctor.Fix{
						{Title: "Best: start Windows, turn off Fast Startup (Control Panel, Power Options), then shut down", URL: "https://learn.microsoft.com/troubleshoot/windows-client/setup-upgrade-and-drivers/fast-startup-causes-hibernation"},
						{Title: "Or, for a USB drive: plug it into Windows and use Safely Remove before unplugging"},
						{Title: "Only if Windows isn't hibernated: clear the mark and open it again", Command: "sudo umount " + p.path() + " && sudo ntfsfix -d " + p.path(), NeedsRoot: true},
					},
					Evidence: []string{p.path() + " mounted at " + p.Mountpoint + " with ro"},
				})
			case usb && p.Mountpoint == "" && p.FSType != "" && p.FSType != "swap" && !strings.Contains(p.FSType, "_member") && p.FSType != "crypto_LUKS":
				f := doctor.Finding{
					Status:  doctor.Info,
					Title:   "The drive " + p.display() + " is plugged in but not opened",
					Explain: "Linux sees it, but it isn't mounted yet. Click it in your file manager to open it.",
				}
				if s.Has("udisksctl") {
					f.Fixes = []doctor.Fix{{Title: "Open it from here", Command: "udisksctl mount -b " + p.path()}}
				}
				fs = append(fs, f)
			case usb && p.FSType == "" && len(disk.Children) == 0:
				fs = append(fs, doctor.Finding{
					Status:  doctor.Info,
					Title:   "The " + disk.Size + " drive " + disk.path() + " has nothing Linux can read on it",
					Explain: "It has no file system, or one Linux doesn't know. It may be new and empty, or the card reader may have no card in it. Formatting erases everything on it.",
					Fixes:   []doctor.Fix{{Title: "If it's empty, format it with the Disks app (choose exFAT to use it on Windows and Mac too)"}},
				})
			case usb && p.Mountpoint != "":
				ready = append(ready, p.display())
			}
		}
	}
	if len(ready) > 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "USB drives open: " + joinAnd(ready)})
	}
	if len(fs) == 0 {
		title := "No USB drive is plugged in"
		if removable > 0 {
			title = "USB drives look fine"
		}
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: title})
	}
	return fs
}

func roMounts(s sys.System) map[string]bool {
	b, _ := s.ReadFile("/proc/mounts")
	ro := map[string]bool{}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !strings.HasPrefix(f[2], "ntfs") && f[2] != "fuseblk" {
			continue
		}
		if slices.Contains(strings.Split(f[3], ","), "ro") {
			ro[strings.ReplaceAll(f[1], `\040`, " ")] = true
		}
	}
	return ro
}

// canMount says whether the kernel or a FUSE helper can open fstype.
func canMount(s sys.System, h *doctor.Host, fstype string) bool {
	if b, err := s.ReadFile("/proc/filesystems"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) > 0 && (f[len(f)-1] == fstype || fstype == "ntfs" && f[len(f)-1] == "ntfs3") {
				return true
			}
		}
	}
	mods := []string{fstype}
	helpers := []string{"mount.exfat-fuse", "mount.exfat"}
	if fstype == "ntfs" {
		mods = []string{"ntfs3", "ntfs"}
		helpers = []string{"ntfs-3g", "mount.ntfs-3g", "mount.ntfs"}
	}
	for _, m := range mods {
		if hits, _ := s.Glob("/lib/modules/" + h.Kernel + "/kernel/fs/" + m + "/*"); len(hits) > 0 {
			return true
		}
	}
	for _, x := range helpers {
		if s.Has(x) {
			return true
		}
	}
	return false
}

func noFSDriver(h *doctor.Host, p blockDev) doctor.Finding {
	name, need := "NTFS (Windows)", "ntfs-3g"
	if p.FSType == "exfat" {
		name, need = "exFAT", "exfat"
	}
	return doctor.Finding{
		Status:  doctor.Fail,
		Title:   "Linux can't open the drive " + p.display() + ": it uses " + name,
		Explain: "Opening " + name + " drives needs a driver this system doesn't have. Install it, then unplug the drive and plug it back in.",
		Fixes:   []doctor.Fix{installFix(h, need)},
	}
}
