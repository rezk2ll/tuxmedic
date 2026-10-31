package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestPerformanceOnBattery(t *testing.T) {
	const bat, ac = "/sys/class/power_supply/BAT0/", "/sys/class/power_supply/AC/"
	fake := func(online, profile string) *sys.Fake {
		return &sys.Fake{
			Files: files(bat+"type", "Battery", bat+"status", "Discharging", bat+"capacity", "70",
				ac+"type", "Mains", ac+"online", online),
			Commands: map[string]string{"powerprofilesctl get": profile + "\n"},
		}
	}
	h := &doctor.Host{Laptop: true}

	r := run(t, Battery, fake("0", "performance"), h)
	expect(t, r, doctor.Info, "performance power mode is on while running on battery")
	if c := r.Findings[0].Fixes[0].Command; c != "powerprofilesctl set balanced" {
		t.Errorf("fix = %q", c)
	}
	expect(t, run(t, Battery, fake("0", "balanced"), h), doctor.OK, "70%")
}

func TestPowerManagers(t *testing.T) {
	const bat = "/sys/class/power_supply/BAT0/"
	q := "systemctl is-active power-profiles-daemon.service tlp.service auto-cpufreq.service tuned.service"
	fake := func(states string) *sys.Fake {
		return &sys.Fake{
			Files:    files(bat+"type", "Battery", bat+"status", "Discharging", bat+"capacity", "70"),
			Commands: map[string]string{q: states},
		}
	}
	h := &doctor.Host{Laptop: true, PackageMgr: "dnf", Systemd: true}

	r := run(t, Battery, fake("active\nactive\ninactive\ninactive\n"), h)
	expect(t, r, doctor.Warn, "Two power-saving tools are fighting each other")
	fx := r.Findings[0].Fixes
	if len(fx) != 2 || fx[0].Title != "Keep only power-profiles-daemon" || fx[0].Command != "sudo systemctl disable --now tlp.service" ||
		fx[1].Command != "sudo systemctl disable --now power-profiles-daemon.service" {
		t.Errorf("fixes = %+v", fx)
	}

	expect(t, run(t, Battery, fake("inactive\nactive\ninactive\ninactive\n"), h), doctor.OK, "70%")
}
