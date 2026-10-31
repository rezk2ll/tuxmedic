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
	return append(vaapi(s, h), fedoraCodecs(ctx, s, h)...)
}

// fedoraCodecs catches Fedora's ffmpeg-free, which leaves out H.264, HEVC
// and AAC: many videos and streams then won't play outside Firefox.
func fedoraCodecs(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.ID != "fedora" || h.PackageMgr != "dnf" || !s.Has("rpm") {
		return nil
	}
	out, _ := s.Run(ctx, "rpm", "-q", "ffmpeg-libs", "libavcodec-freeworld")
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" && !strings.Contains(l, "not installed") {
			return nil
		}
	}
	cmd := "sudo dnf swap ffmpeg-free ffmpeg --allowerasing"
	if !s.Exists("/etc/yum.repos.d/rpmfusion-free.repo") {
		cmd = "sudo dnf install https://mirrors.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm && " + cmd
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  "Many videos won't play: Fedora leaves out the H.264 and HEVC codecs",
		Explain: "For patent reasons Fedora ships a reduced ffmpeg. Videos from phones and cameras, many streams and some sites show an error " +
			"or play without sound. The full ffmpeg comes from RPM Fusion, the repository most Fedora users add.",
		Fixes: []doctor.Fix{{Title: "Add RPM Fusion and switch to the full ffmpeg", Command: cmd, NeedsRoot: true, URL: "https://rpmfusion.org/Howto/Multimedia"}},
	}}
}

func vaapi(s sys.System, h *doctor.Host) []doctor.Finding {
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
