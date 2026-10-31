package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func fpReader(cmds map[string]string) *sys.Fake {
	return &sys.Fake{
		Files: map[string]string{
			"/sys/bus/usb/devices/3-3/idVendor":                "06cb\n",
			"/sys/bus/usb/devices/3-3/idProduct":               "00bd\n",
			"/sys/bus/usb/devices/3-3/3-3:1.0/bInterfaceClass": "ff\n",
			// A touchscreen from a fingerprint vendor: HID, so not a reader.
			"/sys/bus/usb/devices/1-4/idVendor":                "04f3\n",
			"/sys/bus/usb/devices/1-4/idProduct":               "2234\n",
			"/sys/bus/usb/devices/1-4/1-4:1.0/bInterfaceClass": "03\n",
			"/etc/pam.d/common-auth": "auth\t[success=2 default=ignore]\tpam_fprintd.so max-tries=1 timeout=10\n" +
				"auth\t[success=1 default=ignore]\tpam_unix.so nullok try_first_pass\n",
		},
		Commands: cmds,
	}
}

func TestFingerprint(t *testing.T) {
	h := &doctor.Host{ID: "ubuntu", PackageMgr: "apt", User: "alex"}
	tests := []struct {
		name string
		cmds map[string]string
		st   doctor.Status
		want string
	}{
		{"not installed", nil, doctor.Warn, "Fingerprint support isn't installed"},
		{"unsupported", map[string]string{"fprintd-list alex": "No devices available\n"}, doctor.Warn,
			"Linux doesn't support your Synaptics fingerprint reader (06cb:00bd) yet"},
		{"nothing enrolled", map[string]string{"fprintd-list alex": "found 1 devices\nDevice at /net/reactivated/Fprint/Device/0\nUser alex has no fingers enrolled for Synaptics Sensors.\n"},
			doctor.Info, "No fingerprint is saved yet"},
		{"enrolled", map[string]string{"fprintd-list alex": "found 1 devices\nFingerprints for user alex on Synaptics Sensors (press):\n - #0: right-index-finger\n"},
			doctor.OK, "Fingerprint login is set up"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expect(t, run(t, Fingerprint, fpReader(tc.cmds), h), tc.st, tc.want)
		})
	}

	t.Run("touchscreen only", func(t *testing.T) {
		f := fpReader(nil)
		delete(f.Files, "/sys/bus/usb/devices/3-3/idVendor")
		expect(t, run(t, Fingerprint, f, h), doctor.Skipped, "No fingerprint reader")
	})
	enrolled := map[string]string{"fprintd-list alex": tests[3].cmds["fprintd-list alex"]}
	t.Run("enrolled, but PAM never asks for it", func(t *testing.T) {
		cmds := map[string]string{"pam-auth-update": ""}
		for k, v := range enrolled {
			cmds[k] = v
		}
		f := fpReader(cmds)
		f.Files["/etc/pam.d/common-auth"] = "auth\t[success=1 default=ignore]\tpam_unix.so nullok\n"
		f.Files["/etc/pam.d/gdm-fingerprint"] = "auth\trequisite\tpam_nologin.so\nauth\trequired\tpam_fprintd.so\n"
		r := run(t, Fingerprint, f, h)
		expect(t, r, doctor.Info, "works at the login screen, but sudo still asks for a password")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo pam-auth-update --enable fprintd" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("enrolled on Fedora without the authselect feature", func(t *testing.T) {
		cmds := map[string]string{"authselect current": "Profile ID: local\n"}
		for k, v := range enrolled {
			cmds[k] = v
		}
		f := fpReader(cmds)
		delete(f.Files, "/etc/pam.d/common-auth")
		r := run(t, Fingerprint, f, &doctor.Host{ID: "fedora", PackageMgr: "dnf", User: "alex"})
		expect(t, r, doctor.Info, "Your fingerprint is saved, but nothing asks for it")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo authselect enable-feature with-fingerprint && sudo authselect apply-changes" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("apt installs the PAM module too", func(t *testing.T) {
		r := run(t, Fingerprint, fpReader(nil), h)
		if got := r.Findings[0].Fixes[0].Command; got != "sudo apt install fprintd libpam-fprintd" {
			t.Errorf("fix = %q", got)
		}
	})
}
