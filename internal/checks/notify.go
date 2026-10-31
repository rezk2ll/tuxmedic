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
	if strings.Contains(strings.ToLower(h.Desktop), "gnome") && s.Has("gsettings") {
		if out, _ := s.Run(ctx, "gsettings", "get", "org.gnome.desktop.notifications", "show-banners"); strings.TrimSpace(string(out)) == "false" {
			return []doctor.Finding{{
				Status:  doctor.Info,
				Title:   "Do Not Disturb is on",
				Explain: "Notifications still arrive, but they only go to the list under the clock instead of popping up. It stays on until you switch it off.",
				Fixes: []doctor.Fix{
					{Title: "Switch it off from the list under the clock, or in Settings, under Notifications"},
					{Title: "Or switch it off from here", Command: "gsettings set org.gnome.desktop.notifications show-banners true"},
				},
			}}
		}
	}
	return []doctor.Finding{{Status: doctor.OK, Title: "Notifications can be shown"}}
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
