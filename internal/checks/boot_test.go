package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestMissingMounts(t *testing.T) {
	fstab := "# /etc/fstab\nUUID=1111-aaaa / ext4 defaults 0 1\nUUID=2222-bbbb none swap sw 0 0\n" +
		"LABEL=Backup /mnt/backup ext4 defaults,nofail 0 2\ntmpfs /tmp tmpfs defaults 0 0\n"
	fake := &sys.Fake{Files: files("/etc/fstab", fstab, "/dev/disk/by-uuid/1111-aaaa", "")}

	fs := missingMounts(fake)
	if len(fs) != 1 || fs[0].Title != "Startup waits for a disk that isn't there: none (UUID=2222-bbbb)" {
		t.Fatalf("findings = %+v", fs)
	}
	fake.Files["/dev/disk/by-uuid/2222-bbbb"] = ""
	if fs := missingMounts(fake); len(fs) != 0 {
		t.Errorf("all present: %+v", fs)
	}
}

func TestStaleResume(t *testing.T) {
	ubuntu := &doctor.Host{ID: "ubuntu", PackageMgr: "apt"}
	conf := "/etc/initramfs-tools/conf.d/resume"

	fs := staleResume(&sys.Fake{Files: files(conf, "RESUME=UUID=3333-cccc\n", "/proc/cmdline", "ro quiet splash\n")}, ubuntu)
	if len(fs) != 1 || fs[0].Fixes[0].Command != "echo RESUME=none | sudo tee "+conf+" && sudo update-initramfs -u" {
		t.Fatalf("initramfs = %+v", fs)
	}
	if fs := staleResume(&sys.Fake{Files: files(conf, "RESUME=none\n")}, ubuntu); len(fs) != 0 {
		t.Errorf("RESUME=none: %+v", fs)
	}

	fedora := &doctor.Host{ID: "fedora", PackageMgr: "dnf"}
	f := &sys.Fake{Files: files("/proc/cmdline", "ro resume=UUID=4444-dddd rhgb quiet\n"), Commands: map[string]string{"grubby": ""}}
	fs = staleResume(f, fedora)
	if len(fs) != 1 || fs[0].Fixes[0].Command != "sudo grubby --update-kernel=ALL --remove-args=resume=UUID=4444-dddd" {
		t.Fatalf("cmdline = %+v", fs)
	}
	f.Files["/dev/disk/by-uuid/4444-dddd"] = ""
	if fs := staleResume(f, fedora); len(fs) != 0 {
		t.Errorf("present: %+v", fs)
	}
}
