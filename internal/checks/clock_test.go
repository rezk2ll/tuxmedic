package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestClock(t *testing.T) {
	fake := func(show string, files ...string) *sys.Fake {
		f := &sys.Fake{Files: map[string]string{}, Commands: map[string]string{"timedatectl show": show}}
		for i := 0; i+1 < len(files); i += 2 {
			f.Files[files[i]] = files[i+1]
		}
		return f
	}
	desk := &doctor.Host{Desktop: "GNOME"}
	synced := "Timezone=Europe/Berlin\nLocalRTC=no\nCanNTP=yes\nNTP=yes\nNTPSynchronized=yes\n"

	expect(t, run(t, Clock, fake(synced), desk), doctor.OK, "set automatically (Europe/Berlin)")
	expect(t, run(t, Clock, fake("Timezone=Europe/Berlin\nCanNTP=yes\nNTP=no\n"), desk), doctor.Warn, "Automatic time is turned off")
	expect(t, run(t, Clock, fake(synced, "/boot/efi/EFI/Microsoft/Boot/bootmgfw.efi", ""), desk), doctor.Info, "Windows is also installed")

	utc := "Timezone=Etc/UTC\nLocalRTC=no\nCanNTP=yes\nNTP=yes\nNTPSynchronized=yes\n"
	r := run(t, Clock, fake(utc), desk)
	expect(t, r, doctor.Info, "The time zone is set to UTC")
	if c := r.Findings[0].Fixes[1].Command; c != "timedatectl list-timezones" {
		t.Errorf("fix = %q", c)
	}
	expect(t, run(t, Clock, fake(utc), &doctor.Host{}), doctor.OK, "set automatically (Etc/UTC)")

	noLoc := fake(synced)
	noLoc.Commands["gsettings get org.gnome.desktop.datetime automatic-timezone"] = "true\n"
	noLoc.Commands["gsettings get org.gnome.system.location enabled"] = "false\n"
	expect(t, run(t, Clock, noLoc, desk), doctor.Info, "location services are off")
}
