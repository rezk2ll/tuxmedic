package checks

import (
	"strings"
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
	fedora := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Desktop: "GNOME"}
	codecs := func(rpmq string, extra ...string) *sys.Fake {
		f := fake(append([]string{intel, intelUevent, "/usr/lib64/dri/iHD_drv_video.so", ""}, extra...)...)
		f.Commands = map[string]string{"rpm -q ffmpeg-libs libavcodec-freeworld": rpmq}
		return f
	}
	t.Run("fedora with ffmpeg-free only", func(t *testing.T) {
		r := run(t, Video, codecs("package ffmpeg-libs is not installed\npackage libavcodec-freeworld is not installed\n"), fedora)
		expect(t, r, doctor.Info, "Fedora leaves out the H.264 and HEVC codecs")
		if c := r.Findings[0].Fixes[0].Command; !strings.HasPrefix(c, "sudo dnf install https://mirrors.rpmfusion.org/free/fedora/") {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("fedora with rpm fusion but no swap yet", func(t *testing.T) {
		r := run(t, Video, codecs("package ffmpeg-libs is not installed\npackage libavcodec-freeworld is not installed\n",
			"/etc/yum.repos.d/rpmfusion-free.repo", "[rpmfusion-free]\n"), fedora)
		if c := r.Findings[0].Fixes[0].Command; c != "sudo dnf swap ffmpeg-free ffmpeg --allowerasing" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("fedora with freeworld codecs", func(t *testing.T) {
		r := run(t, Video, codecs("package ffmpeg-libs is not installed\nlibavcodec-freeworld-7.1.1-3.fc43.x86_64\n"), fedora)
		expect(t, r, doctor.OK, "decoded by your Intel graphics")
		if len(r.Findings) != 1 {
			t.Errorf("findings = %+v", r.Findings)
		}
	})
	suse := &doctor.Host{ID: "opensuse-tumbleweed", PackageMgr: "zypper", Desktop: "KDE"}
	t.Run("opensuse without packman", func(t *testing.T) {
		f := fake(intel, intelUevent, "/usr/lib64/dri/iHD_drv_video.so", "", "/etc/zypp/repos.d/repo-oss.repo", "[repo-oss]\nname=Main Repository\n")
		expect(t, run(t, Video, f, suse), doctor.Info, "openSUSE leaves out the H.264 and HEVC codecs")
	})
	t.Run("opensuse with packman", func(t *testing.T) {
		f := fake(intel, intelUevent, "/usr/lib64/dri/iHD_drv_video.so", "",
			"/etc/zypp/repos.d/packman-essentials.repo", "[packman-essentials]\nbaseurl=https://ftp.gwdg.de/pub/linux/misc/packman/suse/openSUSE_Tumbleweed/Essentials\n")
		expect(t, run(t, Video, f, suse), doctor.OK, "Intel")
	})
	t.Run("no desktop", func(t *testing.T) {
		expect(t, run(t, Video, fake(intel, intelUevent), &doctor.Host{}), doctor.Skipped, "No desktop session")
	})
}
