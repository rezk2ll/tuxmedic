package checks

import (
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func edid(name string) string {
	b := make([]byte, 128)
	copy(b[54:], []byte{0, 0, 0, 0xFC, 0})
	copy(b[59:], name+"\n     ")
	return string(b)
}

func screens(extra map[string]string) *sys.Fake {
	files := map[string]string{
		"/sys/class/drm/card1/device/uevent":      "DRIVER=amdgpu\n",
		"/sys/class/drm/card1-eDP-1/status":       "connected\n",
		"/sys/class/drm/card1-eDP-1/enabled":      "enabled\n",
		"/sys/class/drm/card1-eDP-1/modes":        "1920x1200\n1600x1200\n",
		"/sys/class/drm/card1-HDMI-A-1/status":    "disconnected\n",
		"/sys/class/drm/card1-HDMI-A-1/enabled":   "disabled\n",
		"/sys/class/drm/card1-Writeback-1/status": "unknown\n",
	}
	for k, v := range extra {
		files[k] = v
	}
	return &sys.Fake{Files: files}
}

func TestDisplay(t *testing.T) {
	hdmi := "/sys/class/drm/card1-HDMI-A-1/"
	tests := []struct {
		name  string
		files map[string]string
		st    doctor.Status
		want  string
	}{
		{"laptop only", nil, doctor.OK, "Screens in use: the built-in screen at 1920x1200"},
		{"monitor in use", map[string]string{
			hdmi + "status": "connected\n", hdmi + "enabled": "enabled\n",
			hdmi + "modes": "2560x1440\n", hdmi + "edid": edid("ACME 27Q"),
		}, doctor.OK, "ACME 27Q (HDMI) at 2560x1440"},
		{"monitor switched off", map[string]string{
			hdmi + "status": "connected\n", hdmi + "modes": "2560x1440\n", hdmi + "edid": edid("ACME 27Q"),
		}, doctor.Warn, "ACME 27Q (HDMI) is plugged in but switched off"},
		{"bad cable", map[string]string{hdmi + "status": "connected\n"}, doctor.Fail, "can't tell the computer what it supports"},
		{"displaylink without driver", map[string]string{
			"/sys/bus/usb/devices/3-1/idVendor": "17e9\n", "/sys/bus/usb/devices/3-1/product": "USB3.0 Dock\n",
		}, doctor.Fail, "USB3.0 Dock is plugged in, but its driver isn't installed"},
		{"displaylink with driver", map[string]string{
			"/sys/bus/usb/devices/3-1/idVendor": "17e9\n", "/sys/module/evdi/refcnt": "1\n",
		}, doctor.OK, "Screens in use"},
		{"thunderbolt dock not approved", map[string]string{
			"/sys/bus/thunderbolt/devices/0-1/authorized":  "0\n",
			"/sys/bus/thunderbolt/devices/0-1/vendor_name": "Acme\n",
			"/sys/bus/thunderbolt/devices/0-1/device_name": "TB4 Dock\n",
		}, doctor.Warn, "Acme TB4 Dock is connected but not allowed yet"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			expect(t, run(t, Display, screens(tc.files), nil), tc.st, tc.want)
		})
	}
}

