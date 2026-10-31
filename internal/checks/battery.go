package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Battery = doctor.Check{
	ID:       "battery",
	Name:     "Battery & charging",
	Category: "Power",
	Question: "Why isn't my laptop charging, or why does the battery drain so fast?",
	Run:      runBattery,
}

type supply struct {
	name string
	dir  string
	s    sys.System
}

func (p supply) str(f string) string {
	b, err := p.s.ReadFile(filepath.Join(p.dir, f))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func (p supply) num(f string) (float64, bool) {
	v, err := strconv.ParseFloat(p.str(f), 64)
	return v, err == nil
}

func powerSupplies(s sys.System) []supply {
	dirs, _ := s.Glob("/sys/class/power_supply/*")
	out := make([]supply, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, supply{name: filepath.Base(d), dir: d, s: s})
	}
	return out
}

func runBattery(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	var bats []supply
	var chargers []supply
	for _, p := range powerSupplies(s) {
		switch p.str("type") {
		case "Battery":
			// Mice, keyboards and headsets report scope=Device.
			if p.str("scope") != "Device" {
				bats = append(bats, p)
			}
		case "Mains", "USB", "USB_C", "USB_PD":
			chargers = append(chargers, p)
		}
	}
	if len(bats) == 0 {
		if h.Laptop {
			return []doctor.Finding{{
				Status:  doctor.Fail,
				Title:   "This laptop's battery isn't detected",
				Explain: "The system can't see a battery at all. It may be disconnected, completely dead, or the battery driver failed to load.",
				Fixes: []doctor.Fix{
					{Title: "Shut down, unplug the charger for 30 seconds, then start again"},
					{Title: "Look for battery or ACPI errors", Command: "journalctl -k -b | grep -iE 'battery|acpi'"},
				},
			}}
		}
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No battery on this computer"}}
	}

	pluggedIn, watts := false, 0.0
	for _, c := range chargers {
		if c.str("online") != "1" {
			continue
		}
		pluggedIn = true
		v, okV := c.num("voltage_now")
		a, okA := c.num("current_now")
		if okV && okA && v*a > 0 {
			watts = max(watts, v*a/1e12)
		}
	}

	var fs []doctor.Finding
	for _, b := range bats {
		fs = append(fs, batteryFindings(b, pluggedIn, watts, len(chargers) > 0)...)
	}
	if !pluggedIn && len(chargers) > 0 && s.Has("powerprofilesctl") {
		if out, _ := s.Run(ctx, "powerprofilesctl", "get"); strings.TrimSpace(string(out)) == "performance" {
			fs = append(fs, doctor.Finding{
				Status:  doctor.Info,
				Title:   "The performance power mode is on while running on battery",
				Explain: "Performance mode keeps the processor at high speed, which empties the battery much faster. The desktop doesn't switch back by itself when you unplug.",
				Fixes:   []doctor.Fix{{Title: "Switch to balanced", Command: "powerprofilesctl set balanced"}},
			})
		}
	}
	return append(fs, powerManagers(ctx, s, h)...)
}

var powerTools = []struct{ unit, name string }{
	{"power-profiles-daemon.service", "power-profiles-daemon (your desktop's power mode setting)"},
	{"tlp.service", "TLP"},
	{"auto-cpufreq.service", "auto-cpufreq"},
	{"tuned.service", "TuneD"},
}

// powerManagers catches two power tools fighting over the same settings:
// each undoes the other's changes, so battery life gets worse, not better.
func powerManagers(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !h.Systemd || !s.Has("systemctl") {
		return nil
	}
	args := []string{"is-active"}
	for _, p := range powerTools {
		args = append(args, p.unit)
	}
	out, _ := s.Run(ctx, "systemctl", args...)
	states := strings.Split(strings.TrimSpace(string(out)), "\n")
	var on []int
	for i := range powerTools {
		if i < len(states) && strings.TrimSpace(states[i]) == "active" {
			on = append(on, i)
		}
	}
	if len(on) < 2 {
		return nil
	}
	var names, ev []string
	var fixes []doctor.Fix
	for _, keep := range on {
		names = append(names, powerTools[keep].name)
		ev = append(ev, powerTools[keep].unit+": active")
		var off []string
		for _, o := range on {
			if o != keep {
				off = append(off, powerTools[o].unit)
			}
		}
		fixes = append(fixes, doctor.Fix{
			Title:     "Keep only " + strings.SplitN(powerTools[keep].name, " (", 2)[0],
			Command:   "sudo systemctl disable --now " + strings.Join(off, " "),
			NeedsRoot: true,
		})
	}
	return []doctor.Finding{{
		Status: doctor.Warn,
		Title:  "Two power-saving tools are fighting each other",
		Explain: "These all change the same processor and device power settings, and each undoes what the other did: " + strings.Join(names, ", ") +
			". Battery life usually gets worse, and the power mode in settings may not stick. Keep one.",
		Fixes:    fixes,
		Evidence: ev,
	}}
}

