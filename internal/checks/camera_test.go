package checks

import (
	"fmt"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

const (
	mic     = `{"name":"alsa_input.pci-0000_00_1f.3.analog-stereo","description":"Built-in Audio Analog Stereo","mute":%s,"volume":{"front-left":{"value_percent":"%s"}}}`
	monitor = `{"name":"alsa_output.pci-0000_00_1f.3.analog-stereo.monitor","description":"Monitor of Built-in Audio","mute":false,"volume":{}}`
)

func webcam() map[string]string {
	return map[string]string{
		"/sys/class/video4linux/video0/name":  "Acme HD Webcam: Acme HD Webcam\n",
		"/sys/class/video4linux/video0/index": "0\n",
		"/sys/class/video4linux/video1/name":  "Acme HD Webcam: Acme HD Webcam\n",
		"/sys/class/video4linux/video1/index": "1\n",
	}
}

func camFake(files map[string]string, sources, def string) *sys.Fake {
	f := &sys.Fake{Files: files, Commands: map[string]string{}}
	if sources != "" {
		f.Commands["pactl -f json list sources"] = sources
		f.Commands["pactl get-default-source"] = def + "\n"
	}
	return f
}

func TestCamera(t *testing.T) {
	micOn := "[" + monitor + "," + fmt.Sprintf(mic, "false", "80%") + "]"
	micName := "alsa_input.pci-0000_00_1f.3.analog-stereo"
	laptop := &doctor.Host{ID: "fedora", Laptop: true}

	t.Run("all ready", func(t *testing.T) {
		r := run(t, Camera, camFake(webcam(), micOn, micName), laptop)
		expect(t, r, doctor.OK, "Camera ready: Acme HD Webcam")
		expect(t, r, doctor.OK, "Microphone ready: Built-in Audio Analog Stereo")
	})
	t.Run("camera held by another app", func(t *testing.T) {
		f := camFake(webcam(), micOn, micName)
		f.Files["/proc/4242/comm"] = "zoom\n"
		f.Files["/proc/77/comm"] = "pipewire\n"
		f.Links = map[string]string{"/proc/4242/fd/31": "/dev/video0", "/proc/77/fd/12": "/dev/video0", "/proc/4242/fd/1": "/dev/null"}
		expect(t, run(t, Camera, f, laptop), doctor.Info, "Your camera is in use by zoom")
	})
	t.Run("no camera, Intel IPU", func(t *testing.T) {
		f := camFake(map[string]string{
			"/sys/bus/pci/devices/0000:00:05.0/class":  "0x048000\n",
			"/sys/bus/pci/devices/0000:00:05.0/vendor": "0x8086\n",
		}, micOn, micName)
		expect(t, run(t, Camera, f, laptop), doctor.Warn, "camera needs a driver that isn't set up")
	})
	t.Run("no camera, AMD audio in the same PCI class", func(t *testing.T) {
		f := camFake(map[string]string{
			"/sys/bus/pci/devices/0000:c4:00.5/class":  "0x048000\n",
			"/sys/bus/pci/devices/0000:c4:00.5/vendor": "0x1022\n",
		}, micOn, micName)
		expect(t, run(t, Camera, f, laptop), doctor.Info, "No camera found")
	})
	t.Run("mic muted", func(t *testing.T) {
		src := "[" + fmt.Sprintf(mic, "true", "80%") + "]"
		expect(t, run(t, Camera, camFake(webcam(), src, micName), laptop), doctor.Warn, "is muted")
	})
	t.Run("mic at zero", func(t *testing.T) {
		src := "[" + fmt.Sprintf(mic, "false", "3%") + "]"
		expect(t, run(t, Camera, camFake(webcam(), src, micName), laptop), doctor.Warn, "turned almost all the way down")
	})
	t.Run("monitor as default input", func(t *testing.T) {
		r := run(t, Camera, camFake(webcam(), micOn, "alsa_output.pci-0000_00_1f.3.analog-stereo.monitor"), laptop)
		expect(t, r, doctor.Warn, "record what your speakers play")
		want := "pactl set-default-source " + micName
		for _, f := range r.Findings {
			for _, x := range f.Fixes {
				if x.Command == want {
					return
				}
			}
		}
		t.Errorf("no fix runs %q", want)
	})
	t.Run("GNOME privacy switch", func(t *testing.T) {
		f := camFake(webcam(), micOn, micName)
		f.Commands["gsettings get org.gnome.desktop.privacy disable-camera"] = "true\n"
		f.Commands["gsettings get org.gnome.desktop.privacy disable-microphone"] = "false\n"
		h := &doctor.Host{ID: "fedora", Desktop: "GNOME"}
		expect(t, run(t, Camera, f, h), doctor.Fail, "The camera is turned off in your privacy settings")
	})
}
