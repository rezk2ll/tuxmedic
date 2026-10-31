package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestGames(t *testing.T) {
	apt := &doctor.Host{ID: "ubuntu", PackageMgr: "apt"}
	amd := map[string]string{"/sys/class/drm/card1/device/uevent": "DRIVER=amdgpu\n"}
	with := func(base map[string]string, kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}
	steam := map[string]string{"steam --version": ""}
	flatSteam := map[string]string{"flatpak list --app --columns=application": "org.mozilla.firefox\ncom.valvesoftware.Steam\n"}

	t.Run("no steam", func(t *testing.T) {
		expect(t, run(t, Games, &sys.Fake{}, apt), doctor.Skipped, "Steam isn't installed")
	})
	t.Run("no vulkan", func(t *testing.T) {
		f := &sys.Fake{Files: amd, Commands: steam}
		r := run(t, Games, f, apt)
		expect(t, r, doctor.Fail, "There's no Vulkan driver")
		if got := r.Findings[0].Fixes[0].Command; got != "sudo apt install mesa-vulkan-drivers" {
			t.Errorf("fix = %q", got)
		}
	})
	t.Run("no 32-bit driver on arch", func(t *testing.T) {
		f := &sys.Fake{Files: with(amd, "/usr/share/vulkan/icd.d/radeon_icd.json", "{}"), Commands: steam}
		r := run(t, Games, f, &doctor.Host{ID: "arch", PackageMgr: "pacman"})
		expect(t, r, doctor.Warn, "Steam is missing the 32-bit graphics driver")
		if got := r.Findings[0].Fixes[0].Command; got != "sudo pacman -Syu lib32-vulkan-radeon" {
			t.Errorf("fix = %q", got)
		}
	})
	t.Run("all there", func(t *testing.T) {
		f := &sys.Fake{Files: with(amd, "/usr/share/vulkan/icd.d/radeon_icd.json", "{}",
			"/usr/lib/i386-linux-gnu/libvulkan_radeon.so", ""), Commands: steam}
		expect(t, run(t, Games, f, apt), doctor.OK, "Steam has the drivers it needs")
	})
	t.Run("flatpak without controller rules", func(t *testing.T) {
		f := &sys.Fake{Files: amd, Commands: flatSteam}
		r := run(t, Games, f, apt)
		expect(t, r, doctor.Info, "controller rules aren't installed")
		if got := r.Findings[0].Fixes[0].Command; got != "sudo apt install steam-devices" {
			t.Errorf("fix = %q", got)
		}
	})
	t.Run("flatpak with controller rules", func(t *testing.T) {
		f := &sys.Fake{Files: with(amd, "/usr/lib/udev/rules.d/60-steam-input.rules", ""), Commands: flatSteam}
		expect(t, run(t, Games, f, apt), doctor.OK, "Steam (Flatpak) has the drivers it needs")
	})
	t.Run("low memory map limit", func(t *testing.T) {
		rules := with(amd, "/usr/lib/udev/rules.d/60-steam-input.rules", "")
		r := run(t, Games, &sys.Fake{Files: with(rules, "/proc/sys/vm/max_map_count", "65530\n"), Commands: flatSteam}, apt)
		expect(t, r, doctor.Info, "the memory map limit is low")
		if r.Findings[0].Evidence[0] != "vm.max_map_count = 65530" {
			t.Errorf("evidence = %v", r.Findings[0].Evidence)
		}
		expect(t, run(t, Games, &sys.Fake{Files: with(rules, "/proc/sys/vm/max_map_count", "1048576\n"), Commands: flatSteam}, apt),
			doctor.OK, "Steam (Flatpak) has the drivers it needs")
	})
	t.Run("nvidia beats the integrated gpu", func(t *testing.T) {
		f := &sys.Fake{Files: map[string]string{
			"/sys/class/drm/card0/device/uevent": "DRIVER=i915\n",
			"/sys/class/drm/card1/device/uevent": "DRIVER=nvidia\n",
		}}
		if got := gpuDriver(f); got != "nvidia" {
			t.Errorf("gpu = %q", got)
		}
	})
}
