package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Disk = doctor.Check{
	ID:       "disk",
	Name:     "Disks & storage",
	Category: "Storage",
	Question: "Is my disk full or failing?",
	Run:      runDisk,
}

var realFS = map[string]bool{
	"ext2": true, "ext3": true, "ext4": true, "btrfs": true, "xfs": true, "f2fs": true,
	"zfs": true, "bcachefs": true, "jfs": true, "reiserfs": true, "vfat": true, "exfat": true, "ntfs": true, "ntfs3": true,
}

type mount struct{ source, target, fstype, options string }

func mounts(s sys.System) []mount {
	b, err := s.ReadFile("/proc/self/mounts")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var ms []mount
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || !realFS[f[2]] {
			continue
		}
		target := unescapeMount(f[1])
		if strings.HasPrefix(target, "/snap/") || strings.HasPrefix(target, "/var/lib/docker") || strings.HasPrefix(target, "/run/") {
			continue
		}
		// btrfs subvolumes share one pool: count each device once.
		key := f[0]
		if f[2] != "btrfs" {
			key = f[0] + target
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		ms = append(ms, mount{source: f[0], target: target, fstype: f[2], options: f[3]})
	}
	return ms
}

func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(s)
}

func runDisk(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	var fs []doctor.Finding
	var okLines []string
	for _, m := range mounts(s) {
		if m.target == "/boot/efi" || m.target == "/efi" {
			continue
		}
		// Container runtimes bind-mount single host files like /etc/hostname.
		if h.Container && m.target != "/" {
			continue
		}
		if m.target == "/" && hasOpt(m.options, "ro") && !h.Container {
			fs = append(fs, doctor.Finding{
				Status: doctor.Fail,
				Title:  "Your main disk switched to read-only",
				Explain: "Linux stops writing to a disk when it finds errors on it, to avoid making them worse. Apps can't save anything until it's repaired. " +
					"This often follows a sudden power loss, and sometimes means the drive is failing.",
				Fixes: []doctor.Fix{
					{Title: "Back up important files now"},
					{Title: "Restart: most distros repair the disk automatically at boot"},
					{Title: "If it comes back, check the drive health with sudo tuxmedic", Command: "sudo tuxmedic", NeedsRoot: true},
				},
				Evidence: []string{fmt.Sprintf("%s on %s type %s (%s)", m.source, m.target, m.fstype, m.options)},
			})
		}
		u, err := s.Statfs(m.target)
		if err != nil || u.Total == 0 {
			continue
		}
		pct := float64(u.Avail) / float64(u.Total) * 100
		where := describeMount(m.target)
		switch {
		case u.Avail < 1<<30 && m.target == "/" || pct < 3:
			fs = append(fs, fullDisk(doctor.Fail, where, u, pct, m, h))
		case pct < 10 && u.Avail < 20<<30:
			fs = append(fs, fullDisk(doctor.Warn, where, u, pct, m, h))
		default:
			okLines = append(okLines, fmt.Sprintf("%s: %s free of %s", where, humanBytes(u.Avail), humanBytes(u.Total)))
		}
		if u.Files > 0 && float64(u.FilesFree)/float64(u.Files) < 0.05 && m.fstype != "btrfs" {
			fs = append(fs, doctor.Finding{
				Status: doctor.Fail,
				Title:  where + " has run out of file slots",
				Explain: "There is free space, but the disk can't hold any more files: it has a fixed number of slots (inodes) and almost all are used. " +
					"Millions of tiny files, usually caches or a runaway log or mail folder, cause this.",
				Fixes: []doctor.Fix{{Title: "Find the folders with the most files", Command: "sudo du --inodes -x " + m.target + " 2>/dev/null | sort -n | tail -15", NeedsRoot: true}},
			})
		}
	}
	if len(okLines) > 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "Enough free space: " + okLines[0], Evidence: okLines})
	}
	fs = append(fs, ioErrors(ctx, s)...)
	fs = append(fs, smartHealth(ctx, s, h)...)
	return fs
}

