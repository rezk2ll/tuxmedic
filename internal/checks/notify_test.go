package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestNotifications(t *testing.T) {
	const bus = "busctl --user list --no-legend"
	owned := "org.freedesktop.DBus 1 systemd user - - - -\norg.freedesktop.Notifications 2101 gnome-shell sam :1.12 session-2.scope 2 -\n"
	gnome := &doctor.Host{Desktop: "GNOME", Session: "wayland"}

	expect(t, run(t, Notifications, &sys.Fake{Commands: map[string]string{bus: owned}}, gnome), doctor.OK, "Notifications can be shown")

	dnd := &sys.Fake{Commands: map[string]string{bus: owned, "gsettings get org.gnome.desktop.notifications show-banners": "false\n"}}
	expect(t, run(t, Notifications, dnd, gnome), doctor.Info, "Do Not Disturb is on")

	dump := "[org-gnome-calendar]\napplication-id='org.gnome.Calendar.desktop'\n\n" +
		"[slack]\napplication-id='slack.desktop'\nenable=false\n\n[firefox]\nenable=false\napplication-id='firefox_firefox.desktop'\n"
	muted := &sys.Fake{
		Commands: map[string]string{bus: owned, "dconf dump /org/gnome/desktop/notifications/application/": dump},
		Files:    files("/usr/share/applications/slack.desktop", "[Desktop Entry]\nName=Slack\nName[de]=Slack\nExec=slack\n"),
	}
	r := run(t, Notifications, muted, gnome)
	expect(t, r, doctor.Info, "Notifications are switched off for Slack and firefox_firefox")

	sway := &doctor.Host{Desktop: "sway", Session: "wayland", PackageMgr: "pacman"}
	r = run(t, Notifications, &sys.Fake{Commands: map[string]string{bus: "org.freedesktop.DBus 1 systemd user - - - -\n"}}, sway)
	expect(t, r, doctor.Warn, "Nothing on your desktop shows notifications")
	if c := r.Findings[0].Fixes[0].Command; c != "sudo pacman -S mako" {
		t.Errorf("fix = %q", c)
	}

	activatable := "org.freedesktop.Notifications - - - (activatable) - - -\n"
	expect(t, run(t, Notifications, &sys.Fake{Commands: map[string]string{bus: activatable}}, sway), doctor.OK, "Notifications can be shown")
	expect(t, run(t, Notifications, &sys.Fake{}, &doctor.Host{}), doctor.Skipped, "No desktop session")
}
