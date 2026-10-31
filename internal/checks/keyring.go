package checks

import (
	"context"
	"sort"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Keyring = doctor.Check{
	ID:       "keyring",
	Name:     "Passwords & keyring",
	Category: "System",
	Question: `Why do apps keep asking me to "unlock the login keyring"?`,
	Run:      runKeyring,
}

// autoLogin reports the display manager set up to log in without a password.
func autoLogin(s sys.System) (dm, file string) {
	for _, f := range []string{"/etc/gdm3/custom.conf", "/etc/gdm3/daemon.conf", "/etc/gdm/custom.conf"} {
		for _, l := range strings.Split(readTrim(s, f), "\n") {
			k, v, _ := strings.Cut(strings.TrimSpace(l), "=")
			if (k == "AutomaticLoginEnable" || k == "TimedLoginEnable") && strings.EqualFold(strings.TrimSpace(v), "true") {
				return "GDM", f
			}
		}
	}
	confs := func(main, dir string) []string {
		ds, _ := s.Glob(dir + "/*.conf")
		sort.Strings(ds)
		return append([]string{main}, ds...)
	}
	for _, f := range confs("/etc/lightdm/lightdm.conf", "/etc/lightdm/lightdm.conf.d") {
		for _, l := range strings.Split(readTrim(s, f), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(l), "autologin-user="); ok && strings.TrimSpace(v) != "" {
				return "LightDM", f
			}
		}
	}
	for _, f := range confs("/etc/sddm.conf", "/etc/sddm.conf.d") {
		section := ""
		for _, l := range strings.Split(readTrim(s, f), "\n") {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "[") {
				section = l
			}
			if v, ok := strings.CutPrefix(l, "User="); ok && section == "[Autologin]" && strings.TrimSpace(v) != "" {
				return "SDDM", f
			}
		}
	}
	return "", ""
}

func runKeyring(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Desktop == "" {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No desktop session, so no keyring to check"}}
	}
	home := s.Getenv("HOME")
	if home == "" {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Can't find your home folder"}}
	}
	dm, file := autoLogin(s)
	if dm == "" {
		return []doctor.Finding{{Status: doctor.OK, Title: "Your password unlocks the keyring when you log in"}}
	}
	gnome := home + "/.local/share/keyrings/login.keyring"
	kde := home + "/.local/share/kwalletd/kdewallet.kwl"
	var what, fix string
	switch {
	case readTrim(s, gnome) != "":
		what, fix = "login keyring", "Open Passwords and Keys (Seahorse), right-click Login, and change its password to an empty one"
	case readTrim(s, kde) != "":
		what, fix = "KDE wallet", "Open KDE Wallet Manager, pick kdewallet, and change its password to an empty one"
	default:
		return []doctor.Finding{{Status: doctor.OK, Title: "You log in automatically, and there's no keyring that needs a password"}}
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  "You log in automatically, so apps ask you to unlock the " + what,
		Explain: "Saved passwords for Wi-Fi, browsers and email are locked with your login password. With automatic login you never type it, " +
			"so the first app that needs one asks for it again.",
		Fixes: []doctor.Fix{
			{Title: "Turn off automatic login in Settings, under Users, so logging in unlocks it"},
			{Title: "Or: " + fix + ". Anyone with your disk can then read the saved passwords, unless the disk is encrypted"},
		},
		Evidence: []string{dm + " automatic login: " + file},
	}}
}
