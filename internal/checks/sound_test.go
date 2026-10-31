package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestSound(t *testing.T) {
	const sinks = "pactl -f json list sinks"
	const sessions = "systemctl --user is-active wireplumber.service pipewire-media-session.service"
	cards := " 0 [PCH            ]: HDA-Intel - HDA Intel PCH\n                      HDA Intel PCH at 0xf0000000 irq 150\n"
	dummy := `[{"name":"auto_null","description":"Dummy Output","mute":false,"volume":{}}]`
	speakers := `[{"name":"alsa_output.pci-0000_00_1f.3.analog-stereo","description":"Built-in Audio Analog Stereo","mute":false,` +
		`"volume":{"front-left":{"value_percent":"65%"},"front-right":{"value_percent":"65%"}}}]`
	h := &doctor.Host{ID: "fedora", PackageMgr: "dnf", SoundServer: "pipewire"}
	fake := func(list, session string) *sys.Fake {
		return &sys.Fake{
			Files: map[string]string{"/proc/asound/cards": cards},
			Commands: map[string]string{
				sinks:                    list,
				"pactl get-default-sink": "",
				"pipewire --version":     "",
				sessions:                 session,
			},
		}
	}

	r := run(t, Sound, fake(dummy, "inactive\ninactive\n"), h)
	expect(t, r, doctor.Fail, "PipeWire is running without WirePlumber")
	if c := r.Findings[0].Fixes[0].Command; c != "systemctl --user enable --now wireplumber.service" {
		t.Errorf("fix = %q", c)
	}
	expect(t, run(t, Sound, fake(dummy, "inactive\nactive\n"), h), doctor.Fail, "No sound hardware was found")
	expect(t, run(t, Sound, fake(dummy, ""), h), doctor.Fail, "No sound hardware was found")
	expect(t, run(t, Sound, fake(speakers, "active\ninactive\n"), h), doctor.OK, "Sound plays through Built-in Audio Analog Stereo at 65%")
}
