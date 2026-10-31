package checks

import (
	"context"
	"strings"
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
	t.Run("no emoji font", func(t *testing.T) {
		f := fake(map[string]string{"LANG": "en_US.UTF-8"})
		f.Commands["fc-list :lang=und-zsye family"] = ""
		r := run(t, Locale, f, &doctor.Host{ID: "arch", PackageMgr: "pacman", Desktop: "KDE"})
		expect(t, r, doctor.Info, "Emoji show up as empty boxes")
		if c := r.Findings[0].Fixes[0].Command; c != "sudo pacman -S noto-fonts-emoji" {
			t.Errorf("fix = %q", c)
		}
	})
	t.Run("emoji font installed", func(t *testing.T) {
		f := fake(map[string]string{"LANG": "en_US.UTF-8"})
		f.Commands["fc-list :lang=und-zsye family"] = "Noto Color Emoji\n"
		r := run(t, Locale, f, &doctor.Host{ID: "arch", PackageMgr: "pacman", Desktop: "KDE"})
		if len(r.Findings) != 1 || r.Status != doctor.OK {
			t.Errorf("findings = %+v", r.Findings)
		}
	})
	t.Run("no locale command", func(t *testing.T) {
		expect(t, run(t, Locale, &sys.Fake{}, nil), doctor.Skipped, "locale command is missing")
	})
}

func TestOfficeFonts(t *testing.T) {
	h := &doctor.Host{ID: "fedora", PackageMgr: "dnf", Desktop: "GNOME"}
	fake := func(arial, times, calibri, cambria string) *sys.Fake {
		return &sys.Fake{Commands: map[string]string{
			"libreoffice --version":                 "",
			"fc-match -f %{family} Arial":           arial,
			"fc-match -f %{family} Times New Roman": times,
			"fc-match -f %{family} Calibri":         calibri,
			"fc-match -f %{family} Cambria":         cambria,
		}}
	}
	fs := officeFonts(context.Background(), fake("Liberation Sans", "Tinos", "DejaVu Sans", "DejaVu Serif,DejaVu Serif Condensed"), h)
	if len(fs) != 1 || !strings.Contains(fs[0].Title, "2 common fonts have no match") || fs[0].Evidence[1] != "Cambria (shown as DejaVu Serif)" {
		t.Fatalf("findings = %+v", fs)
	}
	if fs[0].Fixes[0].Command != "sudo dnf install liberation-fonts google-carlito-fonts google-caladea-fonts" {
		t.Errorf("fix = %q", fs[0].Fixes[0].Command)
	}
	if fs := officeFonts(context.Background(), fake("Arimo", "Times New Roman", "Carlito", "Caladea"), h); len(fs) != 0 {
		t.Errorf("all metric-compatible: %+v", fs)
	}
	if fs := officeFonts(context.Background(), fake("DejaVu Sans", "DejaVu Serif", "DejaVu Sans", "DejaVu Serif"), &doctor.Host{}); len(fs) != 0 {
		t.Errorf("no desktop: %+v", fs)
	}
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
