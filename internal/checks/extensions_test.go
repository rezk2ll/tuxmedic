package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestAppImagesNoFuse(t *testing.T) {
	h := &doctor.Host{ID: "ubuntu", PackageMgr: "apt", Home: "/home/alex"}
	app := "/home/alex/Applications/Some Editor-1.2-x86_64.AppImage"

	fs := appImagesNoFuse(&sys.Fake{Files: files(app, "")}, h)
	if len(fs) != 1 || fs[0].Fixes[0].Command != "sudo apt install libfuse2t64 || sudo apt install libfuse2" {
		t.Fatalf("findings = %+v", fs)
	}
	if c := fs[0].Fixes[1].Command; c != "'"+app+"' --appimage-extract-and-run" {
		t.Errorf("fix = %q", c)
	}
	if fs := appImagesNoFuse(&sys.Fake{Files: files(app, "", "/usr/lib/x86_64-linux-gnu/libfuse.so.2", "")}, h); len(fs) != 0 {
		t.Errorf("libfuse2 installed: %+v", fs)
	}
	if fs := appImagesNoFuse(&sys.Fake{}, h); len(fs) != 0 {
		t.Errorf("no AppImages: %+v", fs)
	}
	fs = appImagesNoFuse(&sys.Fake{Files: files("/home/alex/Downloads/tool.appimage", "")}, &doctor.Host{PackageMgr: "pacman", Home: "/home/alex"})
	if len(fs) != 1 || fs[0].Fixes[0].Command != "sudo pacman -S fuse2" {
		t.Errorf("arch = %+v", fs)
	}
}

func TestBrokenExtensions(t *testing.T) {
	gnome := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Desktop: "GNOME", Systemd: true}
	list := "gnome-extensions list --enabled --details"
	ext := func(uuid, name, state string) string {
		return uuid + "\n  Name: " + name + "\n  Description: synthetic\n  Path: /home/user/.local/share/gnome-shell/extensions/" + uuid +
			"\n  Version: 12\n  Enabled: Yes\n  State: " + state + "\n\n"
	}
	fake := func(out string) *sys.Fake { return &sys.Fake{Commands: map[string]string{list: out}} }

	t.Run("one crashed, one outdated", func(t *testing.T) {
		r := run(t, Apps, fake(ext("dock@example.org", "Example Dock", "ERROR")+ext("tray@example.org", "Tray Icons", "OUT OF DATE")+
			ext("clock@example.org", "Clock", "ACTIVE")), gnome)
		expect(t, r, doctor.Warn, "GNOME couldn't load 1 extension")
		if len(r.Findings) < 2 || r.Findings[1].Title != "1 extension doesn't support this GNOME version yet" {
			t.Errorf("findings = %+v", r.Findings)
		}
		f := r.Findings[0]
		if c := f.Fixes[len(f.Fixes)-1]; c.Command != "gnome-extensions disable dock@example.org" || c.Title != "Switch off Example Dock" {
			t.Errorf("fix = %+v", c)
		}
		if f.Evidence[0] != "Example Dock (dock@example.org)" {
			t.Errorf("evidence = %v", f.Evidence)
		}
	})
	t.Run("all running", func(t *testing.T) {
		r := run(t, Apps, fake(ext("clock@example.org", "Clock", "ACTIVE")), gnome)
		for _, f := range r.Findings {
			if f.Status >= doctor.Info && f.Title != "" && f.Evidence != nil && f.Evidence[0] == "Clock (clock@example.org)" {
				t.Errorf("unexpected %+v", f)
			}
		}
	})
	t.Run("not GNOME", func(t *testing.T) {
		r := run(t, Apps, fake(ext("dock@example.org", "Example Dock", "ERROR")), &doctor.Host{ID: "arch", Desktop: "KDE"})
		for _, f := range r.Findings {
			if f.Status == doctor.Warn && f.Title == "GNOME couldn't load 1 extension" {
				t.Error("checked extensions outside GNOME")
			}
		}
	})
}
