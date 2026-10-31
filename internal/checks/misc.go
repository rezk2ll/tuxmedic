package checks

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Clock = doctor.Check{
	ID:       "clock",
	Name:     "Date & time",
	Category: "System",
	Question: "Why is my clock wrong, especially after using Windows?",
	Run:      runClock,
}

func runClock(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !s.Has("timedatectl") {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Time sync check needs systemd's timedatectl"}}
	}
	out, err := s.Run(ctx, "timedatectl", "show")
	if err != nil {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Couldn't read the time settings", Evidence: []string{err.Error()}}}
	}
	kv := parseKV(string(out))
	var fs []doctor.Finding
	windows := s.Exists("/boot/efi/EFI/Microsoft") || s.Exists("/efi/EFI/Microsoft") || s.Exists("/boot/EFI/Microsoft")

	switch {
	case kv["NTP"] == "no" && kv["CanNTP"] == "yes":
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   "Automatic time is turned off",
			Explain: "Your clock isn't being corrected from the internet, so it slowly drifts. A wrong clock breaks secure websites and logins.",
			Fixes:   []doctor.Fix{{Title: "Turn on automatic time", Command: "timedatectl set-ntp true"}},
		})
	case kv["NTPSynchronized"] == "no" && kv["NTP"] == "yes":
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   "The clock hasn't synchronized with a time server yet",
			Explain: "Automatic time is on, but no time server answered. A firewall or no internet connection usually explains it.",
		})
	default:
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "The clock is set automatically (" + firstNonEmpty(kv["Timezone"], "UTC") + ")"})
	}

	// Installers that skip the time zone step leave UTC, so a desktop's
	// clock shows the wrong hour for nearly everyone.
	switch tz := kv["Timezone"]; {
	case h.Desktop == "" || h.Container || h.Virtual != "":
	case tz == "" || tz == "n/a" || tz == "UTC" || tz == "Etc/UTC" || tz == "Universal" || tz == "Zulu" || tz == "Etc/Universal":
		fs = append(fs, doctor.Finding{
			Status: doctor.Info,
			Title:  "The time zone is set to UTC, so the clock may be hours off for you",
			Explain: "The time is right, but shown for UTC instead of where you are. Installers set this when the time zone step is skipped. " +
				"If you do live in UTC, ignore this.",
			Fixes: []doctor.Fix{
				{Title: "Pick your time zone in the desktop's Date & Time settings"},
				{Title: "Or find your city in this list, then run sudo timedatectl set-timezone with it (e.g. Europe/Paris)", Command: "timedatectl list-timezones"},
			},
			Evidence: []string{"Timezone=" + firstNonEmpty(tz, "(none)")},
		})
	}

	// GNOME finds the automatic time zone through location services.
	if strings.Contains(strings.ToLower(h.Desktop), "gnome") && s.Has("gsettings") {
		auto, _ := s.Run(ctx, "gsettings", "get", "org.gnome.desktop.datetime", "automatic-timezone")
		loc, _ := s.Run(ctx, "gsettings", "get", "org.gnome.system.location", "enabled")
		if strings.TrimSpace(string(auto)) == "true" && strings.TrimSpace(string(loc)) == "false" {
			fs = append(fs, doctor.Finding{
				Status:  doctor.Info,
				Title:   "The automatic time zone can't change: location services are off",
				Explain: "GNOME picks the time zone from where you are, which needs location services. With them off, the clock keeps the old time zone when you travel.",
				Fixes: []doctor.Fix{
					{Title: "Turn on location services in Settings, under Privacy, Location"},
					{Title: "Or turn them on from here", Command: "gsettings set org.gnome.system.location enabled true"},
				},
			})
		}
	}

	if windows && kv["LocalRTC"] == "no" {
		fs = append(fs, doctor.Finding{
			Status: doctor.Info,
			Title:  "Windows is also installed: if the clock jumps by hours after using it, this is why",
			Explain: "Windows stores local time in the computer's hardware clock, Linux stores UTC. Each one \"corrects\" the other after a restart. " +
				"The clean fix is to tell Windows to use UTC too.",
			Fixes: []doctor.Fix{
				{Title: "In Windows, run this in an admin Command Prompt", Command: `reg add "HKLM\System\CurrentControlSet\Control\TimeZoneInformation" /v RealTimeIsUniversal /d 1 /t REG_DWORD /f`},
				{Title: "Or make Linux use local time instead", Command: "timedatectl set-local-rtc 1"},
			},
		})
	}
	return fs
}

