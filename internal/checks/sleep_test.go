package checks

import (
	"context"
	"strings"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestSleepBlockers(t *testing.T) {
	const q = "busctl call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager ListInhibitors"
	out := `a(ssssuu) 4 "handle-power-key:handle-suspend-key:handle-lid-switch" "sam" "GNOME handling keypresses" "block" 1000 2101 ` +
		`"sleep" "sam" "GNOME needs to lock the screen" "delay" 1000 2101 ` +
		`"idle:sleep" "Transmission" "Downloading \"debian.iso\"" "block" 1000 4242 ` +
		`"sleep" "Transmission" "Seeding" "block" 1000 4242` + "\n"
	fs := sleepBlockers(context.Background(), &sys.Fake{Commands: map[string]string{q: out}})
	if len(fs) != 1 || fs[0].Title != "The computer won't go to sleep while Transmission asks it not to" ||
		fs[0].Evidence[0] != `Transmission: Downloading \"debian.iso\"` {
		t.Fatalf("findings = %+v", fs)
	}
	if fs := sleepBlockers(context.Background(), &sys.Fake{Commands: map[string]string{q: "a(ssssuu) 0\n"}}); len(fs) != 0 {
		t.Errorf("none: %+v", fs)
	}
}

func TestSleep(t *testing.T) {
	laptop := &doctor.Host{ID: "fedora", Laptop: true, Desktop: "GNOME"}
	base := func(extra map[string]string) *sys.Fake {
		f := map[string]string{"/sys/power/mem_sleep": "s2idle [deep]\n", "/sys/power/suspend_stats/fail": "0\n"}
		for k, v := range extra {
			f[k] = v
		}
		return &sys.Fake{Files: f}
	}

	t.Run("fine", func(t *testing.T) {
		expect(t, run(t, Sleep, base(nil), laptop), doctor.OK, "Sleep worked every time")
	})
	t.Run("no sleep support", func(t *testing.T) {
		expect(t, run(t, Sleep, &sys.Fake{}, laptop), doctor.Skipped, "doesn't support sleep")
	})
	t.Run("blocked by a device", func(t *testing.T) {
		f := base(map[string]string{"/sys/power/suspend_stats/fail": "2\n", "/sys/power/suspend_stats/last_failed_dev": "0000:00:14.0\n"})
		expect(t, run(t, Sleep, f, laptop), doctor.Warn, "Going to sleep failed 2 times since startup, blocked by 0000:00:14.0")
	})
	t.Run("deep available but not used", func(t *testing.T) {
		f := base(map[string]string{"/sys/power/mem_sleep": "[s2idle] deep\n"})
		expect(t, run(t, Sleep, f, laptop), doctor.Info, "Deep sleep is available but not used")
	})
	t.Run("lid ignored in logind.conf", func(t *testing.T) {
		f := base(map[string]string{"/etc/systemd/logind.conf": "[Login]\n#HandleLidSwitch=suspend\nHandleLidSwitch=ignore\n"})
		r := run(t, Sleep, f, laptop)
		expect(t, r, doctor.Info, "Closing the lid doesn't put the laptop to sleep")
		if ev := r.Findings[0].Evidence; len(ev) != 1 || ev[0] != "/etc/systemd/logind.conf: HandleLidSwitch=ignore" {
			t.Errorf("evidence = %v", ev)
		}
		if c := r.Findings[0].Fixes[0].Command; !strings.Contains(c, "HandleLidSwitch=suspend") {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("a drop-in turns it back on", func(t *testing.T) {
		f := base(map[string]string{
			"/etc/systemd/logind.conf":                   "[Login]\nHandleLidSwitch=ignore\n",
			"/etc/systemd/logind.conf.d/90-lid.conf":     "[Login]\nHandleLidSwitch=suspend\n",
			"/usr/lib/systemd/logind.conf.d/10-pkg.conf": "[Login]\nHandleLidSwitch=lock\n",
		})
		expect(t, run(t, Sleep, f, laptop), doctor.OK, "Sleep worked")
	})
	t.Run("docked lid suspends", func(t *testing.T) {
		f := base(map[string]string{"/etc/systemd/logind.conf.d/50-dock.conf": "[Login]\nHandleLidSwitchDocked=suspend\n"})
		r := run(t, Sleep, f, &doctor.Host{ID: "fedora", Laptop: true, Desktop: "XFCE"})
		expect(t, r, doctor.Info, "even with a monitor plugged in")
		if c := r.Findings[0].Fixes[0].Command; !strings.Contains(c, "HandleLidSwitchDocked=ignore") {
			t.Errorf("fix = %q", c)
		}
		expect(t, run(t, Sleep, f, &doctor.Host{ID: "fedora", Laptop: true, Desktop: "GNOME"}), doctor.OK, "Sleep worked")
	})
	t.Run("KDE handles the lid itself", func(t *testing.T) {
		f := base(map[string]string{"/etc/systemd/logind.conf": "[Login]\nHandleLidSwitch=ignore\n"})
		expect(t, run(t, Sleep, f, &doctor.Host{ID: "arch", Laptop: true, Desktop: "KDE"}), doctor.OK, "Sleep worked")
	})
	t.Run("desktops have no lid", func(t *testing.T) {
		f := base(map[string]string{"/etc/systemd/logind.conf": "[Login]\nHandleLidSwitch=ignore\n"})
		expect(t, run(t, Sleep, f, &doctor.Host{ID: "fedora", Desktop: "GNOME"}), doctor.OK, "Sleep worked")
	})
}