func batteryFindings(b supply, pluggedIn bool, watts float64, canSeeCharger bool) []doctor.Finding {
	var fs []doctor.Finding
	status := b.str("status")
	capacity, hasCap := b.num("capacity")
	label := "Battery"
	if b.name != "BAT0" && b.name != "BAT1" {
		label = "Battery " + b.name
	}
	end, hasEnd := b.num("charge_control_end_threshold")
	if !hasEnd {
		end, hasEnd = b.num("charge_stop_threshold")
	}
	start, hasStart := b.num("charge_control_start_threshold")
	if !hasStart {
		start, hasStart = b.num("charge_start_threshold")
	}
	behaviour := b.str("charge_behaviour")
	charger := "the charger"
	if watts > 0 {
		charger = fmt.Sprintf("the %.0f W charger", watts)
	}
	thresholdFix := doctor.Fix{
		Title:     "Allow a full charge (until the next restart)",
		Command:   fmt.Sprintf("echo 100 | sudo tee %s/charge_control_end_threshold", b.dir),
		NeedsRoot: true,
	}

	switch {
	case strings.Contains(behaviour, "[inhibit-charge]") || strings.Contains(behaviour, "[force-discharge]"):
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   label + " charging is switched off in the firmware settings",
			Explain: "The battery is set to not charge (" + behaviour + "). Something changed this on purpose, usually a power-management tool.",
			Fixes: []doctor.Fix{{
				Title:     "Turn charging back on",
				Command:   fmt.Sprintf("echo auto | sudo tee %s/charge_behaviour", b.dir),
				NeedsRoot: true,
			}},
		})

	case pluggedIn && (status == "Not charging" || status == "Unknown") && hasCap:
		switch {
		case hasEnd && end < 100 && capacity >= end-3:
			fs = append(fs, doctor.Finding{
				Status: doctor.Info,
				Title:  fmt.Sprintf("Charging is paused at %.0f%% on purpose", capacity),
				Explain: fmt.Sprintf("A charge limit of %.0f%% is set. Keeping a battery below full makes it last more years, so this is normal and not a fault. "+
					"Tools like TLP, your desktop's power settings or the laptop maker's app set this limit.", end),
				Fixes: []doctor.Fix{thresholdFix},
			})
		case hasStart && start > 0 && capacity >= start:
			fs = append(fs, doctor.Finding{
				Status: doctor.Info,
				Title:  fmt.Sprintf("Charging waits until the battery drops below %.0f%%", start),
				Explain: fmt.Sprintf("A start threshold of %.0f%% is set: the laptop only starts charging below that level, to avoid topping up all the time. "+
					"It's at %.0f%% now, so it's waiting. That is normal.", start, capacity),
				Fixes: []doctor.Fix{{
					Title:     "Charge now regardless",
					Command:   fmt.Sprintf("echo 0 | sudo tee %s/charge_control_start_threshold", b.dir),
					NeedsRoot: true,
				}},
			})
		case capacity >= 95:
			fs = append(fs, doctor.Finding{Status: doctor.OK, Title: fmt.Sprintf("%s is full (%.0f%%) and plugged in", label, capacity)})
		default:
			fs = append(fs, doctor.Finding{
				Status: doctor.Fail,
				Title:  fmt.Sprintf("Plugged in, but the battery isn't charging (%.0f%%)", capacity),
				Explain: "Power is coming in but the battery isn't taking it, and no charge limit explains it. " +
					"Common causes: a charger too weak for this laptop (phone USB-C chargers), a worn cable or port, or the battery is too hot or too cold to charge safely.",
				Fixes: []doctor.Fix{
					{Title: "Try the laptop's original charger and another port"},
					{Title: "Let the laptop cool down, then plug in again"},
					{Title: "Shut down, unplug for 30 seconds, then start again to reset the charging controller"},
				},
			})
		}

	case pluggedIn && status == "Discharging":
		f := doctor.Finding{
			Status: doctor.Fail,
			Title:  "Plugged in, but the battery is still draining",
			Explain: "The laptop uses more power than " + charger + " supplies, or the charger isn't delivering power properly. " +
				"Low-power USB-C chargers (phone chargers, monitors with weak USB-C) often can't keep up.",
			Fixes: []doctor.Fix{{Title: "Use a charger with at least the wattage printed on the original one"}},
		}
		fs = append(fs, f)

	case !pluggedIn && canSeeCharger && status == "Charging":
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: label + " is charging"})

	case status == "Discharging":
		f := doctor.Finding{Status: doctor.OK, Title: fmt.Sprintf("Running on battery (%.0f%%)", capacity)}
		if p, ok := b.num("power_now"); ok && p > 0 {
			w := p / 1e6
			if e, ok := b.num("energy_now"); ok {
				f.Title += fmt.Sprintf(", about %s left at %.1f W", hoursLeft(e/p), w)
			}
			if w > 25 {
				f.Status = doctor.Warn
				f.Title = fmt.Sprintf("The battery is draining fast: %.0f W", w)
				f.Explain = "A laptop doing light work usually uses 5 to 15 W. Something is keeping the processor or graphics card busy, or power saving is off."
				f.Fixes = []doctor.Fix{
					{Title: "See what uses the most power", Command: "sudo powertop", NeedsRoot: true},
					{Title: "Switch to the power saver profile", Command: "powerprofilesctl set power-saver"},
				}
			}
		}
		if hasCap && capacity <= 10 {
			f.Status = doctor.Warn
			f.Title = fmt.Sprintf("The battery is almost empty (%.0f%%)", capacity)
			f.Explain = "Plug in the charger soon. The computer will shut down by itself when it runs out."
		}
		fs = append(fs, f)

	case status == "Charging":
		t := fmt.Sprintf("%s is charging (%.0f%%)", label, capacity)
		if watts > 0 {
			t += fmt.Sprintf(" from %s", charger)
		}
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: t})

	case status == "Full":
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: label + " is full"})

	case hasCap:
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: fmt.Sprintf("%s is at %.0f%%", label, capacity)})

	default:
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: label + " is present"})
	}

	if hasEnd && end < 100 && !strings.Contains(fs[len(fs)-1].Title, "paused") {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Info,
			Title:   fmt.Sprintf("Charging stops at %.0f%% to protect the battery", end),
			Explain: "A charge limit is set. The battery lasts more years this way, at the cost of some runtime per charge.",
			Fixes:   []doctor.Fix{thresholdFix},
		})
	}

	if f, ok := batteryHealth(b, label); ok {
		fs = append(fs, f)
	}
	return fs
}