var Bluetooth = doctor.Check{
	ID:       "bluetooth",
	Name:     "Bluetooth",
	Category: "Devices",
	Question: "Why won't my Bluetooth headphones, mouse or speaker connect?",
	Run:      runBluetooth,
}

func runBluetooth(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	adapters, _ := s.Glob("/sys/class/bluetooth/hci*")
	if len(adapters) == 0 {
		if h.Laptop {
			return []doctor.Finding{{
				Status:  doctor.Info,
				Title:   "No Bluetooth adapter found",
				Explain: "Either this computer has no Bluetooth, or its driver or firmware is missing. The Drivers check shows missing firmware.",
			}}
		}
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No Bluetooth adapter on this computer"}}
	}
	dirs, _ := s.Glob("/sys/class/rfkill/rfkill*")
	for _, d := range dirs {
		if readTrim(s, d+"/type") != "bluetooth" {
			continue
		}
		if readTrim(s, d+"/hard") == "1" {
			return []doctor.Finding{{
				Status:  doctor.Fail,
				Title:   "Bluetooth is switched off by a hardware key or switch",
				Explain: "The airplane key or a switch turned the radio off. Software can't turn it back on.",
				Fixes:   []doctor.Fix{{Title: "Press the airplane or Bluetooth key"}},
			}}
		}
		if readTrim(s, d+"/soft") == "1" {
			return []doctor.Finding{{
				Status:  doctor.Fail,
				Title:   "Bluetooth is turned off (airplane mode?)",
				Explain: "The Bluetooth radio is switched off, so no device can connect.",
				Fixes:   []doctor.Fix{{Title: "Turn Bluetooth on", Command: "rfkill unblock bluetooth"}},
			}}
		}
	}
	if f := btFirmware(ctx, s, h); f != nil {
		return []doctor.Finding{*f}
	}
	if h.Systemd && s.Has("systemctl") {
		if out, _ := s.Run(ctx, "systemctl", "is-active", "bluetooth"); strings.TrimSpace(string(out)) != "active" {
			return []doctor.Finding{{
				Status:  doctor.Fail,
				Title:   "The Bluetooth service isn't running",
				Explain: "The adapter is there, but the service that manages connections is stopped.",
				Fixes:   []doctor.Fix{{Title: "Start it now and at every startup", Command: "sudo systemctl enable --now bluetooth", NeedsRoot: true}},
			}}
		}
	}
	if s.Has("bluetoothctl") {
		if out, err := s.Run(ctx, "bluetoothctl", "show"); err == nil && strings.Contains(string(out), "Powered: no") {
			f := doctor.Finding{
				Status:  doctor.Fail,
				Title:   "Bluetooth is off",
				Explain: "The adapter is powered down, so nothing can pair or connect.",
				Fixes:   []doctor.Fix{{Title: "Turn it on", Command: "bluetoothctl power on"}},
			}
			if btAutoEnableOff(s) {
				f.Explain += " It's also set to start switched off, so it will be off again after the next restart."
				f.Fixes = append(f.Fixes, doctor.Fix{
					Title:     "Turn it on at every startup",
					Command:   `sudo sed -i 's/^\s*AutoEnable\s*=.*/AutoEnable=true/' /etc/bluetooth/main.conf`,
					NeedsRoot: true,
				})
			}
			return []doctor.Finding{f}
		}
	}
	if f := btNoAudioPlugin(s, h); f != nil {
		return []doctor.Finding{*f}
	}
	return []doctor.Finding{{Status: doctor.OK, Title: "Bluetooth is on and ready"}}
}

// btNoAudioPlugin catches PipeWire installed without its Bluetooth plugin,
// which Debian and Ubuntu ship separately: headphones pair and connect,
// then never appear as a sound output.
func btNoAudioPlugin(s sys.System, h *doctor.Host) *doctor.Finding {
	var spa []string
	for _, p := range []string{"/usr/lib/*/spa-0.2", "/usr/lib64/spa-0.2", "/usr/lib/spa-0.2"} {
		hits, _ := s.Glob(p + "/alsa")
		spa = append(spa, hits...)
	}
	if len(spa) == 0 {
		return nil
	}
	for _, d := range spa {
		if s.Exists(strings.TrimSuffix(d, "alsa") + "bluez5") {
			return nil
		}
	}
	f := &doctor.Finding{
		Status:   doctor.Warn,
		Title:    "Bluetooth headphones and speakers will connect but play no sound",
		Explain:  "PipeWire, which plays your sound, is installed without its Bluetooth part. Devices pair fine, but never show up as a sound output.",
		Evidence: []string{"no spa-0.2/bluez5 next to " + spa[0]},
	}
	switch h.PackageMgr {
	case "apt":
		f.Fixes = []doctor.Fix{installFix(h, "libspa-0.2-bluetooth")}
	case "pacman":
		f.Fixes = []doctor.Fix{installFix(h, "pipewire-audio")}
	default:
		f.Fixes = []doctor.Fix{{Title: "Install your distro's PipeWire Bluetooth package (its name has pipewire or spa and bluetooth or bluez in it)"}}
	}
	f.Fixes = append(f.Fixes, doctor.Fix{Title: "Then restart the sound service", Command: "systemctl --user restart pipewire wireplumber"})
	return f
}

