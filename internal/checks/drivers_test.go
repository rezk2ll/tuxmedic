package checks

import (
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestNvidiaModeset(t *testing.T) {
	gpu := "/sys/bus/pci/devices/0000:01:00.0/"
	fake := func(modeset string) *sys.Fake {
		return &sys.Fake{
			Files: map[string]string{
				gpu + "class":                 "0x030000\n",
				gpu + "vendor":                "0x10de\n",
				gpu + "device":                "0x2860\n",
				gpu + "uevent":                "DRIVER=nvidia\n",
				"/proc/driver/nvidia/version": "NVRM version: synthetic\n",
				"/sys/module/nvidia_drm/parameters/modeset": modeset + "\n",
			},
			Commands: map[string]string{"grubby": ""},
		}
	}
	wayland := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Session: "wayland", Systemd: true}

	r := run(t, Drivers, fake("N"), wayland)
	expect(t, r, doctor.Warn, "NVIDIA kernel mode setting is off")
	if c := r.Findings[0].Fixes[0].Command; !strings.Contains(c, "grubby --update-kernel=ALL --args=nvidia-drm.modeset=1") {
		t.Errorf("fix = %q", c)
	}
	expect(t, run(t, Drivers, fake("Y"), wayland), doctor.OK, "All your hardware has a driver")
}
