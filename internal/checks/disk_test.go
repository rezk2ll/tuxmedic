package checks

import (
	"context"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestHugeSessionLog(t *testing.T) {
	h := &doctor.Host{Home: "/home/sam"}
	fake := &sys.Fake{
		Files: files("/home/sam/.xsession-errors", "", "/home/sam/.xsession-errors.old", ""),
		Commands: map[string]string{
			"stat -c %s /home/sam/.xsession-errors":     "5368709120\n",
			"stat -c %s /home/sam/.xsession-errors.old": "40960\n",
		},
	}
	fs := hugeSessionLog(context.Background(), fake, h)
	if len(fs) != 1 || fs[0].Title != "~/.xsession-errors has grown to 5.0 GB" || fs[0].Fixes[1].Command != ": > ~/.xsession-errors" {
		t.Fatalf("findings = %+v", fs)
	}
}

func TestNoTrim(t *testing.T) {
	h := &doctor.Host{ID: "arch", PackageMgr: "pacman", Systemd: true}
	fake := func(timer, opts string, ssd bool) *sys.Fake {
		rot := "1"
		if ssd {
			rot = "0"
		}
		return &sys.Fake{
			Files: map[string]string{
				"/proc/self/mounts":                          "/dev/nvme0n1p2 / ext4 " + opts + " 0 0\n",
				"/sys/block/nvme0n1/queue/rotational":        rot,
				"/sys/block/nvme0n1/queue/discard_max_bytes": "2199023255040",
				"/sys/block/loop0/queue/rotational":          "0",
				"/sys/block/loop0/queue/discard_max_bytes":   "4096",
			},
			Commands: map[string]string{"systemctl is-enabled fstrim.timer": timer + "\n"},
		}
	}
	noTrimIn := func(r doctor.Report) bool {
		for _, f := range r.Findings {
			if f.Title == "Your SSD is never told which space is free (TRIM is off)" {
				return true
			}
		}
		return false
	}

	if r := run(t, Disk, fake("disabled", "rw,relatime", true), h); !noTrimIn(r) {
		t.Errorf("SSD with the timer off: want a TRIM finding, got %+v", r.Findings)
	}
	for name, f := range map[string]*sys.Fake{
		"timer on":        fake("enabled", "rw,relatime", true),
		"mounted discard": fake("disabled", "rw,relatime,discard", true),
		"spinning disk":   fake("disabled", "rw,relatime", false),
		"no fstrim.timer": fake("", "rw,relatime", true),
	} {
		if r := run(t, Disk, f, h); noTrimIn(r) {
			t.Errorf("%s: unexpected TRIM finding", name)
		}
	}
}