// btFirmware looks in this boot's kernel log for a Bluetooth chip whose
// firmware didn't load, or that stopped answering. The adapter still shows
// up in both cases, but nothing pairs.
// btAutoEnableOff reports AutoEnable=false under [Policy] in BlueZ's
// config, which leaves the adapter off at every boot.
func btAutoEnableOff(s sys.System) bool {
	section := ""
	for _, l := range strings.Split(readTrim(s, "/etc/bluetooth/main.conf"), "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") {
			section = l
			continue
		}
		if k, v, ok := strings.Cut(l, "="); ok && section == "[Policy]" && strings.TrimSpace(k) == "AutoEnable" {
			return strings.EqualFold(strings.TrimSpace(v), "false")
		}
	}
	return false
}

func btFirmware(ctx context.Context, s sys.System, h *doctor.Host) *doctor.Finding {
	es, err := journal(ctx, s, "-b", "-k", "-p", "0..4")
	if err != nil {
		return nil
	}
	for _, e := range es {
		m := e.Message
		lower := strings.ToLower(m)
		bt := strings.Contains(m, "Bluetooth: hci")
		fwFile := strings.HasPrefix(m, "Direct firmware load for") &&
			(strings.Contains(m, "rtl_bt/") || strings.Contains(m, "intel/ibt-") || strings.Contains(m, "mediatek/BT") || strings.Contains(m, "qca/"))
		switch {
		case fwFile || bt && strings.Contains(lower, "firmware") && (strings.Contains(lower, "fail") || strings.Contains(lower, "not found")):
			return &doctor.Finding{
				Status: doctor.Fail,
				Title:  "The Bluetooth chip's firmware didn't load",
				Explain: "The chip needs a firmware file from the linux-firmware package to work. It's missing or too old for this chip, " +
					"so Bluetooth shows up but can't find or connect to anything.",
				Fixes:    []doctor.Fix{installFix(h, "linux-firmware"), {Title: "If it's installed already, install the latest updates and restart; new chips need new firmware"}},
				Evidence: []string{m},
			}
		case bt && (strings.Contains(m, "tx timeout") || strings.Contains(m, "command failed") || strings.Contains(m, "Reading Intel version command failed")):
			return &doctor.Finding{
				Status: doctor.Warn,
				Title:  "The Bluetooth chip stopped answering",
				Explain: "The chip hung and didn't respond to the computer. A full power-off resets it, which a restart sometimes doesn't. " +
					"If it keeps happening after updates, it's a known driver bug for some chips.",
				Fixes: []doctor.Fix{
					{Title: "Shut down completely, wait 10 seconds, and turn the computer back on"},
					{Title: "Reload the driver without restarting", Command: "sudo modprobe -r btusb && sudo modprobe btusb", NeedsRoot: true},
				},
				Evidence: []string{m},
			}
		}
	}
	return nil
}

var Sleep = doctor.Check{
	ID:       "sleep",
	Name:     "Sleep & wake",
	Category: "Power",
	Question: "Why won't my laptop wake up, or why is the battery empty after sleeping?",
	Run:      runSleep,
}

var busToken = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"|(\S+)`)

