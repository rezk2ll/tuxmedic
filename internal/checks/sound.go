package checks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Sound = doctor.Check{
	ID:       "sound",
	Name:     "Sound",
	Category: "Devices",
	Question: "Why is there no sound, or why does it come out of the wrong device?",
	Run:      runSound,
}

type paSink struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Mute        bool   `json:"mute"`
	Volume      map[string]struct {
		Percent string `json:"value_percent"`
	} `json:"volume"`
	ActivePort string `json:"active_port"`
	Ports      []struct {
		Name         string `json:"name"`
		Description  string `json:"description"`
		Availability string `json:"availability"`
	} `json:"ports"`
}

type paCard struct {
	Name          string                     `json:"name"`
	ActiveProfile string                     `json:"active_profile"`
	Profiles      map[string]json.RawMessage `json:"profiles"`
	Properties    map[string]string          `json:"properties"`
}

func (s paSink) volume() int {
	v := 0
	for _, ch := range s.Volume {
		n, _ := strconv.Atoi(strings.TrimSuffix(ch.Percent, "%"))
		v = max(v, n)
	}
	return v
}

func runSound(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	cards, _ := s.ReadFile("/proc/asound/cards")
	noCards := len(cards) > 0 && strings.Contains(string(cards), "no soundcards")
	if h.Container || h.Virtual != "" && len(cards) == 0 {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No sound hardware in this virtual machine or container"}}
	}

	if !s.Has("pactl") {
		if h.SoundServer == "pipewire" {
			return []doctor.Finding{{
				Status:  doctor.Skipped,
				Title:   "Install pactl for a full sound check",
				Explain: "pactl talks to PipeWire and PulseAudio the same way. Most distros ship it in the pulseaudio-utils or libpulse package.",
			}}
		}
		if noCards {
			return []doctor.Finding{noSoundCard(h)}
		}
		return []doctor.Finding{{
			Status:  doctor.Fail,
			Title:   "No sound service is installed",
			Explain: "Apps play sound through a sound service, usually PipeWire. Without one, only a few programs can reach the hardware directly.",
			Fixes:   []doctor.Fix{installFix(h, "pipewire")},
		}}
	}

	out, err := s.Run(ctx, "pactl", "-f", "json", "list", "sinks")
	if err != nil {
		f := doctor.Finding{
			Status:   doctor.Fail,
			Title:    "The sound service isn't running",
			Explain:  "No app can play sound until the sound service runs again. Restarting it is safe and takes a second.",
			Evidence: []string{err.Error()},
		}
		if h.SoundServer == "pulseaudio" || s.Has("pulseaudio") && !s.Has("pipewire") {
			f.Fixes = []doctor.Fix{{Title: "Restart PulseAudio", Command: "systemctl --user restart pulseaudio"}}
		} else {
			f.Fixes = []doctor.Fix{{Title: "Restart PipeWire", Command: "systemctl --user restart pipewire pipewire-pulse wireplumber"}}
		}
		return []doctor.Finding{f}
	}
	var sinks []paSink
	if err := json.Unmarshal(out, &sinks); err != nil {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Couldn't read the list of sound outputs", Evidence: []string{err.Error()}}}
	}

	real := sinks[:0:0]
	for _, k := range sinks {
		if k.Name != "auto_null" && !strings.Contains(strings.ToLower(k.Description), "dummy output") {
			real = append(real, k)
		}
	}
	if len(real) == 0 {
		return []doctor.Finding{noSoundCard(h)}
	}

	var fs []doctor.Finding
	defOut, _ := s.Run(ctx, "pactl", "get-default-sink")
	defName := strings.TrimSpace(string(defOut))
	def := real[0]
	for _, k := range real {
		if k.Name == defName {
			def = k
		}
	}
	setMute, setVol := "pactl set-sink-mute @DEFAULT_SINK@ 0", "pactl set-sink-volume @DEFAULT_SINK@ 70%"
	if s.Has("wpctl") {
		setMute, setVol = "wpctl set-mute @DEFAULT_AUDIO_SINK@ 0", "wpctl set-volume @DEFAULT_AUDIO_SINK@ 0.7"
	}

	name := friendlySink(def)
	switch vol := def.volume(); {
	case def.Mute:
		fs = append(fs, doctor.Finding{
			Status:  doctor.Fail,
			Title:   "Sound is muted on " + name,
			Explain: "The output your apps play to is muted, so nothing comes out.",
			Fixes:   []doctor.Fix{{Title: "Unmute it", Command: setMute}},
		})
	case vol < 5:
		fs = append(fs, doctor.Finding{
			Status:  doctor.Fail,
			Title:   fmt.Sprintf("The volume of %s is at %d%%", name, vol),
			Explain: "It's so low that you won't hear anything.",
			Fixes:   []doctor.Fix{{Title: "Set it to 70%", Command: setVol}},
		})
	default:
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: fmt.Sprintf("Sound plays through %s at %d%%", name, vol)})
	}

	if port := activePort(def); port != nil && port.Availability == "not available" {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   fmt.Sprintf("Sound goes to %s, but nothing is plugged in there", port.Description),
			Explain: "The selected output port reports nothing connected, so you hear nothing even though the volume is up.",
			Fixes:   []doctor.Fix{{Title: "Pick the right output in your sound settings"}},
		})
	}

	if want := configuredDefault(ctx, s); want != "" && want != def.Name {
		fs = append(fs, doctor.Finding{
			Status: doctor.Info,
			Title:  fmt.Sprintf("Your preferred output, %s, isn't connected", prettyNodeName(want)),
			Explain: fmt.Sprintf("You chose it as the default before. While it's away, sound plays through %s instead, and it switches back once you reconnect it.",
				name),
		})
	}

	fs = append(fs, monitorAudio(ctx, s, real)...)
	fs = append(fs, bluetoothCallMode(ctx, s)...)
	return fs
}