func hasOpt(opts, want string) bool {
	for _, o := range strings.Split(opts, ",") {
		if o == want {
			return true
		}
	}
	return false
}

func describeMount(target string) string {
	switch target {
	case "/":
		return "Your main disk"
	case "/home":
		return "Your home folder disk"
	case "/boot":
		return "The boot partition"
	}
	return "The disk at " + target
}

func fullDisk(st doctor.Status, where string, u sys.DiskUsage, pct float64, m mount, h *doctor.Host) doctor.Finding {
	f := doctor.Finding{
		Status: st,
		Title:  fmt.Sprintf("%s is almost full: %s free (%.0f%%)", where, humanBytes(u.Avail), pct),
		Explain: "When a disk fills up, apps fail to save, updates break halfway, and the desktop can refuse to log in. " +
			"The fastest wins are usually old package files, logs, the trash, and container images.",
	}
	if m.target == "/boot" {
		f.Explain = "The boot partition holds kernels. When it's full, the next kernel update fails. Removing old kernels frees it."
		switch h.PackageMgr {
		case "apt":
			f.Fixes = append(f.Fixes, doctor.Fix{Title: "Remove old kernels", Command: "sudo apt autoremove --purge", NeedsRoot: true})
		case "dnf":
			f.Fixes = append(f.Fixes, doctor.Fix{Title: "Keep only the 2 newest kernels", Command: "sudo dnf remove --oldinstallonly --setopt installonly_limit=2 kernel", NeedsRoot: true})
		}
		return f
	}
	f.Fixes = append(f.Fixes, cleanupFixes(h)...)
	f.Fixes = append(f.Fixes, doctor.Fix{Title: "See what takes the most space in your home folder", Command: "du -xh ~ 2>/dev/null | sort -h | tail -20"})
	if m.fstype == "btrfs" {
		f.Explain += " On btrfs, old snapshots also keep deleted files around."
		f.Fixes = append(f.Fixes, doctor.Fix{Title: "List snapshots", Command: "sudo btrfs subvolume list -s " + m.target, NeedsRoot: true})
	}
	return f
}

func cleanupFixes(h *doctor.Host) []doctor.Fix {
	var fx []doctor.Fix
	switch h.PackageMgr {
	case "apt":
		fx = append(fx, doctor.Fix{Title: "Delete downloaded package files and unused packages", Command: "sudo apt clean && sudo apt autoremove", NeedsRoot: true})
	case "dnf":
		fx = append(fx, doctor.Fix{Title: "Delete downloaded package files", Command: "sudo dnf clean all", NeedsRoot: true})
	case "pacman":
		fx = append(fx, doctor.Fix{Title: "Keep only the newest cached packages", Command: "sudo pacman -Sc", NeedsRoot: true})
	case "zypper":
		fx = append(fx, doctor.Fix{Title: "Delete downloaded package files", Command: "sudo zypper clean --all", NeedsRoot: true})
	case "apk":
		fx = append(fx, doctor.Fix{Title: "Delete downloaded package files", Command: "sudo apk cache clean", NeedsRoot: true})
	case "nix":
		fx = append(fx, doctor.Fix{Title: "Delete old generations", Command: "sudo nix-collect-garbage --delete-older-than 14d", NeedsRoot: true})
	case "rpm-ostree":
		fx = append(fx, doctor.Fix{Title: "Delete cached update files", Command: "sudo rpm-ostree cleanup --base --repomd", NeedsRoot: true})
	}
	if h.PackageMgr == "rpm-ostree" || h.PackageMgr == "transactional-update" {
		fx = append(fx, doctor.Fix{Title: "Remove app runtimes no app uses any more", Command: "flatpak uninstall --unused"})
	}
	if h.Systemd {
		fx = append(fx, doctor.Fix{Title: "Shrink the system log to 200 MB", Command: "sudo journalctl --vacuum-size=200M", NeedsRoot: true})
	}
	fx = append(fx, doctor.Fix{Title: "Empty the trash", Command: "rm -rf ~/.local/share/Trash/*"})
	fx = append(fx, doctor.Fix{Title: "Remove Flatpak runtimes nothing uses", Command: "flatpak uninstall --unused"})
	fx = append(fx, doctor.Fix{Title: "Remove unused container images", Command: "docker system prune"})
	return fx
}

