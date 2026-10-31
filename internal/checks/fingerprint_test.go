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
	t.Run("apt installs the PAM module too", func(t *testing.T) {
		r := run(t, Fingerprint, fpReader(nil), h)
		if got := r.Findings[0].Fixes[0].Command; got != "sudo apt install fprintd libpam-fprintd" {
			t.Errorf("fix = %q", got)
		}
	})
}