func activePort(k paSink) *struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Availability string `json:"availability"`
} {
	for i := range k.Ports {
		if k.Ports[i].Name == k.ActivePort {
			return &k.Ports[i]
		}
	}
	return nil
}

// friendlySink prefers the port ("Speaker", "Headphones") for built-in sound
// cards, whose own names are chip model strings nobody recognises.
func friendlySink(k paSink) string {
	if p := activePort(k); p != nil && p.Description != "" && strings.Contains(k.Name, ".pci-") {
		l := strings.ToLower(p.Description)
		if strings.Contains(l, "hdmi") || strings.Contains(l, "displayport") {
			return "your monitor (" + p.Description + ")"
		}
		return "the built-in " + l
	}
	if k.Description != "" {
		return k.Description
	}
	return prettyNodeName(k.Name)
}

var nodeNoise = regexp.MustCompile(`^(alsa|bluez)_(output|input)\.(usb-|pci-)?|[._-][0-9a-f]{8,}.*$|\.(analog|iec958|hdmi)-.*$`)

// prettyNodeName turns "alsa_output.usb-Acme_Wireless_Headset_2021-00.analog-stereo"
// into "Acme Wireless Headset".
func prettyNodeName(n string) string {
	p := nodeNoise.ReplaceAllString(n, "")
	p = strings.TrimRight(strings.ReplaceAll(p, "_", " "), " -0")
	if p == "" {
		return n
	}
	return p
}

var configuredSinkRE = regexp.MustCompile(`(?m)Audio/Sink\s+(\S+)`)

// configuredDefault is the output the user picked, which WirePlumber remembers
// even while the device is unplugged.
func configuredDefault(ctx context.Context, s sys.System) string {
	if !s.Has("wpctl") {
		return ""
	}
	out, err := s.Run(ctx, "wpctl", "status")
	if err != nil {
		return ""
	}
	_, settings, ok := strings.Cut(string(out), "Default Configured Node Names")
	if !ok {
		return ""
	}
	if m := configuredSinkRE.FindStringSubmatch(settings); m != nil {
		return m[1]
	}
	return ""
}

func monitorAudio(ctx context.Context, s sys.System, sinks []paSink) []doctor.Finding {
	connected := false
	conns, _ := s.Glob("/sys/class/drm/card*-*/status")
	for _, c := range conns {
		if !strings.Contains(c, "-HDMI-") && !strings.Contains(c, "-DP-") {
			continue
		}
		if b, _ := s.ReadFile(c); strings.TrimSpace(string(b)) == "connected" {
			connected = true
		}
	}
	if !connected {
		return nil
	}
	for _, k := range sinks {
		l := strings.ToLower(k.Name + k.Description)
		if strings.Contains(l, "hdmi") || strings.Contains(l, "displayport") {
			return nil
		}
	}
	f := doctor.Finding{
		Status:  doctor.Warn,
		Title:   "Your monitor is connected, but it isn't offered as a sound output",
		Explain: "HDMI and DisplayPort monitors can play sound, but the sound device for the graphics card is switched off or set to a profile without HDMI.",
	}
	out, err := s.Run(ctx, "pactl", "-f", "json", "list", "cards")
	if err == nil {
		var cards []paCard
		if json.Unmarshal(out, &cards) == nil {
			for _, c := range cards {
				if p := bestHDMIProfile(c); p != "" {
					f.Fixes = append(f.Fixes, doctor.Fix{
						Title:   "Turn on the monitor's sound device",
						Command: fmt.Sprintf("pactl set-card-profile %s %s", c.Name, p),
					})
				}
			}
		}
	}
	f.Fixes = append(f.Fixes, doctor.Fix{Title: "Or pick the HDMI / DisplayPort profile in your sound settings"})
	return []doctor.Finding{f}
}