var ioErrorRE = regexp.MustCompile(`(?i)I/O error, dev (\w+)|blk_update_request: (?:critical medium|I/O) error, dev (\w+)|(EXT4-fs error) \(device (\w+)\)|(BTRFS error) \(device (\w+)|nvme(\d+)\w*: (?:I/O \d+ QID \d+ timeout|controller is down)|ata\d+\.\d+: failed command|Buffer I/O error on dev (\w+)`)

func ioErrors(ctx context.Context, s sys.System) []doctor.Finding {
	es, err := journal(ctx, s, "-k", "--since", "-7d", "-p", "0..3")
	if err != nil {
		return nil
	}
	var lines []string
	devs := map[string]int{}
	for _, e := range es {
		m := ioErrorRE.FindStringSubmatch(e.Message)
		if m == nil {
			continue
		}
		dev := "a drive"
		for _, g := range m[1:] {
			if g != "" && !strings.HasSuffix(g, "error") {
				dev = g
				break
			}
		}
		if strings.HasPrefix(dev, "loop") || strings.HasPrefix(dev, "sr") {
			continue
		}
		devs[dev]++
		lines = append(lines, firstLineOf(e.Message))
	}
	if len(lines) == 0 {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Fail,
		Title:  fmt.Sprintf("A drive reported read or write errors %s this week (%s)", plural(len(lines), "time", "times"), topNames(devs, 3)),
		Explain: "The kernel couldn't read or write parts of a drive. It can be a loose cable, a failing USB stick, or a drive that is wearing out. " +
			"If it's your main drive, back up your files before anything else.",
		Fixes: []doctor.Fix{
			{Title: "Back up your important files"},
			{Title: "Check the drive's own health report", Command: "sudo tuxmedic", NeedsRoot: true},
		},
		Evidence: lastN(lines, 6),
	}}
}

// smartReport follows smartctl's JSON layout; UDisks2 data is mapped onto it
// so both sources share one judgement.
type smartReport struct {
	Model       string       `json:"model_name"`
	SmartStatus *smartStatus `json:"smart_status"`
	NVMe        *nvmeHealth  `json:"nvme_smart_health_information_log"`
	ATA         *ataAttrs    `json:"ata_smart_attributes"`
}

type smartStatus struct {
	Passed bool `json:"passed"`
}

type nvmeHealth struct {
	CriticalWarning int `json:"critical_warning"`
	PercentageUsed  int `json:"percentage_used"`
	MediaErrors     int `json:"media_errors"`
	AvailableSpare  int `json:"available_spare"`
}

type ataAttrs struct {
	Table []ataAttr `json:"table"`
}

type ataAttr struct {
	ID  int `json:"id"`
	Raw struct {
		Value int64 `json:"value"`
	} `json:"raw"`
}

func diskDevices(s sys.System) []string {
	blocks, _ := s.Glob("/sys/block/*")
	var devs []string
	for _, b := range blocks {
		n := filepath.Base(b)
		if strings.HasPrefix(n, "loop") || strings.HasPrefix(n, "ram") || strings.HasPrefix(n, "zram") ||
			strings.HasPrefix(n, "dm-") || strings.HasPrefix(n, "sr") || strings.HasPrefix(n, "md") || strings.HasPrefix(n, "nbd") {
			continue
		}
		if r, _ := s.ReadFile(b + "/removable"); strings.TrimSpace(string(r)) == "1" {
			continue
		}
		devs = append(devs, "/dev/"+n)
	}
	return devs
}

