package checks

import (
	"strings"
	"testing"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

const aptLog = `
Start-Date: 2026-10-01  10:00:00
Commandline: apt upgrade
Upgrade: firefox:amd64 (140.0, 141.0)
End-Date: 2026-10-01  10:01:00

Start-Date: 2026-10-28  09:00:00
Commandline: /usr/bin/unattended-upgrade
Upgrade: mesa-vulkan-drivers:amd64 (25.0.7-0ubuntu1, 25.1.2-0ubuntu1), libgl1-mesa-dri:amd64 (25.0.7-0ubuntu1, 25.1.2-0ubuntu1)
End-Date: 2026-10-28  09:01:00

Start-Date: 2026-10-30  11:00:00
Commandline: apt install linux-image-generic
Install: linux-image-7.0.0-42-generic:amd64 (7.0.0-42.42, automatic)
Upgrade: linux-image-generic:amd64 (7.0.0-38.38, 7.0.0-42.42)
End-Date: 2026-10-30  11:01:00

Start-Date: 2026-10-30  11:20:00
Commandline: apt autoremove
Remove: linux-image-7.0.0-37-generic:amd64 (7.0.0-37.37)
End-Date: 2026-10-30  11:20:30
`

func TestParseAptHistory(t *testing.T) {
	cs := parseAptHistory(aptLog)
	if len(cs) != 6 {
		t.Fatalf("got %d changes: %+v", len(cs), cs)
	}
	if c := cs[1]; c.pkg != "mesa-vulkan-drivers" || c.from != "25.0.7-0ubuntu1" || c.to != "25.1.2-0ubuntu1" {
		t.Errorf("upgrade parsed as %+v", c)
	}
	if c := cs[3]; c.pkg != "linux-image-7.0.0-42-generic" || c.to != "7.0.0-42.42" {
		t.Errorf("install parsed as %+v", c)
	}
	if c := cs[5]; c.from != "7.0.0-37.37" || c.to != "" {
		t.Errorf("remove parsed as %+v", c)
	}
}

func TestParsePacmanLog(t *testing.T) {
	cs := parsePacmanLog(`[2026-10-29T08:00:00+0000] [PACMAN] Running 'pacman -Syu'
[2026-10-29T08:00:05+0000] [ALPM] upgraded mesa (1:25.1.1-1 -> 1:25.2.0-1)
[2026-10-29T08:00:06+0000] [ALPM] installed nvidia-utils (580.1-1)
[2019-03-01 10:00] [ALPM] removed oldthing (1.0-1)
`)
	if len(cs) != 3 || cs[0].pkg != "mesa" || cs[0].from != "1:25.1.1-1" || cs[0].to != "1:25.2.0-1" || cs[2].from != "1.0-1" {
		t.Errorf("got %+v", cs)
	}
}

func TestParseRPMLast(t *testing.T) {
	cs := parseRPMLast(`kernel-core-6.17.4-300.fc43.x86_64           Wed Oct 29 08:00:00 2026
gpg-pubkey-36f612dc-6786af3b                 Tue Aug 25 06:47:45 2026
mesa-dri-drivers-25.2.0-1.fc43.x86_64        Wed Oct 29 08:00:00 2026
`)
	if len(cs) != 2 || cs[0].pkg != "kernel-core" || cs[0].to != "6.17.4-300.fc43" || cs[1].pkg != "mesa-dri-drivers" {
		t.Errorf("got %+v", cs)
	}
}

func TestChanges(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.Local)
	debian := &doctor.Host{ID: "debian", PackageMgr: "apt"}

	t.Run("apt", func(t *testing.T) {
		f := &sys.Fake{Clock: now, Files: map[string]string{"/var/log/apt/history.log": aptLog}}
		r := run(t, Changes, f, debian)
		expect(t, r, doctor.Info, "The Linux kernel changed yesterday at 11:00 (linux-image-generic 7.0.0-38.38 → 7.0.0-42.42)")
		expect(t, r, doctor.Info, "Graphics drivers changed")
		expect(t, r, doctor.Info, "5 packages installed, updated or removed in the last 2 weeks")
		for _, fd := range r.Findings {
			if strings.Contains(fd.Title, "firefox") {
				t.Errorf("a change older than 2 weeks is listed: %q", fd.Title)
			}
		}
	})
	t.Run("nothing recent", func(t *testing.T) {
		f := &sys.Fake{Clock: now.AddDate(0, 2, 0), Files: map[string]string{"/var/log/apt/history.log": aptLog}}
		expect(t, run(t, Changes, f, debian), doctor.OK, "Nothing was installed or updated in the last 2 weeks")
	})
	t.Run("dnf offers a downgrade", func(t *testing.T) {
		f := &sys.Fake{Clock: now, Commands: map[string]string{
			"rpm -qa --last": "mesa-dri-drivers-25.2.0-1.fc43.x86_64        Wed Oct 29 08:00:00 2026\n",
		}}
		r := run(t, Changes, f, &doctor.Host{ID: "fedora", PackageMgr: "dnf"})
		expect(t, r, doctor.Info, "Graphics drivers changed")
		if r.Findings[0].Fixes[0].Command != "sudo dnf downgrade mesa-dri-drivers" {
			t.Errorf("fix = %+v", r.Findings[0].Fixes)
		}
	})
	t.Run("no history", func(t *testing.T) {
		expect(t, run(t, Changes, &sys.Fake{Clock: now}, &doctor.Host{ID: "alpine", PackageMgr: "apk"}), doctor.Skipped, "can't read the update history of apk")
	})
}
