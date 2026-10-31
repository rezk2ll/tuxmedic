package checks

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Camera = doctor.Check{
	ID:       "camera",
	Name:     "Camera & microphone",
	Category: "Devices",
	Question: "Why doesn't my webcam or microphone work in video calls?",
	Run:      runCamera,
}

func runCamera(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	var fs []doctor.Finding
	fs = append(fs, cameras(s, h)...)
	fs = append(fs, privacySwitches(ctx, s, h)...)
	fs = append(fs, microphone(ctx, s)...)
	return fs
}

func cameras(s sys.System, h *doctor.Host) []doctor.Finding {
	nodes, _ := s.Glob("/sys/class/video4linux/video*")
	if len(nodes) == 0 {
		if h.Container || h.Virtual != "" {
			return []doctor.Finding{{Status: doctor.Skipped, Title: "No camera in this virtual machine or container"}}
		}
		if intelIPU(s) {
			return []doctor.Finding{{
				Status: doctor.Warn,
				Title:  "Your laptop's camera needs a driver that isn't set up",
				Explain: "Many laptops with Intel 12th generation or newer processors use a MIPI camera behind Intel's image processor (IPU). " +
					"It doesn't work like a USB webcam: it needs the IPU driver, firmware and a camera service, which only some distros set up automatically.",
				Fixes: []doctor.Fix{
					{Title: "Install every update and restart; newer kernels support more of these cameras"},
					{Title: "Search your distro's documentation for \"IPU6 camera\""},
					{Title: "A USB webcam works in the meantime"},
				},
			}}
		}
		f := doctor.Finding{Status: doctor.Info, Title: "No camera found"}
		if h.Laptop {
			f.Explain = "Linux sees no camera at all. Some laptops have a key (often F8, F9 or F10 with a camera icon) or a setting in the firmware (BIOS) that turns the camera off completely."
		}
		return []doctor.Finding{f}
	}

	var names []string
	devs := map[string]bool{}
	for _, n := range nodes {
		// Most webcams create two nodes; only index 0 is the picture.
		if readTrim(s, n+"/index") != "0" && readTrim(s, n+"/index") != "" {
			continue
		}
		name, _, _ := strings.Cut(readTrim(s, n+"/name"), ":")
		names = append(names, name)
		devs["/dev/"+filepath.Base(n)] = true
	}
	if users := cameraUsers(s, devs); len(users) > 0 {
		return []doctor.Finding{{
			Status: doctor.Info,
			Title:  "Your camera is in use by " + joinAnd(users),
			Explain: "Only one app can use the camera at a time. If a video call shows no camera, close the other app " +
				"(browser tabs count too) and try again.",
		}}
	}
	return []doctor.Finding{{Status: doctor.OK, Title: "Camera ready: " + joinAnd(dedupe(names))}}
}

// intelIPU reports an Intel image processing unit (PCI class 0x0480xx, vendor 8086).
func intelIPU(s sys.System) bool {
	devs, _ := s.Glob("/sys/bus/pci/devices/*/class")
	for _, c := range devs {
		dir := filepath.Dir(c)
		if strings.HasPrefix(readTrim(s, c), "0x0480") && readTrim(s, dir+"/vendor") == "0x8086" {
			return true
		}
	}
	return false
}

// cameraUsers names the processes holding a camera open. Without root only the
// user's own processes are visible, which covers the browser and call apps.
func cameraUsers(s sys.System, devs map[string]bool) []string {
	fds, _ := s.Glob("/proc/[0-9]*/fd/*")
	seen := map[string]bool{}
	for _, fd := range fds {
		t, err := s.Readlink(fd)
		if err != nil || !devs[t] {
			continue
		}
		pid := strings.Split(fd, "/")[2]
		// PipeWire keeps cameras open to offer them to apps; that alone isn't a conflict.
		if c := readTrim(s, "/proc/"+pid+"/comm"); c != "" && c != "pipewire" && c != "wireplumber" {
			seen[c] = true
		}
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func privacySwitches(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !s.Has("gsettings") || !strings.Contains(strings.ToLower(h.Desktop), "gnome") {
		return nil
	}
	var fs []doctor.Finding
	for _, k := range []struct{ key, what string }{
		{"disable-camera", "camera"},
		{"disable-microphone", "microphone"},
	} {
		out, err := s.Run(ctx, "gsettings", "get", "org.gnome.desktop.privacy", k.key)
		if err != nil || strings.TrimSpace(string(out)) != "true" {
			continue
		}
		fs = append(fs, doctor.Finding{
			Status:  doctor.Fail,
			Title:   "The " + k.what + " is turned off in your privacy settings",
			Explain: "GNOME blocks every app from the " + k.what + " while this privacy switch is off.",
			Fixes: []doctor.Fix{
				{Title: "Turn it on in Settings, under Privacy"},
				{Title: "Or turn it on from here", Command: "gsettings set org.gnome.desktop.privacy " + k.key + " false"},
			},
		})
	}
	return fs
}

func microphone(ctx context.Context, s sys.System) []doctor.Finding {
	if !s.Has("pactl") {
		return nil
	}
	out, err := s.Run(ctx, "pactl", "-f", "json", "list", "sources")
	if err != nil {
		return nil
	}
	var sources []paSink
	if json.Unmarshal(out, &sources) != nil {
		return nil
	}
	var mics []paSink
	for _, src := range sources {
		if !strings.HasSuffix(src.Name, ".monitor") {
			mics = append(mics, src)
		}
	}
	if len(mics) == 0 {
		return []doctor.Finding{{
			Status:  doctor.Info,
			Title:   "No microphone found",
			Explain: "No app can record sound. Plug in a headset with a microphone, or check the Sound check for missing firmware.",
		}}
	}
	def, _ := s.Run(ctx, "pactl", "get-default-source")
	name := strings.TrimSpace(string(def))
	if strings.HasSuffix(name, ".monitor") {
		return []doctor.Finding{{
			Status:  doctor.Warn,
			Title:   "Apps record what your speakers play instead of your microphone",
			Explain: "The default input is set to a \"Monitor of\" device, which captures the computer's own sound output.",
			Fixes: []doctor.Fix{
				{Title: "Pick your microphone as the input in your sound settings"},
				{Title: "Or switch to it from here", Command: "pactl set-default-source " + mics[0].Name},
			},
		}}
	}
	for _, m := range mics {
		if m.Name != name {
			continue
		}
		label := m.Description
		switch {
		case m.Mute:
			return []doctor.Finding{{
				Status:  doctor.Warn,
				Title:   "Your microphone (" + label + ") is muted",
				Explain: "Apps get silence from it. Some laptops have a microphone mute key with a light that shows this.",
				Fixes:   []doctor.Fix{{Title: "Unmute it", Command: "pactl set-source-mute @DEFAULT_SOURCE@ 0"}},
			}}
		case m.volume() < 10:
			return []doctor.Finding{{
				Status:  doctor.Warn,
				Title:   "Your microphone (" + label + ") is turned almost all the way down",
				Explain: "People in calls won't hear you, or will hear you very faintly.",
				Fixes:   []doctor.Fix{{Title: "Turn it up", Command: "pactl set-source-volume @DEFAULT_SOURCE@ 100%"}},
			}}
		}
		return append(silentRecorders(ctx, s), doctor.Finding{Status: doctor.OK, Title: "Microphone ready: " + label})
	}
	return nil
}
