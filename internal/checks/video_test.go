package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestVideo(t *testing.T) {
	ubuntu := &doctor.Host{ID: "ubuntu", PackageMgr: "apt", Desktop: "GNOME"}
	card := func(n, drv string) (string, string) {
		return "/sys/class/drm/card" + n + "/device/uevent", "DRIVER=" + drv + "\nPCI_CLASS=30000\n"
	}
	fake := func(files ...string) *sys.Fake {
		f := &sys.Fake{Files: map[string]string{}}
		for i := 0; i+1 < len(files); i += 2 {
			f.Files[files[i]] = files[i+1]
		}
		return f
	}
	intel, intelUevent := card("0", "i915")
	nv, nvUevent := card("1", "nvidia")

	t.Run("intel with iHD", func(t *testing.T) {
		f := fake(intel, intelUevent, "/usr/lib/x86_64-linux-gnu/dri/iHD_drv_video.so", "")
		expect(t, run(t, Video, f, ubuntu), doctor.OK, "decoded by your Intel graphics")
	})
	t.Run("intel without a driver", func(t *testing.T) {
		r := run(t, Video, fake(intel, intelUevent), ubuntu)
		expect(t, r, doctor.Info, "the driver for Intel video decoding isn't installed")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo apt install intel-media-va-driver" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("hybrid laptop, integrated chip decodes", func(t *testing.T) {
		f := fake(intel, intelUevent, nv, nvUevent, "/usr/lib64/dri/i965_drv_video.so", "")
		expect(t, run(t, Video, f, &doctor.Host{ID: "fedora", PackageMgr: "dnf", Desktop: "KDE"}), doctor.OK, "Intel")
	})
	t.Run("amd on fedora without mesa va", func(t *testing.T) {
		a, au := card("0", "amdgpu")
		r := run(t, Video, fake(a, au), &doctor.Host{ID: "fedora", PackageMgr: "dnf", Desktop: "GNOME"})
		if c := r.Findings[0].Fixes[0].Command; c != "sudo dnf install mesa-va-drivers" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("custom LIBVA_DRIVERS_PATH", func(t *testing.T) {
		f := fake(nv, nvUevent, "/opt/va/nvidia_drv_video.so", "")
		f.Env = map[string]string{"LIBVA_DRIVERS_PATH": "/opt/va"}
		expect(t, run(t, Video, f, ubuntu), doctor.OK, "NVIDIA")
	})
	t.Run("unknown chip", func(t *testing.T) {
		v, vu := card("0", "virtio_gpu")
		expect(t, run(t, Video, fake(v, vu), ubuntu), doctor.Skipped, "No graphics chip")
	})
	t.Run("no desktop", func(t *testing.T) {
		expect(t, run(t, Video, fake(intel, intelUevent), &doctor.Host{}), doctor.Skipped, "No desktop session")
	})
}