// sleepBlockers lists apps holding a "block" inhibitor on sleep, from
// logind's ListInhibitors: a(ssssuu) N "what" "who" "why" "mode" uid pid ...
// Desktop shells and power managers block only the lid and power keys, to
// handle them themselves, so they don't show up here.
func sleepBlockers(ctx context.Context, s sys.System) []doctor.Finding {
	if !s.Has("busctl") {
		return nil
	}
	out, err := s.Run(ctx, "busctl", "call", "org.freedesktop.login1", "/org/freedesktop/login1",
		"org.freedesktop.login1.Manager", "ListInhibitors")
	if err != nil {
		return nil
	}
	var tok []string
	for _, m := range busToken.FindAllStringSubmatch(string(out), -1) {
		tok = append(tok, m[1]+m[2])
	}
	if len(tok) < 2 {
		return nil
	}
	var who, ev []string
	for r := tok[2:]; len(r) >= 6; r = r[6:] {
		what, app, why, mode := r[0], r[1], r[2], r[3]
		if mode != "block" || !slices.Contains(strings.Split(what, ":"), "sleep") || slices.Contains(who, app) {
			continue
		}
		who = append(who, app)
		ev = append(ev, app+": "+why)
	}
	if len(who) == 0 {
		return nil
	}
	return []doctor.Finding{{
		Status:   doctor.Info,
		Title:    "The computer won't go to sleep while " + joinAnd(who) + " asks it not to",
		Explain:  "Apps can hold off sleep while they work, for example during a download, a backup or a video. Sleep works again once they finish or close.",
		Fixes:    []doctor.Fix{{Title: "Let the app finish, or close it"}, {Title: "See every app holding off sleep", Command: "systemd-inhibit --list"}},
		Evidence: ev,
	}}
}

func runSleep(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	modes := readTrim(s, "/sys/power/mem_sleep")
	if modes == "" {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "This system doesn't support sleep"}}
	}
	fs := sleepBlockers(ctx, s)
	fail := readTrim(s, "/sys/power/suspend_stats/fail")
	if fail != "" && fail != "0" {
		dev := readTrim(s, "/sys/power/suspend_stats/last_failed_dev")
		step := readTrim(s, "/sys/power/suspend_stats/last_failed_step")
		f := doctor.Finding{
			Status:  doctor.Warn,
			Title:   fmt.Sprintf("Going to sleep failed %s since startup", plural(atoi(fail), "time", "times")),
			Explain: "When a device refuses to sleep, the laptop wakes straight back up or stays on in your bag.",
		}
		if dev != "" {
			f.Title += ", blocked by " + dev
			f.Explain += " The last one to refuse was " + dev + " (step: " + firstNonEmpty(step, "unknown") + "). If it's a USB device or dock, unplug it before closing the lid."
		}
		fs = append(fs, f)
	}

	// Sleep errors come from the kernel and from systemd itself (PID 1).
	if es, err := journal(ctx, s, "-b", "-1", "-p", "0..4", "_TRANSPORT=kernel", "+", "_PID=1"); err == nil {
		for _, e := range es {
			if strings.Contains(e.Message, "PM: Some devices failed to suspend") || strings.Contains(e.Message, "Failed to freeze unit") {
				fs = append(fs, doctor.Finding{
					Status:   doctor.Warn,
					Title:    "Sleep failed during your last session",
					Explain:  "A device or program refused to sleep, so the laptop either woke up again or got stuck.",
					Evidence: []string{firstLineOf(e.Message)},
				})
				break
			}
		}
	}

	if h.Laptop && modes == "[s2idle]" {
		fs = append(fs, doctor.Finding{
			Status: doctor.Info,
			Title:  "This laptop only offers \"modern standby\" sleep",
			Explain: "In this mode the computer is never fully asleep, and some laptops lose several percent of battery per hour. " +
				"If the battery is empty in the morning, look for a \"Linux\" or \"S3\" sleep option in the BIOS settings, or hibernate overnight.",
		})
	} else if h.Laptop && strings.Contains(modes, "[s2idle]") && strings.Contains(modes, "deep") {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Info,
			Title:   "Deep sleep is available but not used",
			Explain: "Your laptop supports a deeper sleep that uses less battery, but the lighter \"modern standby\" is selected. Deep sleep may wake up slower, and doesn't work well on every model.",
			Fixes:   []doctor.Fix{{Title: "Try deep sleep until the next restart", Command: "echo deep | sudo tee /sys/power/mem_sleep", NeedsRoot: true}},
		})
	}
	if h.Laptop {
		fs = append(fs, lidSwitch(s, h)...)
	}
	fs = append(fs, hibernateFailed(ctx, s)...)
	if len(fs) == 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "Sleep worked every time since startup"})
	}
	return fs
}