func batteryHealth(b supply, label string) (doctor.Finding, bool) {
	full, ok1 := b.num("energy_full")
	design, ok2 := b.num("energy_full_design")
	if !ok1 || !ok2 {
		full, ok1 = b.num("charge_full")
		design, ok2 = b.num("charge_full_design")
	}
	if !ok1 || !ok2 || design <= 0 || full <= 0 {
		return doctor.Finding{}, false
	}
	health := min(100, full/design*100)
	cycles := ""
	if c, ok := b.num("cycle_count"); ok && c > 0 {
		cycles = fmt.Sprintf(" after %.0f charge cycles", c)
	}
	f := doctor.Finding{
		Status: doctor.OK,
		Title:  fmt.Sprintf("%s health is good: %.0f%% of its original capacity%s", label, health, cycles),
	}
	switch {
	case health < 50:
		f.Status = doctor.Fail
		f.Title = fmt.Sprintf("%s is worn out: it holds only %.0f%% of its original charge%s", label, health, cycles)
		f.Explain = "Batteries wear with use and age. At this level runtime is short and the laptop may shut down suddenly under load. Replacing the battery fixes it."
		f.Fixes = []doctor.Fix{{Title: "Order a replacement battery (model " + firstNonEmpty(b.str("model_name"), "shown on the battery label") + ")"}}
	case health < 75:
		f.Status = doctor.Warn
		f.Title = fmt.Sprintf("%s is wearing out: it holds %.0f%% of its original charge%s", label, health, cycles)
		f.Explain = "This is normal for an older battery, but expect noticeably shorter runtime. A charge limit around 80% slows further wear."
	}
	return f, true
}

func hoursLeft(h float64) string {
	if h <= 0 || h > 48 {
		return "an unknown time"
	}
	hh := int(h)
	mm := int((h - float64(hh)) * 60)
	if hh == 0 {
		return fmt.Sprintf("%d min", mm)
	}
	return fmt.Sprintf("%dh%02d", hh, mm)
}
