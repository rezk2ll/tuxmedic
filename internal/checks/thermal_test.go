package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestCappedCPU(t *testing.T) {
	const pol = "/sys/devices/system/cpu/cpufreq/policy0/"
	fake := func(extra ...string) *sys.Fake {
		f := &sys.Fake{Files: map[string]string{
			"/sys/class/hwmon/hwmon0/name":        "k10temp",
			"/sys/class/hwmon/hwmon0/temp1_input": "52000",
			pol + "cpuinfo_max_freq":              "5000000",
			pol + "scaling_max_freq":              "5000000",
		}}
		for i := 0; i+1 < len(extra); i += 2 {
			f.Files[extra[i]] = extra[i+1]
		}
		return f
	}
	h := &doctor.Host{Laptop: true}

	expect(t, run(t, Thermal, fake(), h), doctor.OK, "Temperatures are normal")

	r := run(t, Thermal, fake(pol+"scaling_max_freq", "1600000", "/sys/devices/system/cpu/cpufreq/boost", "0"), h)
	expect(t, r, doctor.Warn, "held at 1.6 GHz, 32% of its 5.0 GHz top speed")
	if c := r.Findings[0].Fixes[0].Command; c != "echo 1 | sudo tee /sys/devices/system/cpu/cpufreq/boost" {
		t.Errorf("fix = %q", c)
	}

	r = run(t, Thermal, fake(pol+"bios_limit", "1200000"), h)
	expect(t, r, doctor.Warn, "held at 1.2 GHz")
	if r.Findings[0].Fixes[0].Title != "Use the charger that came with the laptop, then restart" {
		t.Errorf("firmware limit fixes = %+v", r.Findings[0].Fixes)
	}

	expect(t, run(t, Thermal, fake("/sys/devices/system/cpu/intel_pstate/max_perf_pct", "40"), h), doctor.Warn, "40% of its")
	expect(t, run(t, Thermal, fake(pol+"scaling_max_freq", "4000000"), h), doctor.OK, "Temperatures are normal")
}
