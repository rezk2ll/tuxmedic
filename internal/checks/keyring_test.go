package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestKeyring(t *testing.T) {
	gnome := &doctor.Host{ID: "ubuntu", Desktop: "GNOME"}
	keyring := "/home/alex/.local/share/keyrings/login.keyring"
	fake := func(files ...string) *sys.Fake {
		f := &sys.Fake{Files: map[string]string{}, Env: map[string]string{"HOME": "/home/alex"}}
		for i := 0; i+1 < len(files); i += 2 {
			f.Files[files[i]] = files[i+1]
		}
		return f
	}

	t.Run("gdm automatic login with a keyring", func(t *testing.T) {
		f := fake("/etc/gdm3/custom.conf", "[daemon]\nAutomaticLoginEnable=True\nAutomaticLogin=alex\n", keyring, "GnomeKeyring\n")
		r := run(t, Keyring, f, gnome)
		expect(t, r, doctor.Info, "apps ask you to unlock the login keyring")
		if ev := r.Findings[0].Evidence; len(ev) != 1 || ev[0] != "GDM automatic login: /etc/gdm3/custom.conf" {
			t.Errorf("evidence = %v", ev)
		}
	})
	t.Run("commented out", func(t *testing.T) {
		f := fake("/etc/gdm3/custom.conf", "[daemon]\n#AutomaticLoginEnable=true\n", keyring, "GnomeKeyring\n")
		expect(t, run(t, Keyring, f, gnome), doctor.OK, "Your password unlocks the keyring")
	})
	t.Run("lightdm drop-in", func(t *testing.T) {
		f := fake("/etc/lightdm/lightdm.conf.d/50-autologin.conf", "[Seat:*]\nautologin-user=alex\n", keyring, "GnomeKeyring\n")
		r := run(t, Keyring, f, &doctor.Host{ID: "linuxmint", Desktop: "X-Cinnamon"})
		expect(t, r, doctor.Info, "unlock the login keyring")
		if ev := r.Findings[0].Evidence[0]; ev != "LightDM automatic login: /etc/lightdm/lightdm.conf.d/50-autologin.conf" {
			t.Errorf("evidence = %q", ev)
		}
	})
	t.Run("lightdm with an empty autologin-user", func(t *testing.T) {
		f := fake("/etc/lightdm/lightdm.conf", "[Seat:*]\nautologin-user=\n", keyring, "GnomeKeyring\n")
		expect(t, run(t, Keyring, f, gnome), doctor.OK, "unlocks")
	})
	t.Run("sddm with a kde wallet", func(t *testing.T) {
		f := fake("/etc/sddm.conf.d/kde_settings.conf", "[Autologin]\nRelogin=false\nUser=alex\n",
			"/home/alex/.local/share/kwalletd/kdewallet.kwl", "KWALLET\n")
		expect(t, run(t, Keyring, f, &doctor.Host{ID: "fedora", Desktop: "KDE"}), doctor.Info, "unlock the KDE wallet")
	})
	t.Run("sddm User= outside Autologin", func(t *testing.T) {
		f := fake("/etc/sddm.conf", "[Users]\nUser=alex\n", keyring, "GnomeKeyring\n")
		expect(t, run(t, Keyring, f, gnome), doctor.OK, "unlocks")
	})
	t.Run("automatic login, no keyring yet", func(t *testing.T) {
		f := fake("/etc/gdm/custom.conf", "[daemon]\nAutomaticLoginEnable=true\n")
		expect(t, run(t, Keyring, f, gnome), doctor.OK, "no keyring that needs a password")
	})
}
