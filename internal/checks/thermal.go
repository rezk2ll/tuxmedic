package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Thermal = doctor.Check{
	ID:       "thermal",
	Name:     "Temperature & fans",
	Category: "Performance",
	Question: "Why is my computer hot, loud, or slowing down?",
	Run:      runThermal,
}

type sensor struct {
	chip, label string
	temp, crit  float64
}

var cpuChips = map[string]bool{"coretemp": true, "k10temp": true, "zenpower": true, "cpu_thermal": true, "x86_pkg_temp": true}

func sensors(s sys.System) []sensor {
	var out []sensor
	inputs, _ := s.Glob("/sys/class/hwmon/hwmon*/temp*_input")
	for _, in := range inputs {
		dir := filepath.Dir(in)
		base := strings.TrimSuffix(filepath.Base(in), "_input")
		chip := readTrim(s, dir+"/name")
		t, ok := milli(s, in)
		if !ok || t <= 0 || t > 150 {
			continue
		}
		crit, ok := milli(s, dir+"/"+base+"_crit")
		if !ok || crit <= 0 {
			crit, _ = milli(s, dir+"/"+base+"_max")
		}
		out = append(out, sensor{chip: chip, label: firstNonEmpty(readTrim(s, dir+"/"+base+"_label"), base), temp: t, crit: crit})
	}
	return out
}

func readTrim(s sys.System, path string) string {
	b, err := s.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func milli(s sys.System, path string) (float64, bool) {
	v, err := strconv.ParseFloat(readTrim(s, path), 64)
	return v / 1000, err == nil
}

func friendlyChip(chip string) string {
	switch {
	case cpuChips[chip]:
		return "Processor"
	case chip == "amdgpu" || chip == "nouveau" || chip == "radeon" || chip == "i915" || chip == "xe":
		return "Graphics"
	case chip == "nvme" || chip == "drivetemp":
		return "SSD"
	case chip == "acpitz":
		return "Motherboard"
	case strings.HasPrefix(chip, "iwlwifi") || strings.HasPrefix(chip, "mt7") || strings.HasPrefix(chip, "ath"):
		return "Wi-Fi card"
	}
	return chip
}

func runThermal(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	ss := sensors(s)
	if len(ss) == 0 {
		if h.Virtual != "" || h.Container {
			return []doctor.Finding{{Status: doctor.Skipped, Title: "Virtual machines have no temperature sensors"}}
		}
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No temperature sensors are visible", Explain: "Install lm-sensors and run sensors-detect to load the sensor drivers."}}
	}
	sort.Slice(ss, func(i, j int) bool { return ss[i].temp-ss[i].crit > ss[j].temp-ss[j].crit })

	var fs []doctor.Finding
	var hottest sensor
	for _, x := range ss {
		if x.temp > hottest.temp {
			hottest = x
		}
	}
	busy := topCPU(ctx, s)
	busyHint := ""
	if busy != "" {
		busyHint = " The busiest program right now is " + busy + "."
	}

	worst := ss[0]
	switch {
	case worst.crit > 0 && worst.temp >= worst.crit-5 || worst.temp >= 95:
		fs = append(fs, doctor.Finding{
			Status: doctor.Fail,
			Title:  fmt.Sprintf("%s is dangerously hot: %.0f°C", friendlyChip(worst.chip), worst.temp),
			Explain: "It is close to the temperature where the hardware slows itself down hard or switches off to avoid damage." + busyHint +
				" Blocked vents and dust are the most common cause.",
			Fixes: hotFixes(),
		})
	case hottest.temp >= 85:
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   fmt.Sprintf("%s is running hot: %.0f°C", friendlyChip(hottest.chip), hottest.temp),
			Explain: "Hardware this hot slows itself down to cool off, which makes the computer feel sluggish and the fans loud." + busyHint,
			Fixes:   hotFixes(),
		})
	default:
		fs = append(fs, doctor.Finding{
			Status:   doctor.OK,
			Title:    fmt.Sprintf("Temperatures are normal (hottest: %s at %.0f°C)", strings.ToLower(friendlyChip(hottest.chip)), hottest.temp),
			Evidence: sensorLines(ss),
		})
	}

	if f, ok := deadFan(s, hottest); ok {
		fs = append(fs, f)
	}
	if n := throttleCount(s); n > 0 {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Info,
			Title:   fmt.Sprintf("The processor slowed itself down %d times since startup to cool off", n),
			Explain: "Some throttling under heavy load is normal. If it happens while you're doing light work, the cooling isn't keeping up.",
		})
	}
	if f, ok := cappedCPU(s); ok {
		fs = append(fs, f)
	}
	return fs
}

