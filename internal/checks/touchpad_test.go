package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

const inputs = `I: Bus=0018 Vendor=04f3 Product=3195 Version=0100
N: Name="ELAN0678:00 04F3:3195 Mouse"
H: Handlers=mouse0 event4

I: Bus=0018 Vendor=04f3 Product=3195 Version=0100
N: Name="ELAN0678:00 04F3:3195 Touchpad"
H: Handlers=mouse1 event5
`

func TestTouchpad(t *testing.T) {
	laptop := &doctor.Host{ID: "fedora", Laptop: true, Desktop: "GNOME"}
	usbMouse := inputs + "\nN: Name=\"Logi USB Optical Mouse\"\n"

	t.Run("found", func(t *testing.T) {
		f := &sys.Fake{Files: map[string]string{"/proc/bus/input/devices": inputs}}
		expect(t, run(t, Touchpad, f, laptop), doctor.OK, "Touchpad found: ELAN0678:00 04F3:3195 Touchpad")
	})
	t.Run("missing", func(t *testing.T) {
		f := &sys.Fake{Files: map[string]string{"/proc/bus/input/devices": "N: Name=\"AT Translated Set 2 keyboard\"\n"}}
		expect(t, run(t, Touchpad, f, laptop), doctor.Warn, "Linux doesn't see your touchpad")
	})
	t.Run("turned off in GNOME", func(t *testing.T) {
		f := &sys.Fake{
			Files:    map[string]string{"/proc/bus/input/devices": inputs},
			Commands: map[string]string{"gsettings get org.gnome.desktop.peripherals.touchpad send-events": "'disabled'\n"},
		}
		expect(t, run(t, Touchpad, f, laptop), doctor.Fail, "The touchpad is turned off in your settings")
	})
	t.Run("off with a mouse, and a mouse is plugged in", func(t *testing.T) {
		f := &sys.Fake{
			Files:    map[string]string{"/proc/bus/input/devices": usbMouse},
			Commands: map[string]string{"gsettings get org.gnome.desktop.peripherals.touchpad send-events": "'disabled-on-external-mouse'\n"},
		}
		expect(t, run(t, Touchpad, f, laptop), doctor.Info, "off while a mouse is plugged in")
	})
	t.Run("off with a mouse, but only the touchpad's own mouse node", func(t *testing.T) {
		f := &sys.Fake{
			Files:    map[string]string{"/proc/bus/input/devices": inputs},
			Commands: map[string]string{"gsettings get org.gnome.desktop.peripherals.touchpad send-events": "'disabled-on-external-mouse'\n"},
		}
		expect(t, run(t, Touchpad, f, laptop), doctor.OK, "Touchpad found")
	})
	t.Run("tap to click off", func(t *testing.T) {
		f := &sys.Fake{
			Files:    map[string]string{"/proc/bus/input/devices": inputs},
			Commands: map[string]string{"gsettings get org.gnome.desktop.peripherals.touchpad tap-to-click": "false\n"},
		}
		expect(t, run(t, Touchpad, f, laptop), doctor.Info, "tap to click is off")
	})
	t.Run("turned off in KDE", func(t *testing.T) {
		const q = "busctl --user get-property org.kde.KWin /org/kde/KWin/InputDevice/event5 org.kde.KWin.InputDevice enabled"
		f := &sys.Fake{Files: map[string]string{"/proc/bus/input/devices": inputs}, Commands: map[string]string{q: "b false\n"}}
		kde := &doctor.Host{Laptop: true, Desktop: "KDE", Session: "wayland"}
		r := run(t, Touchpad, f, kde)
		expect(t, r, doctor.Fail, "The touchpad is turned off in your settings")
		if c := r.Findings[0].Fixes[1].Command; c != "busctl --user set-property org.kde.KWin /org/kde/KWin/InputDevice/event5 org.kde.KWin.InputDevice enabled b true" {
			t.Errorf("fix = %q", c)
		}
		f.Commands[q] = "b true\n"
		expect(t, run(t, Touchpad, f, kde), doctor.OK, "Touchpad found")
	})
	t.Run("desktop", func(t *testing.T) {
		expect(t, run(t, Touchpad, &sys.Fake{}, &doctor.Host{}), doctor.Skipped, "Only laptops")
	})
}
