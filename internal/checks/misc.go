package checks

import (
	"context"
	"fmt"
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
			return []doctor.Finding{{
				Status:  doctor.Fail,
				Title:   "Bluetooth is off",
				Explain: "The adapter is powered down, so nothing can pair or connect.",
				Fixes:   []doctor.Fix{{Title: "Turn it on", Command: "bluetoothctl power on"}},
			}}
		}
	}
	return []doctor.Finding{{Status: doctor.OK, Title: "Bluetooth is on and ready"}}
}

var Sleep = doctor.Check{
	ID:       "sleep",
	Name:     "Sleep & wake",
	Category: "Power",
	Question: "Why won't my laptop wake up, or why is the battery empty after sleeping?",
	Run:      runSleep,
}

func runSleep(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	modes := readTrim(s, "/sys/power/mem_sleep")
	if modes == "" {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "This system doesn't support sleep"}}
	}
	var fs []doctor.Finding
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
	if len(fs) == 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "Sleep worked every time since startup"})
	}
	return fs
}

func atoi(s string) int {
	n := 0
	_, _ = fmt.Sscanf(s, "%d", &n) // anything unreadable counts as 0
	return n
}