// cappedCPU catches a processor held well below its top speed by a
// setting, not by heat: a power tool's battery limit, intel_pstate's
// max_perf_pct, or a limit the firmware sets when the charger is too weak.
func cappedCPU(s sys.System) (doctor.Finding, bool) {
	var limit, top float64
	var bios bool
	policies, _ := s.Glob("/sys/devices/system/cpu/cpufreq/policy*/scaling_max_freq")
	for _, p := range policies {
		dir := filepath.Dir(p)
		m, ok1 := milli(s, p)
		c, ok2 := milli(s, dir+"/cpuinfo_max_freq")
		if !ok1 || !ok2 || c <= 0 {
			continue
		}
		if b, ok := milli(s, dir+"/bios_limit"); ok && b > 0 && b < m {
			m, bios = b, true
		}
		if top == 0 || m/c < limit/top {
			limit, top = m, c
		}
	}
	pct := 100.0
	if top > 0 {
		pct = limit / top * 100
	}
	if v, err := strconv.Atoi(readTrim(s, "/sys/devices/system/cpu/intel_pstate/max_perf_pct")); err == nil && float64(v) < pct {
		pct, bios = float64(v), false
		limit = top * pct / 100
	}
	if top == 0 || pct >= 70 {
		return doctor.Finding{}, false
	}
	ghz := func(mhz float64) string { return strconv.FormatFloat(mhz/1000, 'f', 1, 64) + " GHz" }
	f := doctor.Finding{
		Status: doctor.Warn,
		Title:  fmt.Sprintf("The processor is held at %s, %.0f%% of its %s top speed", ghz(limit), pct, ghz(top)),
		Explain: "This is a setting, not heat. Power-saving tools such as TLP or auto-cpufreq can set a lower top speed, often only on battery, " +
			"and everything feels slow. Undo it in the tool that set it.",
		Fixes: []doctor.Fix{
			{Title: "Look for a speed limit in TLP's settings (CPU_SCALING_MAX_FREQ, CPU_MAX_PERF)", Command: "grep -rE '^CPU_(SCALING_MAX_FREQ|MAX_PERF)' /etc/tlp.conf /etc/tlp.d/"},
			{Title: "Look for a speed limit in auto-cpufreq's settings", Command: "grep -E '^scaling_max_freq' /etc/auto-cpufreq.conf"},
		},
		Evidence: []string{fmt.Sprintf("top speed allowed: %.0f MHz of %.0f MHz", limit, top)},
	}
	switch {
	case readTrim(s, "/sys/devices/system/cpu/cpufreq/boost") == "0":
		f.Fixes = append([]doctor.Fix{{Title: "Turn turbo boost back on (until restart)", Command: "echo 1 | sudo tee /sys/devices/system/cpu/cpufreq/boost", NeedsRoot: true}}, f.Fixes...)
	case readTrim(s, "/sys/devices/system/cpu/intel_pstate/no_turbo") == "1":
		f.Fixes = append([]doctor.Fix{{Title: "Turn turbo boost back on (until restart)", Command: "echo 0 | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo", NeedsRoot: true}}, f.Fixes...)
	}
	if bios {
		f.Explain = "The computer's firmware set this limit. Laptops do it when the charger is too weak or not the original one, or when the battery is very worn."
		f.Fixes = []doctor.Fix{{Title: "Use the charger that came with the laptop, then restart"}, {Title: "Install firmware (BIOS) updates", Command: "fwupdmgr get-updates"}}
	}
	return f, true
}

func hotFixes() []doctor.Fix {
	return []doctor.Fix{
		{Title: "Close the program using the most processor time"},
		{Title: "Keep the vents clear: use a hard, flat surface, not a bed or lap"},
		{Title: "Clean dust out of the vents and fans"},
	}
}

func sensorLines(ss []sensor) []string {
	var out []string
	for _, x := range ss {
		l := fmt.Sprintf("%s %s: %.0f°C", x.chip, x.label, x.temp)
		if x.crit > 0 {
			l += fmt.Sprintf(" (limit %.0f°C)", x.crit)
		}
		out = append(out, l)
	}
	return out
}

func deadFan(s sys.System, hottest sensor) (doctor.Finding, bool) {
	fans, _ := s.Glob("/sys/class/hwmon/hwmon*/fan*_input")
	if len(fans) == 0 || hottest.temp < 75 {
		return doctor.Finding{}, false
	}
	for _, f := range fans {
		if v, err := strconv.Atoi(readTrim(s, f)); err == nil && v > 0 {
			return doctor.Finding{}, false
		}
	}
	return doctor.Finding{
		Status:  doctor.Warn,
		Title:   fmt.Sprintf("The fan reports 0 RPM while the %s is at %.0f°C", strings.ToLower(friendlyChip(hottest.chip)), hottest.temp),
		Explain: "The fan should be spinning at this temperature. It may be stuck, broken, or controlled by a tool that switched it off.",
		Fixes:   []doctor.Fix{{Title: "Listen for the fan; if it's silent, have it checked or replaced"}},
	}, true
}

func throttleCount(s sys.System) int {
	files, _ := s.Glob("/sys/devices/system/cpu/cpu*/thermal_throttle/package_throttle_count")
	if len(files) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(readTrim(s, files[0]))
	return n
}

// topCPU samples /proc twice to find the process burning the most CPU right now.
func topCPU(ctx context.Context, s sys.System) string {
	first := procTimes(s)
	select {
	case <-ctx.Done():
		return ""
	case <-time.After(400 * time.Millisecond):
	}
	second := procTimes(s)
	best, bestName := uint64(0), ""
	for pid, p := range second {
		if q, ok := first[pid]; ok && p.ticks > q.ticks && p.ticks-q.ticks > best {
			best, bestName = p.ticks-q.ticks, p.name
		}
	}
	// 100 ticks per second on every Linux build; below 10% CPU isn't worth naming.
	if best < 4 {
		return ""
	}
	return appName(bestName)
}

type procTime struct {
	name  string
	ticks uint64
}

func procTimes(s sys.System) map[string]procTime {
	files, _ := s.Glob("/proc/[0-9]*/stat")
	out := make(map[string]procTime, len(files))
	for _, f := range files {
		b, err := s.ReadFile(f)
		if err != nil {
			continue
		}
		line := string(b)
		open, close := strings.IndexByte(line, '('), strings.LastIndexByte(line, ')')
		if open < 0 || close < open {
			continue
		}
		fields := strings.Fields(line[close+1:])
		if len(fields) < 13 {
			continue
		}
		u, _ := strconv.ParseUint(fields[11], 10, 64)
		k, _ := strconv.ParseUint(fields[12], 10, 64)
		out[filepath.Base(filepath.Dir(f))] = procTime{name: line[open+1 : close], ticks: u + k}
	}
	return out
}
