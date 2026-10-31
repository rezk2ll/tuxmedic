package checks

import (
	"testing"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func TestLocale(t *testing.T) {
	installed := "C\nC.utf8\nPOSIX\nen_US.utf8\n"
	fake := func(env map[string]string) *sys.Fake {
		return &sys.Fake{Commands: map[string]string{"locale -a": installed}, Env: env}
	}

	t.Run("installed", func(t *testing.T) {
		expect(t, run(t, Locale, fake(map[string]string{"LANG": "en_US.UTF-8"}), nil), doctor.OK, "installed and use UTF-8")
	})
	t.Run("C.UTF-8 is always there", func(t *testing.T) {
		expect(t, run(t, Locale, fake(map[string]string{"LC_ALL": "C.UTF-8"}), nil), doctor.OK, "installed")
	})
	t.Run("missing on Debian", func(t *testing.T) {
		r := run(t, Locale, fake(map[string]string{"LANG": "en_US.UTF-8", "LC_TIME": "de_DE.UTF-8"}), &doctor.Host{ID: "debian", PackageMgr: "apt"})
		expect(t, r, doctor.Warn, "isn't installed: de_DE.UTF-8")
		f := r.Findings[0]
		if want := "sudo sed -i 's/^# *de_DE.UTF-8 /de_DE.UTF-8 /' /etc/locale.gen && sudo locale-gen"; f.Fixes[0].Command != want {
			t.Errorf("fix = %q\nwant  %q", f.Fixes[0].Command, want)
		}
		if len(f.Evidence) != 1 || f.Evidence[0] != "LC_TIME=de_DE.UTF-8" {
			t.Errorf("evidence = %v", f.Evidence)
		}
	})
	t.Run("missing on Ubuntu, lower-case codeset from ssh", func(t *testing.T) {
		r := run(t, Locale, fake(map[string]string{"LANG": "fr_FR.utf8"}), &doctor.Host{ID: "ubuntu", PackageMgr: "apt"})
		expect(t, r, doctor.Warn, "fr_FR.utf8")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo locale-gen fr_FR.UTF-8" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("missing on Fedora", func(t *testing.T) {
		r := run(t, Locale, fake(map[string]string{"LANG": "ja_JP.UTF-8"}), &doctor.Host{ID: "fedora", PackageMgr: "dnf"})
		if c := r.Findings[0].Fixes[0].Command; c != "sudo dnf install glibc-langpack-ja" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("not UTF-8", func(t *testing.T) {
		f := &sys.Fake{Commands: map[string]string{"locale -a": installed + "en_US\nen_US.iso88591\n"}, Env: map[string]string{"LANG": "en_US.ISO-8859-1"}}
		expect(t, run(t, Locale, f, nil), doctor.Info, "doesn't use UTF-8 (en_US.ISO-8859-1)")
	})
	t.Run("no locale command", func(t *testing.T) {
		expect(t, run(t, Locale, &sys.Fake{}, nil), doctor.Skipped, "locale command is missing")
	})
}

func TestNormLocale(t *testing.T) {
	for in, want := range map[string]string{
		"en_US.UTF-8": "en_US.utf8", "en_US.utf8": "en_US.utf8", "sr_RS.UTF-8@latin": "sr_RS.utf8@latin", "C": "C",
	} {
		if got := normLocale(in); got != want {
			t.Errorf("normLocale(%q) = %q, want %q", in, got, want)
		}
	}
}
