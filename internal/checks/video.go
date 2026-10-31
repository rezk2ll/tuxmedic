package checks

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Video = doctor.Check{
	ID:       "video",
	Name:     "Video playback",
	Category: "Performance",
	Question: "Why do videos stutter, or make the fans spin up?",
	Run:      runVideo,
}

// vaDrivers maps a kernel GPU driver to the VA-API drivers that can decode
// video on it, and the package that ships the first one.
var vaDrivers = map[string]struct {
	files []string
	need  string
	brand string
}{
	"i915":    {[]string{"iHD", "i965"}, "va-intel", "Intel"},
	"xe":      {[]string{"iHD"}, "va-intel", "Intel"},
	"amdgpu":  {[]string{"radeonsi"}, "va-mesa", "AMD"},
	"radeon":  {[]string{"radeonsi", "r600"}, "va-mesa", "AMD"},
	"nouveau": {[]string{"nouveau"}, "va-mesa", "NVIDIA"},
	"nvidia":  {[]string{"nvidia"}, "va-nvidia", "NVIDIA"},
}

func runVideo(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Desktop == "" || h.Container {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No desktop session, so no video playback to check"}}
	}
	have := map[string]bool{}
	for _, pat := range []string{"/usr/lib/*-linux-gnu/dri*/*_drv_video.so", "/usr/lib*/dri*/*_drv_video.so"} {
		fs, _ := s.Glob(pat)
		for _, f := range fs {
			have[strings.TrimSuffix(filepath.Base(f), "_drv_video.so")] = true
		}
	}
	if p := s.Getenv("LIBVA_DRIVERS_PATH"); p != "" {
		fs, _ := s.Glob(p + "/*_drv_video.so")
		for _, f := range fs {
			have[strings.TrimSuffix(filepath.Base(f), "_drv_video.so")] = true
		}
	}

	cards, _ := s.Glob("/sys/class/drm/card[0-9]*/device/uevent")
	seen := map[string]bool{}
	var missing []string
	var fixes []doctor.Fix
	for _, c := range cards {
		b, _ := s.ReadFile(c)
		d := parseKV(string(b))["DRIVER"]
		va, ok := vaDrivers[d]
		if !ok || seen[d] {
			continue
		}
		seen[d] = true
		found := false
		for _, f := range va.files {
			found = found || have[f]
		}
		if found {
			// One working chip is enough: browsers decode on the integrated one.
			return []doctor.Finding{{Status: doctor.OK, Title: "Videos are decoded by your " + va.brand + " graphics"}}
		}
		missing = append(missing, va.brand)
		fixes = append(fixes, installFix(h, va.need))
	}
	if len(missing) == 0 {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No graphics chip that tuxmedic knows how to check for video decoding"}}
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  "Videos are decoded by the processor: the driver for " + strings.Join(missing, " and ") + " video decoding isn't installed",
		Explain: "Without it, browsers and video players do all the work on the processor. Videos can stutter at high resolutions, " +
			"the fans spin up, and a laptop battery drains faster.",
		Fixes: fixes,
	}}
}