func TestBlurryXwayland(t *testing.T) {
	gnome := &doctor.Host{ID: "fedora", Desktop: "GNOME", Session: "wayland"}
	state := "gdbus call --session --dest org.gnome.Mutter.DisplayConfig --object-path /org/gnome/Mutter/DisplayConfig --method org.gnome.Mutter.DisplayConfig.GetCurrentState"
	at := func(scale string) string {
		return "(uint32 7, [(('eDP-1', 'ACME', 'Panel', '0x00000000'), [('1920x1200@60', 1920, 1200, 60.0, 1.0, [1.0, 1.25, 1.5], {})], {})], " +
			"[(0, 0, " + scale + ", uint32 0, true, [('eDP-1', 'ACME', 'Panel', '0x00000000')], @a{sv} {})], {})"
	}
	cmds := func(scale, shell, feats string) *sys.Fake {
		f := screens(nil)
		f.Commands = map[string]string{state: at(scale), "gnome-shell --version": shell, "gsettings get org.gnome.mutter experimental-features": feats}
		return f
	}

	t.Run("125% without native scaling", func(t *testing.T) {
		r := run(t, Display, cmds("1.25", "GNOME Shell 48.2\n", "['scale-monitor-framebuffer', 'variable-refresh-rate']\n"), gnome)
		expect(t, r, doctor.Info, "Some apps look blurry at 125% scaling")
		want := `gsettings set org.gnome.mutter experimental-features "['scale-monitor-framebuffer', 'xwayland-native-scaling', 'variable-refresh-rate']"`
		if got := r.Findings[0].Fixes[0].Command; got != want {
			t.Errorf("fix = %q\nwant  %q", got, want)
		}
	})
	t.Run("already on", func(t *testing.T) {
		r := run(t, Display, cmds("1.5", "GNOME Shell 48.2\n", "['scale-monitor-framebuffer', 'xwayland-native-scaling']\n"), gnome)
		expect(t, r, doctor.OK, "Screens in use")
	})
	t.Run("whole-number scale", func(t *testing.T) {
		expect(t, run(t, Display, cmds("2", "GNOME Shell 48.2\n", "@as []\n"), gnome), doctor.OK, "Screens in use")
	})
	t.Run("GNOME 46 has no such setting", func(t *testing.T) {
		expect(t, run(t, Display, cmds("1.25", "GNOME Shell 46.0\n", "@as []\n"), gnome), doctor.OK, "Screens in use")
	})
	t.Run("empty feature list", func(t *testing.T) {
		r := run(t, Display, cmds("1.75", "GNOME Shell 47.1\n", "@as []\n"), gnome)
		if got := r.Findings[0].Fixes[0].Command; !strings.HasSuffix(got, `"['scale-monitor-framebuffer', 'xwayland-native-scaling']"`) {
			t.Errorf("fix = %q", got)
		}
	})
}

func TestBrightness(t *testing.T) {
	laptop := &doctor.Host{ID: "ubuntu", PackageMgr: "apt", Laptop: true}
	bl := "/sys/class/backlight/intel_backlight/"

	t.Run("no control", func(t *testing.T) {
		f := screens(nil)
		f.Commands = map[string]string{"update-grub --version": ""}
		r := run(t, Display, f, laptop)
		expect(t, r, doctor.Warn, "The screen brightness can't be changed")
		want := `sudo sed -i 's/^GRUB_CMDLINE_LINUX_DEFAULT="/&acpi_backlight=native /' /etc/default/grub && sudo update-grub`
		if got := r.Findings[0].Fixes[0].Command; got != want {
			t.Errorf("fix = %q", got)
		}
	})
	t.Run("turned all the way down", func(t *testing.T) {
		f := screens(map[string]string{bl + "brightness": "12\n", bl + "max_brightness": "19393\n"})
		expect(t, run(t, Display, f, laptop), doctor.Info, "almost at zero")
	})
	t.Run("fine", func(t *testing.T) {
		f := screens(map[string]string{bl + "brightness": "9000\n", bl + "max_brightness": "19393\n"})
		expect(t, run(t, Display, f, laptop), doctor.OK, "Screens in use")
	})
	t.Run("desktop with a monitor only", func(t *testing.T) {
		expect(t, run(t, Display, screens(nil), nil), doctor.OK, "Screens in use")
	})
}

func TestKernelArgFix(t *testing.T) {
	tests := []struct {
		h    *doctor.Host
		f    *sys.Fake
		want string
	}{
		{&doctor.Host{PackageMgr: "rpm-ostree"}, &sys.Fake{}, "sudo rpm-ostree kargs --append=x=1"},
		{&doctor.Host{PackageMgr: "dnf"}, &sys.Fake{Commands: map[string]string{"grubby --info": ""}}, "sudo grubby --update-kernel=ALL --args=x=1"},
		{&doctor.Host{PackageMgr: "apt"}, &sys.Fake{Commands: map[string]string{"kernelstub --help": "", "update-grub --help": ""}}, "sudo kernelstub -a x=1"},
	}
	for _, tc := range tests {
		if got := kernelArgFix(tc.f, tc.h, "x=1").Command; got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.h.PackageMgr, got, tc.want)
		}
	}
	if f := kernelArgFix(&sys.Fake{}, &doctor.Host{PackageMgr: "nix"}, "x=1"); f.Command != "" {
		t.Errorf("nix should get instructions, not a command: %+v", f)
	}
}

func TestDisplayInContainer(t *testing.T) {
	r := run(t, Display, &sys.Fake{}, &doctor.Host{Container: true})
	expect(t, r, doctor.Skipped, "No screens to check")
}
