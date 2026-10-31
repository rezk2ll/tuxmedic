package checks

import (
	"strings"
	"testing"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestCrashNamesTheUpdateBeforeIt(t *testing.T) {
	const mesa = "Start-Date: 2026-10-28  09:00:00\nUpgrade: libgl1-mesa-dri:amd64 (25.0.7-0ubuntu1, 25.1.2-0ubuntu1)\nEnd-Date: 2026-10-28  09:01:00\n"
	gpuHang := func(us string) string {
		return `{"__REALTIME_TIMESTAMP":"` + us + `","PRIORITY":"3","SYSLOG_IDENTIFIER":"kernel","MESSAGE":"amdgpu 0000:03:00.0: amdgpu: GPU reset begin!"}` + "\n"
	}
	crashAt := func(us string) *sys.Fake {
		return &sys.Fake{
			Clock: time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
			Files: map[string]string{"/var/log/apt/history.log": mesa},
			Commands: map[string]string{
				"journalctl --no-pager --list-boots -o json": `[{"index":-1,"boot_id":"aaaa","first_entry":1,"last_entry":2},` +
					`{"index":0,"boot_id":"bbbb","first_entry":3,"last_entry":4}]`,
				"journalctl --no-pager -q -o json -b aaaa -n 300": gpuHang(us),
			},
		}
	}

	t.Run("a day after a graphics update", func(t *testing.T) {
		r := run(t, Crash, crashAt("1793275200000000"), nil) // 2026-10-29 12:00 UTC
		var f doctor.Finding
		for _, x := range r.Findings {
			if strings.Contains(x.Title, "crashed") {
				f = x
			}
		}
		if !strings.HasPrefix(f.Explain, "This started ") || !strings.Contains(f.Explain, "after an update to graphics drivers") ||
			!strings.Contains(f.Explain, "libgl1-mesa-dri 25.0.7-0ubuntu1 → 25.1.2-0ubuntu1") {
			t.Errorf("explain = %q", f.Explain)
		}
		if len(f.Fixes) == 0 || f.Fixes[0].Command != "sudo apt install libgl1-mesa-dri=25.0.7-0ubuntu1" {
			t.Errorf("fixes = %+v", f.Fixes)
		}
	})
	t.Run("crash before the update", func(t *testing.T) {
		r := run(t, Crash, crashAt("1793102400000000"), nil) // 2026-10-27 12:00 UTC
		for _, x := range r.Findings {
			if strings.Contains(x.Explain, "This started") {
				t.Errorf("blamed an update that came later: %q", x.Explain)
			}
		}
	})
}

func TestSuspectUpdate(t *testing.T) {
	first := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	cs := []pkgChange{
		{pkg: "firefox", from: "1", to: "2", at: first.Add(-time.Hour)},
		{pkg: "linux-image-6.12.0-1-amd64", to: "6.12.0-1", at: first.Add(-30 * time.Hour)},
		{pkg: "mesa-vulkan-drivers", from: "1", to: "2", at: first.Add(-5 * 24 * time.Hour)},
		{pkg: "nvidia-driver", from: "1", at: first.Add(-2 * time.Hour)},
	}
	a, c, ok := suspectUpdate(cs, first)
	if !ok || a.label != "The Linux kernel" || c.pkg != "linux-image-6.12.0-1-amd64" {
		t.Errorf("got %v %+v %v; want the kernel (firefox can't crash the system, mesa is too old, the nvidia removal isn't an upgrade)", a.label, c, ok)
	}
}