func smartHealth(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	devs := diskDevices(s)
	if len(devs) == 0 || h.Container || h.Virtual != "" {
		return nil
	}
	if fs, ok := udisksHealth(ctx, s); ok {
		return fs
	}
	if !s.Has("smartctl") {
		return []doctor.Finding{{
			Status:  doctor.Info,
			Title:   "Install smartmontools to check whether your drive is wearing out",
			Explain: "Drives keep their own health record. smartctl reads it, and tuxmedic explains it.",
			Fixes:   []doctor.Fix{installFix(h, "smartmontools")},
		}}
	}
	if !s.IsRoot() {
		return []doctor.Finding{{
			Status:  doctor.Skipped,
			Title:   "Drive health needs administrator rights to read",
			Explain: "The drive's health record is only readable by root.",
			Fixes:   []doctor.Fix{{Title: "Run the full check", Command: "sudo tuxmedic", NeedsRoot: true}},
		}}
	}
	var fs []doctor.Finding
	for _, d := range devs {
		out, _ := s.Run(ctx, "smartctl", "-j", "-H", "-A", "-i", d)
		var r smartReport
		if json.Unmarshal(out, &r) != nil || r.SmartStatus == nil {
			continue
		}
		fs = append(fs, judgeSmart(d, r))
	}
	return fs
}

func judgeSmart(dev string, r smartReport) doctor.Finding {
	name := dev
	if r.Model != "" {
		name = fmt.Sprintf("%s (%s)", r.Model, dev)
	}
	backup := []doctor.Fix{{Title: "Back up your files now and plan to replace this drive"}}
	if !r.SmartStatus.Passed {
		return doctor.Finding{
			Status:  doctor.Fail,
			Title:   name + " says it is failing",
			Explain: "The drive's own self-check failed. It may stop working at any moment.",
			Fixes:   backup,
		}
	}
	if n := r.NVMe; n != nil {
		switch {
		case n.CriticalWarning != 0 || n.MediaErrors > 0:
			return doctor.Finding{
				Status:  doctor.Fail,
				Title:   fmt.Sprintf("%s reports errors (%d media errors, warning flags %#x)", name, n.MediaErrors, n.CriticalWarning),
				Explain: "The SSD has hit data errors or raised a critical warning. Data on it is at risk.",
				Fixes:   backup,
			}
		case n.PercentageUsed >= 90:
			return doctor.Finding{
				Status:  doctor.Warn,
				Title:   fmt.Sprintf("%s has used %d%% of its rated life", name, n.PercentageUsed),
				Explain: "SSDs wear out after a certain amount of writing. This one is near the end of what the maker guarantees.",
				Fixes:   backup,
			}
		}
		return doctor.Finding{Status: doctor.OK, Title: fmt.Sprintf("%s is healthy (%d%% of its rated life used)", name, n.PercentageUsed)}
	}
	if a := r.ATA; a != nil {
		var bad []string
		for _, attr := range a.Table {
			switch attr.ID {
			case 5:
				if attr.Raw.Value > 0 {
					bad = append(bad, fmt.Sprintf("%d reallocated sectors", attr.Raw.Value))
				}
			case 197:
				if attr.Raw.Value > 0 {
					bad = append(bad, fmt.Sprintf("%d sectors waiting to be reallocated", attr.Raw.Value))
				}
			case 198:
				if attr.Raw.Value > 0 {
					bad = append(bad, fmt.Sprintf("%d unreadable sectors", attr.Raw.Value))
				}
			}
		}
		if len(bad) > 0 {
			return doctor.Finding{
				Status:  doctor.Warn,
				Title:   name + " is developing bad spots: " + strings.Join(bad, ", "),
				Explain: "The drive still passes its self-check, but it has started replacing damaged areas. This number tends to grow.",
				Fixes:   backup,
			}
		}
	}
	return doctor.Finding{Status: doctor.OK, Title: name + " is healthy"}
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	v := float64(b) / float64(div)
	if v >= 100 {
		return fmt.Sprintf("%.0f %cB", v, "KMGTPE"[exp])
	}
	return fmt.Sprintf("%.1f %cB", v, "KMGTPE"[exp])
}
