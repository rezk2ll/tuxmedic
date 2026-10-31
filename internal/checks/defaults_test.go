package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestDefaults(t *testing.T) {
	gnome := &doctor.Host{ID: "fedora", Desktop: "GNOME", PackageMgr: "dnf"}
	q := func(mime string) string { return "xdg-mime query default " + mime }
	fake := func(cmds map[string]string, files ...string) *sys.Fake {
		f := &sys.Fake{Commands: cmds, Files: map[string]string{}, Env: map[string]string{"HOME": "/home/alex"}}
		for _, p := range files {
			f.Files[p] = "[Desktop Entry]\n"
		}
		return f
	}

	t.Run("all installed", func(t *testing.T) {
		f := fake(map[string]string{
			q("x-scheme-handler/https"): "firefox.desktop\n",
			q("inode/directory"):        "org.gnome.Nautilus.desktop\n",
			q("application/pdf"):        "kde-okular.desktop\n",
		}, "/usr/share/applications/firefox.desktop", "/usr/share/applications/org.gnome.Nautilus.desktop",
			"/usr/share/applications/kde/okular.desktop")
		expect(t, run(t, Defaults, f, gnome), doctor.OK, "open in apps that are installed")
	})
	t.Run("browser removed, a flatpak one is there", func(t *testing.T) {
		f := fake(map[string]string{q("x-scheme-handler/https"): "google-chrome.desktop\n"},
			"/var/lib/flatpak/exports/share/applications/org.mozilla.firefox.desktop")
		r := run(t, Defaults, f, gnome)
		expect(t, r, doctor.Warn, "Web links open nothing: their default app (google-chrome) isn't installed anymore")
		if c := r.Findings[0].Fixes[0].Command; c != "xdg-settings set default-web-browser org.mozilla.firefox.desktop" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("an app in the user's own folder counts", func(t *testing.T) {
		f := fake(map[string]string{q("x-scheme-handler/https"): "zen.desktop\n"}, "/home/alex/.local/share/applications/zen.desktop")
		expect(t, run(t, Defaults, f, gnome), doctor.OK, "installed")
	})
	t.Run("no browser set", func(t *testing.T) {
		f := fake(map[string]string{q("x-scheme-handler/https"): "\n"})
		r := run(t, Defaults, f, gnome)
		expect(t, r, doctor.Warn, "No default web browser is set")
		if r.Findings[0].Fixes[0].Command != "" {
			t.Errorf("no browser is installed, yet the fix runs %q", r.Findings[0].Fixes[0].Command)
		}
	})
	t.Run("folders point to a removed file manager", func(t *testing.T) {
		f := fake(map[string]string{q("x-scheme-handler/https"): "firefox.desktop\n", q("inode/directory"): "nemo.desktop\n"},
			"/usr/share/applications/firefox.desktop")
		expect(t, run(t, Defaults, f, gnome), doctor.Warn, "Folders open nothing")
	})
	t.Run("no desktop", func(t *testing.T) {
		expect(t, run(t, Defaults, fake(map[string]string{q("x-scheme-handler/https"): ""}), &doctor.Host{}), doctor.Skipped, "No desktop session")
	})
}
