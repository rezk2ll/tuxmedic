package checks

import (
	"encoding/json"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestLockedOut(t *testing.T) {
	q := "journalctl --no-pager -q -o json --since -15min -p 0..5 -g account temporarily locked"
	h := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Systemd: true, User: "sam", Home: "/home/sam"}
	line := func(user string) string {
		b, _ := json.Marshal(map[string]string{
			"MESSAGE":              "pam_faillock(sudo:auth): Consecutive login failures for user " + user + " account temporarily locked",
			"PRIORITY":             "5",
			"SYSLOG_IDENTIFIER":    "sudo",
			"__REALTIME_TIMESTAMP": "1761900000000000",
		})
		return string(b) + "\n"
	}
	fake := func(log string) *sys.Fake {
		return &sys.Fake{
			Files:    map[string]string{"/proc/sys/kernel/hostname": "box\n", "/etc/hosts": "127.0.1.1 box\n"},
			Commands: map[string]string{q: log},
		}
	}

	r := run(t, Permissions, fake(line("sam")), h)
	expect(t, r, doctor.Warn, "Your account is locked for a few minutes")
	if c := r.Findings[0].Fixes[2].Command; c != "faillock --user sam" {
		t.Errorf("fix = %q", c)
	}
	expect(t, run(t, Permissions, fake(line("alex")), h), doctor.OK, "group memberships are active")
}
