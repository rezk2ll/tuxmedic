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
	return fs
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
