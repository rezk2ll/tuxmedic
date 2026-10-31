package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func cups(p, o string, extra map[string]string) *sys.Fake {
	cmds := map[string]string{
		"lpstat -r": "scheduler is running\n",
		"lpstat -p": p,
		"lpstat -o": o,
	}
	for k, v := range extra {
		cmds[k] = v
	}
	return &sys.Fake{Commands: cmds}
}

func TestPrinter(t *testing.T) {
	h := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Systemd: true}
	idle := "printer Office is idle.  enabled since Thu 30 Oct 2026 10:00:00 AM UTC\n"

	t.Run("ready", func(t *testing.T) {
		expect(t, run(t, Printer, cups(idle, "", nil), h), doctor.OK, "Ready to print: Office")
	})
	t.Run("paused with jobs", func(t *testing.T) {
		r := run(t, Printer, cups("printer Office disabled since Thu 30 Oct 2026 10:00:00 AM UTC -\n\tPaused\n",
			"Office-12  alex  1024   Thu 30 Oct 2026 10:01:00 AM UTC\nOffice-13  alex  2048   Thu 30 Oct 2026 10:02:00 AM UTC\n", nil), h)
		expect(t, r, doctor.Warn, "The printer Office is paused with 2 print jobs waiting")
		if r.Findings[0].Fixes[0].Command != "sudo cupsenable Office" {
			t.Errorf("fix = %q", r.Findings[0].Fixes[0].Command)
		}
	})
	t.Run("stuck job", func(t *testing.T) {
		expect(t, run(t, Printer, cups(idle, "Office-12  alex  1024   Thu 30 Oct 2026 10:01:00 AM UTC\n", nil), h),
			doctor.Warn, "1 print job is waiting for Office, which isn't printing")
	})
	t.Run("printing", func(t *testing.T) {
		expect(t, run(t, Printer, cups("printer Office now printing Office-12.  enabled since Thu 30 Oct\n",
			"Office-12  alex  1024   Thu 30 Oct 2026 10:01:00 AM UTC\n", nil), h), doctor.OK, "Ready to print: Office")
	})
	t.Run("usb printer without ipp-usb", func(t *testing.T) {
		const d = "/sys/bus/usb/devices/1-2"
		f := cups("", "", map[string]string{"lpstat -e": ""})
		f.Files = files(d+"/manufacturer", "Acme", d+"/product", "LaserJet 100",
			d+":1.0/bInterfaceClass", "07", d+":1.0/bInterfaceSubClass", "01", d+":1.0/bInterfaceProtocol", "02",
			d+":1.1/bInterfaceClass", "07", d+":1.1/bInterfaceSubClass", "01", d+":1.1/bInterfaceProtocol", "04",
			d+":1.2/bInterfaceClass", "07", d+":1.2/bInterfaceSubClass", "01", d+":1.2/bInterfaceProtocol", "04")
		r := run(t, Printer, f, h)
		expect(t, r, doctor.Warn, "needs ipp-usb to work without a driver: Acme LaserJet 100")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo dnf install ipp-usb" {
			t.Errorf("fix = %q", c)
		}
		f.Commands["ipp-usb --version"] = ""
		expect(t, run(t, Printer, f, h), doctor.Info, "No printer is set up")
	})
	t.Run("none set up, one on the network", func(t *testing.T) {
		expect(t, run(t, Printer, cups("", "", map[string]string{"lpstat -e": "Acme_LaserJet\n"}), h),
			doctor.Info, "A printer on your network is ready to add: Acme_LaserJet")
	})
	t.Run("none set up, avahi off", func(t *testing.T) {
		r := run(t, Printer, cups("", "", map[string]string{"lpstat -e": "", "systemctl is-active avahi-daemon": "inactive\n"}), h)
		expect(t, r, doctor.Info, "No printer is set up")
		if len(r.Findings[0].Fixes) == 0 || r.Findings[0].Fixes[0].Command != "sudo systemctl enable --now avahi-daemon" {
			t.Errorf("fixes = %+v", r.Findings[0].Fixes)
		}
	})
	t.Run("scheduler stopped", func(t *testing.T) {
		f := &sys.Fake{Commands: map[string]string{"lpstat -r": "scheduler is not running\n"}}
		expect(t, run(t, Printer, f, h), doctor.Warn, "The printing service isn't running")
	})
	t.Run("no cups", func(t *testing.T) {
		expect(t, run(t, Printer, &sys.Fake{}, h), doctor.Skipped, "isn't installed")
	})
}
