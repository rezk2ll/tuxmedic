package checks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestSerialAccess(t *testing.T) {
	ubuntu := &doctor.Host{ID: "ubuntu", PackageMgr: "apt", Systemd: true, User: "sam"}
	brl := "journalctl --no-pager -q -o json -b -k -g brltty"

	fs := serialAccess(context.Background(), &sys.Fake{Commands: map[string]string{
		brl: kernelLog("usb 1-2: usbfs: interface 0 claimed by ch341 while 'brltty' sets config #1"),
	}}, ubuntu)
	if len(fs) != 1 || fs[0].Fixes[0].Command != "sudo apt remove brltty" {
		t.Fatalf("brltty = %+v", fs)
	}

	locked := &sys.Fake{
		Files:    files("/dev/ttyACM0", "", "/etc/group", "root:x:0:\nuucp:x:14:\n"),
		Commands: map[string]string{brl: ""},
		Failing:  map[string]error{"test -w /dev/ttyACM0": errors.New("exit status 1")},
	}
	arch := &doctor.Host{ID: "arch", PackageMgr: "pacman", Systemd: true, User: "sam"}
	fs = serialAccess(context.Background(), locked, arch)
	if len(fs) != 1 || fs[0].Fixes[0].Command != `sudo usermod -aG uucp "$USER"` {
		t.Fatalf("no group = %+v", fs)
	}
	locked.Files["/etc/group"] = "uucp:x:14:alex,sam\n"
	if fs := serialAccess(context.Background(), locked, arch); len(fs) != 0 {
		t.Errorf("already a member: %+v", fs)
	}
	if fs := serialAccess(context.Background(), &sys.Fake{Commands: map[string]string{brl: ""}}, arch); len(fs) != 0 {
		t.Errorf("no serial device: %+v", fs)
	}
}

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
