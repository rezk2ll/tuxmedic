package checks

import (
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

func TestDisplayInContainer(t *testing.T) {
	r := run(t, Display, &sys.Fake{}, &doctor.Host{Container: true})
	expect(t, r, doctor.Skipped, "No screens to check")
}