// hibernateFailed explains the two refusals that leave a clear log line:
// Secure Boot's lockdown, and no swap big enough to hold memory.
func hibernateFailed(ctx context.Context, s sys.System) []doctor.Finding {
	es, err := journal(ctx, s, "--since", "-7d", "-p", "0..4", "-g", "hibernat")
	if err != nil {
		return nil
	}
	for i := len(es) - 1; i >= 0; i-- {
		m := es[i].Message
		switch {
		case strings.Contains(m, "hibernation is restricted"):
			return []doctor.Finding{{
				Status: doctor.Warn,
				Title:  "Hibernate is blocked because Secure Boot is on",
				Explain: "With Secure Boot, Linux locks down features that could be used to change the running system, and resuming from hibernation is one of them. " +
					"Sleep still works. Hibernation needs Secure Boot switched off in the firmware settings.",
				Fixes:    []doctor.Fix{{Title: "Use sleep instead, or turn off Secure Boot in the firmware settings", Command: "systemctl reboot --firmware-setup"}},
				Evidence: []string{firstLineOf(m)},
			}}
		case strings.Contains(m, "swap space for hibernation") || strings.Contains(m, "location to hibernate to"):
			return []doctor.Finding{{
				Status: doctor.Warn,
				Title:  "Hibernate failed: there's no swap big enough to hold your memory",
				Explain: "Hibernating saves everything in memory to swap on the disk. The swap here is missing or smaller than what's in use, " +
					"and zram doesn't count because it lives in memory too.",
				Fixes:    []doctor.Fix{{Title: "Make a swap file at least as large as your memory", URL: "https://wiki.archlinux.org/title/Power_management/Suspend_and_hibernate#Hibernation"}},
				Evidence: []string{firstLineOf(m)},
			}}
		}
	}
	return nil
}

// lidSwitch reads logind's lid setting the way logind does: main files, then
// drop-ins, the last one wins. KDE's PowerDevil takes the lid over from
// logind and follows its own settings, so KDE is left out.
func lidSwitch(s sys.System, h *doctor.Host) []doctor.Finding {
	if strings.Contains(strings.ToLower(h.Desktop), "kde") {
		return nil
	}
	files := []string{"/usr/lib/systemd/logind.conf", "/etc/systemd/logind.conf"}
	for _, d := range []string{"/usr/lib/systemd/logind.conf.d", "/run/systemd/logind.conf.d", "/etc/systemd/logind.conf.d"} {
		ds, _ := s.Glob(d + "/*.conf")
		sort.Strings(ds)
		files = append(files, ds...)
	}
	action, from, docked, dockedFrom := "", "", "", ""
	for _, f := range files {
		for _, line := range strings.Split(readTrim(s, f), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "HandleLidSwitch="); ok {
				action, from = strings.TrimSpace(v), f
			}
			if v, ok := strings.CutPrefix(strings.TrimSpace(line), "HandleLidSwitchDocked="); ok {
				docked, dockedFrom = strings.TrimSpace(v), f
			}
		}
	}
	// GNOME keeps the laptop awake with a monitor plugged in whatever
	// logind says, so the docked setting only matters elsewhere.
	if (docked == "suspend" || docked == "hibernate" || docked == "hybrid-sleep" || docked == "suspend-then-hibernate" || docked == "poweroff") &&
		!strings.Contains(strings.ToLower(h.Desktop), "gnome") {
		return []doctor.Finding{{
			Status:  doctor.Info,
			Title:   "Closing the lid puts the laptop to sleep even with a monitor plugged in",
			Explain: "The login manager is set to " + docked + " when the lid closes while a monitor or dock is connected, so you can't use the laptop closed on a desk.",
			Fixes: []doctor.Fix{{
				Title:     "Stay awake when a monitor is plugged in (applies after a restart)",
				Command:   "sudo mkdir -p /etc/systemd/logind.conf.d && printf '[Login]\\nHandleLidSwitchDocked=ignore\\n' | sudo tee /etc/systemd/logind.conf.d/90-lid-docked.conf",
				NeedsRoot: true,
			}},
			Evidence: []string{dockedFrom + ": HandleLidSwitchDocked=" + docked},
		}}
	}
	if action != "ignore" && action != "lock" {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  "Closing the lid doesn't put the laptop to sleep",
		Explain: "The login manager is set to " + map[string]string{"ignore": "do nothing", "lock": "only lock the screen"}[action] + " when the lid closes, so the laptop stays on in your bag and drains the battery. " +
			"Someone may have set it on purpose, for example to use the laptop closed with a monitor; logind already stays awake when a monitor is plugged in.",
		Fixes: []doctor.Fix{{
			Title:     "Sleep when the lid closes (applies after a restart)",
			Command:   "sudo mkdir -p /etc/systemd/logind.conf.d && printf '[Login]\\nHandleLidSwitch=suspend\\n' | sudo tee /etc/systemd/logind.conf.d/90-lid.conf",
			NeedsRoot: true,
		}},
		Evidence: []string{from + ": HandleLidSwitch=" + action},
	}}
}

func atoi(s string) int {
	n := 0
	_, _ = fmt.Sscanf(s, "%d", &n) // anything unreadable counts as 0
	return n
}
