package checks

import (
	"strconv"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

// backlight looks at the built-in screen's brightness control. Without one in
// /sys/class/backlight, the brightness keys and slider do nothing.
func backlight(s sys.System, h *doctor.Host, conns []connector) []doctor.Finding {
	panel := false
	for _, c := range conns {
		if c.builtin() && c.connected {
			panel = true
		}
	}
	if !panel {
		return nil
	}
	dirs, _ := s.Glob("/sys/class/backlight/*")
	if len(dirs) == 0 {
		return []doctor.Finding{{
			Status: doctor.Warn,
			Title:  "The screen brightness can't be changed",
			Explain: "Linux found no brightness control for the built-in screen, so the brightness keys and slider do nothing. " +
				"The laptop's firmware offers several ways to set the brightness, and Linux picked one that doesn't work on this model. A kernel setting picks another.",
			Fixes: []doctor.Fix{
				kernelArgFix(s, h, "acpi_backlight=native"),
				{Title: "If it still doesn't work after the restart, swap that setting for acpi_backlight=video"},
			},
		}}
	}
	for _, d := range dirs {
		cur, err1 := strconv.Atoi(readTrim(s, d+"/brightness"))
		top, err2 := strconv.Atoi(readTrim(s, d+"/max_brightness"))
		if err1 != nil || err2 != nil || top <= 0 {
			continue
		}
		if cur*100/top < 5 {
			f := doctor.Finding{
				Status:  doctor.Info,
				Title:   "The screen brightness is almost at zero",
				Explain: "If the screen looks black or very dark, it may simply be turned all the way down.",
				Fixes:   []doctor.Fix{{Title: "Press the brightness-up key, or use the slider in the system menu"}},
			}
			if s.Has("brightnessctl") {
				f.Fixes = append(f.Fixes, doctor.Fix{Title: "Set it to half from here", Command: "brightnessctl set 50%"})
			}
			return []doctor.Finding{f}
		}
	}
	return nil
}
