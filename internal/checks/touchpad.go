package checks

import (
	"context"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Touchpad = doctor.Check{
	ID:       "touchpad",
	Name:     "Touchpad",
	Category: "Devices",
	Question: "Why doesn't my touchpad move the pointer or click?",
	Run:      runTouchpad,
}

func inputNames(s sys.System) []string {
	b, err := s.ReadFile("/proc/bus/input/devices")
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(b), "\n") {
		if n, ok := strings.CutPrefix(line, "N: Name="); ok {
			names = append(names, strings.Trim(n, `"`))
		}
	}
	return names
}

func runTouchpad(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !h.Laptop {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Only laptops have a built-in touchpad"}}
	}
	var pad string
	mouse := false
	for _, n := range inputNames(s) {
		l := strings.ToLower(n)
		switch {
		case strings.Contains(l, "touchpad") || strings.Contains(l, "trackpad") || strings.Contains(l, "glidepoint"):
			if pad == "" {
				pad = n
			}
		case strings.Contains(l, "mouse") && !strings.Contains(l, "elan") && !strings.Contains(l, "syna") && !strings.Contains(l, "i2c"):
			mouse = true
		}
	}
	if pad == "" {
		return []doctor.Finding{{
			Status: doctor.Warn,
			Title:  "Linux doesn't see your touchpad",
			Explain: "No touchpad shows up among the input devices. On many recent laptops the touchpad is connected over I2C and needs a newer kernel. " +
				"Some laptops also have a firmware (BIOS) setting for the touchpad, or a key that turns it off.",
			Fixes: []doctor.Fix{
				{Title: "Press the touchpad key (often F6, F7 or F9 with a touchpad icon) and try again"},
				{Title: "Install every update and restart, to get a newer kernel"},
				{Title: "In the firmware (BIOS) settings, set the touchpad to enabled, or from Basic to Advanced/Precision if it offers that"},
			},
		}}
	}

	if s.Has("gsettings") && strings.Contains(strings.ToLower(h.Desktop), "gnome") {
		out, _ := s.Run(ctx, "gsettings", "get", "org.gnome.desktop.peripherals.touchpad", "send-events")
		switch strings.Trim(strings.TrimSpace(string(out)), "'") {
		case "disabled":
			return []doctor.Finding{{
				Status:  doctor.Fail,
				Title:   "The touchpad is turned off in your settings",
				Explain: "The touchpad works, but GNOME ignores it. A touchpad key on the keyboard can switch this by accident.",
				Fixes: []doctor.Fix{
					{Title: "Turn it on in Settings, under Mouse & Touchpad"},
					{Title: "Or turn it on from here", Command: "gsettings set org.gnome.desktop.peripherals.touchpad send-events enabled"},
				},
			}}
		case "disabled-on-external-mouse":
			if mouse {
				return []doctor.Finding{{
					Status:  doctor.Info,
					Title:   "The touchpad is off while a mouse is plugged in",
					Explain: "Your settings turn the touchpad off whenever a mouse is connected. Unplug the mouse, or change the setting.",
					Fixes:   []doctor.Fix{{Title: "Keep the touchpad on with a mouse", Command: "gsettings set org.gnome.desktop.peripherals.touchpad send-events enabled"}},
				}}
			}
		}
	}
	return []doctor.Finding{{Status: doctor.OK, Title: "Touchpad found: " + pad}}
}
