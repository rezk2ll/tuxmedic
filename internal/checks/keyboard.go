package checks

import (
	"context"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Keyboard = doctor.Check{
	ID:       "keyboard",
	Name:     "Keyboard & typing",
	Category: "Devices",
	Question: "Why do keys not type, type late, or why can't I type in my language?",
	Run:      runKeyboard,
}

// appleFnKeys catches hid_apple's fnmode=1, which makes F1 to F12 change
// volume and brightness unless Fn is held. Keychron and other Mac-layout
// keyboards use this driver too. The default (3) already gives non-Apple
// keyboards their F keys, so only an explicit 1 is reported.
func appleFnKeys(s sys.System) []doctor.Finding {
	if devs, _ := s.Glob("/sys/bus/hid/drivers/apple/0*"); len(devs) == 0 || readTrim(s, "/sys/module/hid_apple/parameters/fnmode") != "1" {
		return nil
	}
	return []doctor.Finding{{
		Status:  doctor.Info,
		Title:   "F1 to F12 change volume and brightness unless you hold Fn",
		Explain: "Your keyboard uses the Mac keyboard driver, which is set to put the media keys first, as on a Mac. If you want the F keys first, switch the order.",
		Fixes: []doctor.Fix{
			{Title: "Put the F keys first until the next restart", Command: "echo 2 | sudo tee /sys/module/hid_apple/parameters/fnmode", NeedsRoot: true},
			{Title: "Keep it after restarts", Command: "echo 'options hid_apple fnmode=2' | sudo tee /etc/modprobe.d/hid_apple.conf", NeedsRoot: true},
		},
		Evidence: []string{"hid_apple fnmode=1"},
	}}
}

func runKeyboard(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	var fs []doctor.Finding
	if strings.Contains(strings.ToLower(h.Desktop), "gnome") && s.Has("gsettings") {
		fs = append(fs, accessKeys(ctx, s)...)
	}
	fs = append(fs, inputMethod(s, h)...)
	fs = append(fs, appleFnKeys(s)...)
	if len(fs) == 0 {
		return []doctor.Finding{{Status: doctor.OK, Title: "Nothing is slowing down or blocking your typing"}}
	}
	return fs
}

// accessKeys finds GNOME's accessibility keyboard features, which are easy to
// turn on by accident: holding Shift for 8 seconds enables Slow Keys.
func accessKeys(ctx context.Context, s sys.System) []doctor.Finding {
	features := []struct{ key, name, effect string }{
		{"slowkeys-enable", "Slow Keys", "Keys only type when you hold them down for a moment, so quick presses do nothing"},
		{"bouncekeys-enable", "Bounce Keys", "Pressing the same key twice quickly types it only once"},
		{"stickykeys-enable", "Sticky Keys", "Shift, Ctrl and Alt stay pressed after you let go, so the next key types a shortcut"},
	}
	var fs []doctor.Finding
	for _, f := range features {
		out, _ := s.Run(ctx, "gsettings", "get", "org.gnome.desktop.a11y.keyboard", f.key)
		if strings.TrimSpace(string(out)) != "true" {
			continue
		}
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   f.name + " is turned on",
			Explain: f.effect + ". It's an accessibility feature, and it's easy to turn on by accident: holding Shift for 8 seconds, or pressing it 5 times, switches it on.",
			Fixes: []doctor.Fix{
				{Title: "Turn it off", Command: "gsettings set org.gnome.desktop.a11y.keyboard " + f.key + " false"},
				{Title: "Or in Settings, under Accessibility, Typing"},
			},
		})
	}
	return fs
}

func inputMethod(s sys.System, h *doctor.Host) []doctor.Finding {
	gtk, qt, xmod := s.Getenv("GTK_IM_MODULE"), s.Getenv("QT_IM_MODULE"), s.Getenv("XMODIFIERS")
	want := ""
	for _, v := range []string{gtk, qt, strings.TrimPrefix(xmod, "@im=")} {
		switch {
		case strings.HasPrefix(v, "fcitx"):
			want = "fcitx"
		case v == "ibus" && want == "":
			want = "ibus"
		}
	}
	var fs []doctor.Finding
	ibus, fcitx := running(s, "ibus-daemon") != "", running(s, "fcitx5", "fcitx") != ""
	switch {
	case want == "fcitx" && !fcitx:
		fs = append(fs, imNotRunning("Fcitx", "fcitx5 -d", gtk, qt, xmod))
	case want == "ibus" && !ibus && !strings.Contains(strings.ToLower(h.Desktop), "gnome"):
		fs = append(fs, imNotRunning("IBus", "ibus-daemon -drx", gtk, qt, xmod))
	case ibus && fcitx:
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   "Two input methods are running at once: IBus and Fcitx",
			Explain: "Both try to handle every key you press. Typing can lag, characters can appear twice, or apps stop taking input. Keep the one you use and stop the other from starting.",
			Fixes:   []doctor.Fix{{Title: "Pick one in your system's input method settings (im-config on Debian and Ubuntu, imsettings on Fedora)"}},
		})
	}
	if fcitx && h.Session == "wayland" && strings.Contains(strings.ToLower(h.Desktop), "kde") && (gtk != "" || qt != "") {
		fs = append(fs, doctor.Finding{
			Status: doctor.Info,
			Title:  "On Plasma with Wayland, Fcitx works better without GTK_IM_MODULE and QT_IM_MODULE",
			Explain: "Plasma on Wayland talks to Fcitx directly. With those variables set, apps use an older path, and the candidate window can show up " +
				"in the wrong place or typing can stop in some apps.",
			Fixes: []doctor.Fix{
				{Title: "In System Settings, under Keyboard, Virtual Keyboard, pick Fcitx 5"},
				{Title: "Then remove GTK_IM_MODULE and QT_IM_MODULE from /etc/environment or your profile, and log in again"},
			},
		})
	}
	return fs
}

func imNotRunning(name, start, gtk, qt, xmod string) doctor.Finding {
	var ev []string
	for k, v := range map[string]string{"GTK_IM_MODULE": gtk, "QT_IM_MODULE": qt, "XMODIFIERS": xmod} {
		if v != "" {
			ev = append(ev, k+"="+v)
		}
	}
	return doctor.Finding{
		Status: doctor.Fail,
		Title:  "Apps expect " + name + " for typing, but it isn't running",
		Explain: "Your session tells apps to send key presses through " + name + ", the input method for typing in other languages. " +
			"It isn't running, so some apps ignore the keyboard or won't let you switch languages.",
		Fixes: []doctor.Fix{
			{Title: "Start it now", Command: start},
			{Title: "Make it start when you log in, in your desktop's autostart settings"},
		},
		Evidence: ev,
	}
}
