package checks

import (
	"context"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Notifications = doctor.Check{
	ID:       "notifications",
	Name:     "Notifications",
	Category: "Desktop",
	Question: "Why don't I get notifications for messages and mail?",
	Run:      runNotifications,
}

const notifyName = "org.freedesktop.Notifications"

func runNotifications(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Desktop == "" || h.Container {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No desktop session to show notifications"}}
	}
	if s.Has("busctl") {
		out, err := s.Run(ctx, "busctl", "--user", "list", "--no-legend")
		if err == nil && !hasBusName(string(out), notifyName) {
			return []doctor.Finding{{
				Status: doctor.Warn,
				Title:  "Nothing on your desktop shows notifications",
				Explain: "Apps hand their notifications to a notification service, and none is running or installed. Full desktops such as GNOME " +
					"and KDE have one built in; window managers such as Sway, Hyprland or i3 need one installed separately, so notifications are dropped.",
				Fixes:    []doctor.Fix{notifyDaemonFix(h)},
				Evidence: []string{"no " + notifyName + " on the session bus"},
			}}
		}
	}
	var fs []doctor.Finding
	if strings.Contains(strings.ToLower(h.Desktop), "gnome") {
		if out, _ := s.Run(ctx, "gsettings", "get", "org.gnome.desktop.notifications", "show-banners"); s.Has("gsettings") && strings.TrimSpace(string(out)) == "false" {
			fs = append(fs, doctor.Finding{
				Status:  doctor.Info,
				Title:   "Do Not Disturb is on",
				Explain: "Notifications still arrive, but they only go to the list under the clock instead of popping up. It stays on until you switch it off.",
				Fixes: []doctor.Fix{
					{Title: "Switch it off from the list under the clock, or in Settings, under Notifications"},
					{Title: "Or switch it off from here", Command: "gsettings set org.gnome.desktop.notifications show-banners true"},
				},
			})
		}
		fs = append(fs, mutedApps(ctx, s)...)
	}
	if len(fs) == 0 {
		return []doctor.Finding{{Status: doctor.OK, Title: "Notifications can be shown"}}
	}
	return fs
}

// mutedApps lists the apps whose notifications are switched off in GNOME's
// settings, from one dconf dump of the per-app sections:
//
//	[slack]
//	application-id='slack.desktop'
//	enable=false
func mutedApps(ctx context.Context, s sys.System) []doctor.Finding {
	if !s.Has("dconf") {
		return nil
	}
	out, err := s.Run(ctx, "dconf", "dump", "/org/gnome/desktop/notifications/application/")
	if err != nil {
		return nil
	}
	var muted, ids []string
	id, off := "", false
	flush := func() {
		if off && id != "" {
			muted = append(muted, desktopAppName(s, id))
			ids = append(ids, id)
		}
		id, off = "", false
	}
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(l, "["):
			flush()
		case strings.HasPrefix(l, "application-id="):
			id = strings.Trim(strings.TrimPrefix(l, "application-id="), "'")
		case l == "enable=false":
			off = true
		}
	}
	flush()
	if len(muted) == 0 {
		return nil
	}
	return []doctor.Finding{{
		Status:   doctor.Info,
		Title:    "Notifications are switched off for " + joinAnd(muted),
		Explain:  "These apps can't show notifications at all, not even in the list under the clock. Someone switched them off in the settings, or clicked away a notification with that option.",
		Fixes:    []doctor.Fix{{Title: "Switch them back on in Settings, under Notifications, by app"}},
		Evidence: ids,
	}}
}

// desktopAppName reads the name an app shows in menus from its .desktop file.
func desktopAppName(s sys.System, id string) string {
	for _, dir := range []string{"/usr/share/applications/", "/var/lib/flatpak/exports/share/applications/", "/var/lib/snapd/desktop/applications/"} {
		for _, l := range strings.Split(readTrim(s, dir+id), "\n") {
			if n, ok := strings.CutPrefix(l, "Name="); ok {
				return n
			}
		}
	}
	return strings.TrimSuffix(id, ".desktop")
}

func hasBusName(list, name string) bool {
	for _, l := range strings.Split(list, "\n") {
		if f := strings.Fields(l); len(f) > 0 && f[0] == name {
			return true
		}
	}
	return false
}

func notifyDaemonFix(h *doctor.Host) doctor.Fix {
	if h.Session == "x11" {
		return installFix(h, "dunst")
	}
	return installFix(h, "mako")
}
