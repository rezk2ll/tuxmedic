package checks

import (
	"encoding/json"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestHibernateFailed(t *testing.T) {
	q := "journalctl --no-pager -q -o json --since -7d -p 0..4 -g hibernat"
	line := func(ident, msg string) string {
		b, _ := json.Marshal(map[string]string{"MESSAGE": msg, "PRIORITY": "3", "SYSLOG_IDENTIFIER": ident, "__REALTIME_TIMESTAMP": "1761900000000000"})
		return string(b) + "\n"
	}
	fake := func(log string) *sys.Fake {
		return &sys.Fake{
			Files:    map[string]string{"/sys/power/mem_sleep": "s2idle [deep]"},
			Commands: map[string]string{q: log},
		}
	}
	h := &doctor.Host{}

	expect(t, run(t, Sleep, fake(line("kernel", "Lockdown: systemd-sleep: hibernation is restricted; see man kernel_lockdown.7")), h),
		doctor.Warn, "Hibernate is blocked because Secure Boot is on")
	expect(t, run(t, Sleep, fake(line("systemd-sleep", "Not enough suitable swap space for hibernation available on compatible block devices and file systems.")), h),
		doctor.Warn, "no swap big enough")
	expect(t, run(t, Sleep, fake(""), h), doctor.OK, "Sleep worked")
}
