package checks

import (
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestNomodeset(t *testing.T) {
	gpu := "/sys/bus/pci/devices/0000:00:02.0/"
	fake := func(cmdline string, tools ...string) *sys.Fake {
		f := &sys.Fake{
			Files:    files(gpu+"class", "0x030000\n", gpu+"vendor", "0x8086\n", gpu+"device", "0x46a6\n", gpu+"uevent", "PCI_ID=8086:46A6\n", "/proc/cmdline", cmdline),
			Commands: map[string]string{},
		}
		for _, t := range tools {
			f.Commands[t] = ""
		}
		return f
	}
	ubuntu := &doctor.Host{ID: "ubuntu", PackageMgr: "apt"}

	r := run(t, Drivers, fake("BOOT_IMAGE=/vmlinuz root=UUID=0000 ro quiet splash nomodeset\n", "update-grub"), ubuntu)
	expect(t, r, doctor.Warn, "switched off by the nomodeset startup setting")
	if len(r.Findings) != 1 {
		t.Errorf("the driverless GPU should not get its own finding: %+v", r.Findings)
	}
	if c := r.Findings[0].Fixes[0].Command; c != `sudo sed -i '/^GRUB_CMDLINE_LINUX/s/ *\bnomodeset\b//' /etc/default/grub && sudo update-grub` {
		t.Errorf("fix = %q", c)
	}
	fedora := &doctor.Host{ID: "fedora", PackageMgr: "dnf"}
	if c := run(t, Drivers, fake("ro rhgb quiet nomodeset\n", "grubby"), fedora).Findings[0].Fixes[0].Command; c != "sudo grubby --update-kernel=ALL --remove-args=nomodeset" {
		t.Errorf("fedora fix = %q", c)
	}
	if r := run(t, Drivers, fake("ro quiet nomodeset\n"), &doctor.Host{Virtual: "kvm"}); strings.Contains(r.Findings[0].Title, "nomodeset") {
		t.Errorf("virtual machine: %+v", r.Findings)
	}
}

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