func bestHDMIProfile(c paCard) string {
	if c.ActiveProfile != "off" {
		return ""
	}
	var fallback string
	for p := range c.Profiles {
		lp := strings.ToLower(p)
		if strings.Contains(lp, "hdmi") && strings.Contains(lp, "stereo") {
			return p
		}
		if lp == "hifi" {
			fallback = p
		}
	}
	return fallback
}

func bluetoothCallMode(ctx context.Context, s sys.System) []doctor.Finding {
	out, err := s.Run(ctx, "pactl", "-f", "json", "list", "cards")
	if err != nil {
		return nil
	}
	var cards []paCard
	if json.Unmarshal(out, &cards) != nil {
		return nil
	}
	for _, c := range cards {
		p := strings.ToLower(c.ActiveProfile)
		if !strings.HasPrefix(c.Name, "bluez_card") || !(strings.Contains(p, "headset") || strings.Contains(p, "handsfree") || strings.Contains(p, "hsp") || strings.Contains(p, "hfp")) {
			continue
		}
		dev := firstNonEmpty(c.Properties["device.description"], "Your Bluetooth headset")
		return []doctor.Finding{{
			Status:  doctor.Info,
			Title:   dev + " is in call mode, so music sounds muffled",
			Explain: "Bluetooth headsets switch to a low-quality mode when their microphone is in use. Pick the high fidelity (A2DP) profile when you only listen.",
			Fixes:   []doctor.Fix{{Title: "Switch to high quality", Command: fmt.Sprintf("pactl set-card-profile %s a2dp-sink", c.Name)}},
		}}
	}
	return nil
}

func noSoundCard(h *doctor.Host) doctor.Finding {
	f := doctor.Finding{
		Status: doctor.Fail,
		Title:  "No sound hardware was found (only \"Dummy Output\")",
		Explain: "Linux didn't get the sound card working. On recent laptops this is nearly always the missing Sound Open Firmware (SOF) package, " +
			"which newer Intel and AMD audio chips need.",
	}
	f.Fixes = append(f.Fixes, installFix(h, "sof-firmware"))
	f.Fixes = append(f.Fixes,
		doctor.Fix{Title: "Restart the computer after installing it"},
		doctor.Fix{Title: "Look for audio errors from the kernel", Command: "journalctl -k -b | grep -iE 'sof|snd|hda'"},
	)
	return f
}

// packages maps a generic need to the package name each distro family uses.
var packages = map[string]map[string]string{
	"sof-firmware": {
		"apt": "firmware-sof-signed", "dnf": "alsa-sof-firmware", "pacman": "sof-firmware",
		"zypper": "sof-firmware", "apk": "sof-firmware", "xbps": "sof-firmware",
	},
	"pipewire": {
		"apt": "pipewire-audio", "dnf": "pipewire-pulseaudio", "pacman": "pipewire-pulse",
		"zypper": "pipewire-pulseaudio", "apk": "pipewire-pulse", "xbps": "pipewire",
	},
	"smartmontools": {
		"apt": "smartmontools", "dnf": "smartmontools", "pacman": "smartmontools",
		"zypper": "smartmontools", "apk": "smartmontools", "xbps": "smartmontools",
	},
	"linux-firmware": {
		"apt": "linux-firmware", "dnf": "linux-firmware", "pacman": "linux-firmware",
		"zypper": "kernel-firmware", "apk": "linux-firmware", "xbps": "linux-firmware",
	},
}

func installFix(h *doctor.Host, need string) doctor.Fix {
	pkg := packages[need][h.PackageMgr]
	if pkg == "" {
		pkg = need
	}
	if h.ID == "ubuntu" || h.Like("ubuntu") {
		if need == "sof-firmware" {
			pkg = "linux-firmware"
		}
	}
	cmd := ""
	switch h.PackageMgr {
	case "apt":
		cmd = "sudo apt install " + pkg
	case "dnf":
		cmd = "sudo dnf install " + pkg
	case "pacman":
		cmd = "sudo pacman -S " + pkg
	case "zypper":
		cmd = "sudo zypper install " + pkg
	case "apk":
		cmd = "sudo apk add " + pkg
	case "xbps":
		cmd = "sudo xbps-install " + pkg
	case "nix":
		return doctor.Fix{Title: "Add " + pkg + " to your NixOS configuration and rebuild", URL: "https://search.nixos.org/packages?query=" + pkg}
	}
	if cmd == "" {
		return doctor.Fix{Title: "Install the " + pkg + " package with your package manager"}
	}
	return doctor.Fix{Title: "Install " + pkg, Command: cmd, NeedsRoot: true}
}
