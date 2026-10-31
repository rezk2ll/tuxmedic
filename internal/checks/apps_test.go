package checks

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

const (
	gnomePortal = "[portal]\nDBusName=org.freedesktop.impl.portal.desktop.gnome\nInterfaces=org.freedesktop.impl.portal.FileChooser;org.freedesktop.impl.portal.ScreenCast;\nUseIn=gnome\n"
	gtkPortal   = "[portal]\nInterfaces=org.freedesktop.impl.portal.FileChooser;org.freedesktop.impl.portal.AppChooser;\nUseIn=gnome\n"
	wlrPortal   = "[portal]\nInterfaces=org.freedesktop.impl.portal.Screenshot;org.freedesktop.impl.portal.ScreenCast;\nUseIn=wlroots;sway;Wayfire;river;\n"
)

func appsFake(files map[string]string, cmds map[string]string) *sys.Fake {
	if cmds == nil {
		cmds = map[string]string{}
	}
	cmds["pipewire --version"] = "pipewire 1.4\n"
	return &sys.Fake{Files: files, Commands: cmds}
}

func TestFileWatches(t *testing.T) {
	gnome := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Desktop: "GNOME", Session: "wayland"}
	files := func(limit string, procs ...string) map[string]string {
		m := map[string]string{
			portalDir + "/portals/gnome.portal":     gnomePortal,
			"/proc/sys/fs/inotify/max_user_watches": limit + "\n",
		}
		for i, p := range procs {
			m[fmt.Sprintf("/proc/%d/comm", 100+i)] = p + "\n"
		}
		return m
	}

	t.Run("low limit with an editor open", func(t *testing.T) {
		r := run(t, Apps, appsFake(files("8192", "bash", "code"), nil), gnome)
		expect(t, r, doctor.Info, "code can only watch 8192 files for changes")
		if c := r.Findings[0].Fixes[0].Command; !strings.Contains(c, "fs.inotify.max_user_watches=524288") {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("low limit, nothing that needs it", func(t *testing.T) {
		expect(t, run(t, Apps, appsFake(files("8192", "bash"), nil), gnome), doctor.OK, "Screen sharing")
	})
	t.Run("modern default", func(t *testing.T) {
		expect(t, run(t, Apps, appsFake(files("524288", "code"), nil), gnome), doctor.OK, "Screen sharing")
	})
}

func TestApps(t *testing.T) {
	gnome := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Desktop: "GNOME", Session: "wayland"}
	sway := &doctor.Host{ID: "arch", PackageMgr: "pacman", Desktop: "sway", Session: "wayland"}

	t.Run("gnome wayland ready", func(t *testing.T) {
		f := appsFake(map[string]string{portalDir + "/portals/gnome.portal": gnomePortal}, nil)
		expect(t, run(t, Apps, f, gnome), doctor.OK, "Screen sharing is set up for Wayland")
	})
	t.Run("gnome with only the gtk portal", func(t *testing.T) {
		f := appsFake(map[string]string{portalDir + "/portals/gtk.portal": gtkPortal}, nil)
		r := run(t, Apps, f, gnome)
		expect(t, r, doctor.Warn, "Screen sharing won't work in this Wayland session")
		if r.Findings[0].Fixes[0].Command != "sudo dnf install xdg-desktop-portal-gnome" {
			t.Errorf("fix = %q", r.Findings[0].Fixes[0].Command)
		}
	})
	t.Run("sway picked through portals.conf", func(t *testing.T) {
		f := appsFake(map[string]string{
			portalDir + "/portals/wlr.portal": "[portal]\nInterfaces=org.freedesktop.impl.portal.ScreenCast;\n",
			portalDir + "/sway-portals.conf":  "[preferred]\ndefault=gtk\norg.freedesktop.impl.portal.ScreenCast=wlr\n",
		}, nil)
		expect(t, run(t, Apps, f, sway), doctor.OK, "Screen sharing is set up")
	})
	t.Run("sway via UseIn", func(t *testing.T) {
		f := appsFake(map[string]string{portalDir + "/portals/wlr.portal": wlrPortal}, nil)
		expect(t, run(t, Apps, f, sway), doctor.OK, "Screen sharing is set up")
	})
	t.Run("no portals at all", func(t *testing.T) {
		expect(t, run(t, Apps, appsFake(nil, nil), sway), doctor.Fail, "Desktop portals aren't installed")
	})
	t.Run("x11 without flatpak has nothing to check", func(t *testing.T) {
		h := &doctor.Host{ID: "debian", Desktop: "XFCE", Session: "x11"}
		r := run(t, Apps, appsFake(nil, nil), h)
		if len(r.Findings) != 0 {
			t.Errorf("findings = %+v", r.Findings)
		}
	})
	t.Run("flatpak without a file chooser", func(t *testing.T) {
		f := appsFake(map[string]string{portalDir + "/portals/wlr.portal": wlrPortal}, map[string]string{"flatpak --version": "Flatpak 1.16\n"})
		expect(t, run(t, Apps, f, sway), doctor.Warn, "Flatpak apps can't show Open and Save dialogs")
	})
	t.Run("portal service crashed", func(t *testing.T) {
		f := appsFake(map[string]string{portalDir + "/portals/gnome.portal": gnomePortal}, nil)
		f.Commands["systemctl --user is-active xdg-desktop-portal"] = "failed\n"
		expect(t, run(t, Apps, f, gnome), doctor.Fail, "The desktop portal service crashed")
	})
	t.Run("snap leftovers and a running change", func(t *testing.T) {
		f := appsFake(nil, map[string]string{
			"snap changes --abs-time": "ID   Status  Spawn                 Ready  Summary\n" +
				"42   Doing   2026-10-30T09:00:00Z  -      Auto-refresh snap \"firefox\"\n",
			"snap list --all": "Name  Version  Rev  Tracking  Publisher  Notes\n" +
				"a 1 1 - x disabled\nb 1 2 - x disabled\nc 1 3 - x disabled\nc 2 4 - x -\n",
		})
		h := &doctor.Host{ID: "ubuntu", Session: "x11"}
		r := run(t, Apps, f, h)
		expect(t, r, doctor.Info, `A snap change is still running: Auto-refresh snap "firefox"`)
		expect(t, r, doctor.Info, "Snap keeps 3 old versions")
	})
	t.Run("snap apps without removable media", func(t *testing.T) {
		f := appsFake(nil, map[string]string{
			"snap connections --all": "Interface        Plug                     Slot              Notes\n" +
				"audio-playback   firefox:audio-playback   :audio-playback   -\n" +
				"removable-media  firefox:removable-media  -                 -\n" +
				"removable-media  vlc:removable-media      :removable-media  manual\n" +
				"removable-media  gimp:removable-media     -                 -\n",
		})
		r := run(t, Apps, f, &doctor.Host{ID: "ubuntu", Session: "x11"})
		expect(t, r, doctor.Info, "can't open files on USB drives or other disks: firefox, gimp")
		for _, fd := range r.Findings {
			if len(fd.Fixes) == 2 && fd.Fixes[1].Command != "sudo snap connect gimp:removable-media" {
				t.Errorf("fix = %q", fd.Fixes[1].Command)
			}
		}
	})
	t.Run("container", func(t *testing.T) {
		expect(t, run(t, Apps, appsFake(nil, nil), &doctor.Host{Container: true}), doctor.Skipped, "No desktop session")
	})
}
