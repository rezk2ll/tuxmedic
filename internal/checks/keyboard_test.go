package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestKeyboard(t *testing.T) {
	gnome := &doctor.Host{ID: "ubuntu", Desktop: "GNOME", Session: "wayland"}
	plasma := &doctor.Host{ID: "arch", Desktop: "KDE", Session: "wayland"}
	proc := func(names ...string) map[string]string {
		m := map[string]string{}
		for i, n := range names {
			m["/proc/"+string(rune('1'+i))+"00/comm"] = n + "\n"
		}
		return m
	}

	t.Run("all fine", func(t *testing.T) {
		f := &sys.Fake{Commands: map[string]string{"gsettings get org.gnome.desktop.a11y.keyboard slowkeys-enable": "false\n"}}
		expect(t, run(t, Keyboard, f, gnome), doctor.OK, "Nothing is slowing down")
	})
	t.Run("slow keys on", func(t *testing.T) {
		f := &sys.Fake{Commands: map[string]string{
			"gsettings get org.gnome.desktop.a11y.keyboard slowkeys-enable":   "true\n",
			"gsettings get org.gnome.desktop.a11y.keyboard stickykeys-enable": "false\n",
		}}
		r := run(t, Keyboard, f, gnome)
		expect(t, r, doctor.Warn, "Slow Keys is turned on")
		if c := r.Findings[0].Fixes[0].Command; c != "gsettings set org.gnome.desktop.a11y.keyboard slowkeys-enable false" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("not on GNOME, gsettings is not asked", func(t *testing.T) {
		f := &sys.Fake{Commands: map[string]string{"gsettings get org.gnome.desktop.a11y.keyboard slowkeys-enable": "true\n"}}
		expect(t, run(t, Keyboard, f, plasma), doctor.OK, "Nothing is slowing down")
	})
	t.Run("fcitx expected but not running", func(t *testing.T) {
		f := &sys.Fake{Env: map[string]string{"GTK_IM_MODULE": "fcitx", "XMODIFIERS": "@im=fcitx"}, Files: proc("plasmashell")}
		r := run(t, Keyboard, f, plasma)
		expect(t, r, doctor.Fail, "Apps expect Fcitx for typing, but it isn't running")
		if r.Findings[0].Fixes[0].Command != "fcitx5 -d" {
			t.Errorf("fix = %+v", r.Findings[0].Fixes)
		}
	})
	t.Run("ibus expected outside GNOME but not running", func(t *testing.T) {
		f := &sys.Fake{Env: map[string]string{"QT_IM_MODULE": "ibus"}, Files: proc("plasmashell")}
		expect(t, run(t, Keyboard, f, plasma), doctor.Fail, "Apps expect IBus")
	})
	t.Run("ibus on GNOME is started by the desktop on demand", func(t *testing.T) {
		f := &sys.Fake{Env: map[string]string{"GTK_IM_MODULE": "ibus"}}
		expect(t, run(t, Keyboard, f, gnome), doctor.OK, "Nothing is slowing down")
	})
	t.Run("both running", func(t *testing.T) {
		f := &sys.Fake{Env: map[string]string{"GTK_IM_MODULE": "ibus"}, Files: proc("ibus-daemon", "fcitx5")}
		expect(t, run(t, Keyboard, f, plasma), doctor.Warn, "Two input methods are running at once")
	})
	t.Run("fcitx on plasma wayland with the old variables", func(t *testing.T) {
		f := &sys.Fake{Env: map[string]string{"GTK_IM_MODULE": "fcitx", "QT_IM_MODULE": "fcitx"}, Files: proc("fcitx5")}
		expect(t, run(t, Keyboard, f, plasma), doctor.Info, "Fcitx works better without GTK_IM_MODULE")
	})
}
