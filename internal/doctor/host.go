package doctor

import (
	"bufio"
	"context"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/sys"
)

// Host is what checks need to know about the machine before they look for
// problems: which distro, which init system, which sound and network stack.
type Host struct {
	Distro      string // PRETTY_NAME, e.g. "Fedora Linux 41 (Workstation Edition)"
	ID          string // ID from os-release, e.g. "fedora"
	IDLike      []string
	Kernel      string
	Systemd     bool
	Desktop     string // XDG_CURRENT_DESKTOP
	Session     string // wayland, x11, tty
	SoundServer string // pipewire, pulseaudio, alsa
	Network     string // NetworkManager, iwd, connman, systemd-networkd
	PackageMgr  string // apt, dnf, pacman, zypper, apk, xbps, nix, emerge
	Laptop      bool
	Virtual     string // empty on bare metal
	Container   bool
	Hostname    string
	User        string
	Home        string
	SecureBoot  string // enabled, disabled, or empty when unknown
}

func Probe(ctx context.Context, s sys.System) *Host {
	h := &Host{}
	if b, err := s.ReadFile("/etc/os-release"); err == nil {
		kv := parseOSRelease(string(b))
		h.Distro, h.ID = kv["PRETTY_NAME"], kv["ID"]
		h.IDLike = strings.Fields(kv["ID_LIKE"])
	} else if b, err := s.ReadFile("/usr/lib/os-release"); err == nil {
		kv := parseOSRelease(string(b))
		h.Distro, h.ID = kv["PRETTY_NAME"], kv["ID"]
		h.IDLike = strings.Fields(kv["ID_LIKE"])
	}
	if h.Distro == "" {
		h.Distro = "Unknown Linux"
	}
	if b, err := s.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		h.Kernel = strings.TrimSpace(string(b))
	}
	if b, err := s.ReadFile("/proc/sys/kernel/hostname"); err == nil {
		h.Hostname = strings.TrimSpace(string(b))
	}
	h.Systemd = s.Exists("/run/systemd/system")
	h.Desktop = s.Getenv("XDG_CURRENT_DESKTOP")
	h.Session = s.Getenv("XDG_SESSION_TYPE")
	h.User = s.Getenv("USER")
	h.Home = s.Getenv("HOME")
	h.Container = s.Exists("/.dockerenv") || s.Exists("/run/.containerenv")
	h.SoundServer = soundServer(ctx, s)
	h.Network = networkStack(s)
	h.PackageMgr = packageManager(s)
	h.Laptop = isLaptop(s)
	if s.Has("systemd-detect-virt") {
		if out, err := s.Run(ctx, "systemd-detect-virt"); err == nil {
			if v := strings.TrimSpace(string(out)); v != "none" {
				h.Virtual = v
			}
		}
	}
	h.SecureBoot = secureBoot(s)
	return h
}

func (h *Host) Like(ids ...string) bool {
	for _, id := range ids {
		if h.ID == id {
			return true
		}
		for _, l := range h.IDLike {
			if l == id {
				return true
			}
		}
	}
	return false
}

func parseOSRelease(s string) map[string]string {
	kv := map[string]string{}
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || strings.HasPrefix(k, "#") {
			continue
		}
		kv[k] = strings.Trim(v, `"'`)
	}
	return kv
}

func soundServer(ctx context.Context, s sys.System) string {
	if s.Has("pactl") {
		if out, err := s.Run(ctx, "pactl", "info"); err == nil {
			if strings.Contains(string(out), "PipeWire") {
				return "pipewire"
			}
			return "pulseaudio"
		}
	}
	if s.Has("wpctl") {
		if _, err := s.Run(ctx, "wpctl", "status"); err == nil {
			return "pipewire"
		}
	}
	if s.Exists("/proc/asound/cards") {
		return "alsa"
	}
	return ""
}

func networkStack(s sys.System) string {
	switch {
	case s.Exists("/run/NetworkManager"):
		return "NetworkManager"
	case s.Exists("/run/iwd") || s.Exists("/var/lib/iwd"):
		return "iwd"
	case s.Exists("/run/connman"):
		return "connman"
	case s.Exists("/run/systemd/netif/state"):
		return "systemd-networkd"
	}
	return ""
}

func packageManager(s sys.System) string {
	for _, pm := range []string{"apt", "dnf", "pacman", "zypper", "apk", "xbps-install", "emerge", "nix-env", "eopkg", "swupd"} {
		if s.Has(pm) {
			return strings.TrimSuffix(strings.TrimSuffix(pm, "-install"), "-env")
		}
	}
	return ""
}

// isLaptop uses the SMBIOS chassis type; a battery alone is not enough because
// UPS units and wireless mice also show up as power supplies.
func isLaptop(s sys.System) bool {
	if b, err := s.ReadFile("/sys/class/dmi/id/chassis_type"); err == nil {
		switch strings.TrimSpace(string(b)) {
		case "8", "9", "10", "11", "14", "30", "31", "32":
			return true
		}
	}
	bats, _ := s.Glob("/sys/class/power_supply/BAT*")
	return len(bats) > 0
}

func secureBoot(s sys.System) string {
	vars, _ := s.Glob("/sys/firmware/efi/efivars/SecureBoot-*")
	if len(vars) == 0 {
		return ""
	}
	b, err := s.ReadFile(vars[0])
	if err != nil || len(b) < 5 {
		return ""
	}
	if b[4] == 1 {
		return "enabled"
	}
	return "disabled"
}
