package checks

import (
	"strings"
	"testing"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestAptListName(t *testing.T) {
	for _, tc := range []struct{ uri, suite, want string }{
		{"http://archive.ubuntu.com/ubuntu/", "noble-updates", "archive.ubuntu.com_ubuntu_dists_noble-updates_"},
		{"https://deb.example.org/node_22.x", "nodistro", "deb.example.org_node%5f22.x_dists_nodistro_"},
		{"https://user:secret@repo.example.org/apt", "stable", "repo.example.org_apt_dists_stable_"},
		{"http://mirror.example.org:8080/debian", "trixie", "mirror.example.org:8080_debian_dists_trixie_"},
	} {
		if got, ok := aptListName(tc.uri, tc.suite); !ok || got != tc.want {
			t.Errorf("aptListName(%q, %q) = %q, want %q", tc.uri, tc.suite, got, tc.want)
		}
	}
	for _, tc := range [][2]string{{"cdrom:[Ubuntu 24.04]/", "noble"}, {"file:/srv/repo", "stable"}, {"http://example.org/flat", "./"}} {
		if _, ok := aptListName(tc[0], tc[1]); ok {
			t.Errorf("%v should be skipped", tc)
		}
	}
}

func TestStaleArch(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	log := "[2026-03-02T09:00:00+0100] [PACMAN] Running 'pacman -Syu'\n" +
		"[2026-03-02T09:00:00+0100] [PACMAN] starting full system upgrade\n" +
		"[2026-03-02T09:05:12+0100] [ALPM] upgraded mesa (25.0.1-1 -> 25.0.2-1)\n" +
		"[2026-04-10T18:00:00+0200] [PACMAN] Running 'pacman -S htop'\n"
	fake := func(log string) *sys.Fake {
		return &sys.Fake{Files: map[string]string{"/var/log/pacman.log": log}, Clock: now}
	}

	t.Run("eight months without a full upgrade", func(t *testing.T) {
		r := run(t, Updates, fake(log), &doctor.Host{ID: "arch", PackageMgr: "pacman"})
		expect(t, r, doctor.Info, "hasn't been fully updated in 243 days")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo pacman -Sy --needed archlinux-keyring && sudo pacman -Su" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("manjaro has its own keyring too", func(t *testing.T) {
		r := run(t, Updates, fake(log), &doctor.Host{ID: "manjaro", IDLike: []string{"arch"}, PackageMgr: "pacman"})
		if c := r.Findings[0].Fixes[0].Command; !strings.Contains(c, "archlinux-keyring manjaro-keyring") {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("old log format, recent upgrade", func(t *testing.T) {
		r := run(t, Updates, fake("[2026-10-20 08:00] [PACMAN] starting full system upgrade\n"), &doctor.Host{ID: "arch", PackageMgr: "pacman"})
		expect(t, r, doctor.OK, "healthy")
	})
	t.Run("no log", func(t *testing.T) {
		expect(t, run(t, Updates, fake(""), &doctor.Host{ID: "arch", PackageMgr: "pacman"}), doctor.OK, "healthy")
	})
}

func TestFlathubOff(t *testing.T) {
	q := "flatpak remotes --columns=name,options"
	h := &doctor.Host{ID: "fedora", PackageMgr: "dnf"}
	fake := func(out string) *sys.Fake { return &sys.Fake{Commands: map[string]string{q: out}} }

	expect(t, run(t, Updates, fake("fedora\tsystem,oci\nflathub\tsystem,filtered\n"), h), doctor.Info, "Flathub is switched off or limited")
	expect(t, run(t, Updates, fake("flathub\tsystem,disabled\n"), h), doctor.Info, "Flathub is switched off or limited")
	r := run(t, Updates, fake("fedora\tsystem,oci\n"), h)
	expect(t, r, doctor.Info, "Flatpak is set up without Flathub")
	if c := r.Findings[0].Fixes[0].Command; c != "flatpak remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo" {
		t.Errorf("fix = %q", c)
	}
	expect(t, run(t, Updates, fake("flathub\tsystem,filtered\nflathub\tuser\n"), h), doctor.OK, "healthy")
	expect(t, run(t, Updates, fake(""), h), doctor.OK, "healthy")
}

func TestDeadAptSources(t *testing.T) {
	h := &doctor.Host{ID: "ubuntu", PackageMgr: "apt"}
	f := func(extra map[string]string) *sys.Fake {
		files := map[string]string{
			"/etc/apt/sources.list": "# deb cdrom:[Ubuntu 24.04]/ noble main\n",
			"/etc/apt/sources.list.d/ubuntu.sources": "Types: deb\nURIs: http://archive.ubuntu.com/ubuntu/\nSuites: noble noble-updates\nComponents: main\n" +
				"Signed-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n\nTypes: deb-src\nURIs: http://archive.ubuntu.com/ubuntu/\nSuites: noble-backports\n",
			aptLists + "archive.ubuntu.com_ubuntu_dists_noble_InRelease":         "x",
			aptLists + "archive.ubuntu.com_ubuntu_dists_noble-updates_InRelease": "x",
		}
		for k, v := range extra {
			files[k] = v
		}
		return &sys.Fake{Files: files}
	}

	t.Run("all downloaded", func(t *testing.T) {
		expect(t, run(t, Updates, f(nil), h), doctor.OK, "package manager is healthy")
	})
	t.Run("a PPA that dropped this release", func(t *testing.T) {
		r := run(t, Updates, f(map[string]string{
			"/etc/apt/sources.list.d/oldtool.list": "deb [signed-by=/etc/apt/keyrings/oldtool.gpg] https://ppa.launchpadcontent.net/someone/oldtool/ubuntu/ noble main\n",
		}), h)
		expect(t, r, doctor.Warn, "apt can't download from https://ppa.launchpadcontent.net/someone/oldtool/ubuntu/")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo add-apt-repository --remove ppa:someone/oldtool" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("disabled deb822 stanza and a Release-only repo", func(t *testing.T) {
		r := run(t, Updates, f(map[string]string{
			"/etc/apt/sources.list.d/off.sources":                    "Enabled: no\nTypes: deb\nURIs: https://off.example.org/apt\nSuites: stable\n",
			"/etc/apt/sources.list.d/flat.list":                      "deb https://old.example.org/debian stable main\n",
			aptLists + "old.example.org_debian_dists_stable_Release": "x",
		}), h)
		expect(t, r, doctor.OK, "healthy")
	})
	t.Run("two broken", func(t *testing.T) {
		r := run(t, Updates, f(map[string]string{
			"/etc/apt/sources.list.d/a.list":    "deb https://a.example.org/apt stable main\n",
			"/etc/apt/sources.list.d/b.sources": "Types: deb\nURIs: https://b.example.org/apt\nSuites: stable\n",
		}), h)
		expect(t, r, doctor.Warn, "apt can't download from 2 sources")
		if ev := strings.Join(r.Findings[0].Evidence, "\n"); !strings.Contains(ev, "a.list: https://a.example.org/apt stable") {
			t.Errorf("evidence = %s", ev)
		}
	})
	t.Run("apt update never ran", func(t *testing.T) {
		fk := &sys.Fake{Files: map[string]string{"/etc/apt/sources.list.d/a.list": "deb https://a.example.org/apt stable main\n"}}
		expect(t, run(t, Updates, fk, h), doctor.OK, "healthy")
	})
}
