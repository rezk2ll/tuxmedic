package checks

import (
	"context"
	"slices"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Locale = doctor.Check{
	ID:       "locale",
	Name:     "Language & characters",
	Category: "System",
	Question: `Why do I see "Setting locale failed", or question marks instead of letters?`,
	Run:      runLocale,
}

// normLocale makes "en_US.UTF-8" and "en_US.utf8" compare equal.
func normLocale(l string) string {
	name, codeset, ok := strings.Cut(l, ".")
	if !ok {
		return l
	}
	codeset, mod, _ := strings.Cut(codeset, "@")
	out := name + "." + strings.ReplaceAll(strings.ToLower(codeset), "-", "")
	if mod != "" {
		out += "@" + mod
	}
	return out
}

func runLocale(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !s.Has("locale") {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Can't list the installed languages (the locale command is missing)"}}
	}
	out, err := s.Run(ctx, "locale", "-a")
	if err != nil {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Can't list the installed languages"}}
	}
	have := map[string]bool{}
	for _, l := range strings.Fields(string(out)) {
		have[normLocale(l)] = true
	}

	var missing, vars []string
	for _, v := range []string{"LC_ALL", "LANG", "LC_CTYPE", "LC_MESSAGES", "LC_TIME", "LC_NUMERIC"} {
		l := s.Getenv(v)
		if l == "" || l == "C" || l == "POSIX" || strings.HasPrefix(l, "C.") || have[normLocale(l)] {
			continue
		}
		vars = append(vars, v+"="+l)
		if !slices.Contains(missing, l) {
			missing = append(missing, l)
		}
	}
	if len(missing) > 0 {
		return []doctor.Finding{{
			Status: doctor.Warn,
			Title:  "Your session asks for a language that isn't installed: " + strings.Join(missing, ", "),
			Explain: "Programs fall back to plain English and ASCII. That's where " + `"Setting locale failed"` + " and " +
				`"cannot change locale"` + " warnings come from, and why accented letters can turn into question marks in the terminal.",
			Fixes:    localeFixes(h, missing),
			Evidence: vars,
		}}
	}

	lang := s.Getenv("LC_ALL")
	if lang == "" {
		lang = s.Getenv("LANG")
	}
	if lang != "" && lang != "C" && lang != "POSIX" && !strings.Contains(normLocale(lang), ".utf8") {
		return []doctor.Finding{{
			Status:  doctor.Info,
			Title:   "Your language setting doesn't use UTF-8 (" + lang + ")",
			Explain: "Without UTF-8, letters outside your language and most emoji can't be shown or saved correctly. Nearly every system uses UTF-8 today.",
			Fixes:   []doctor.Fix{{Title: "Pick a UTF-8 version of your language in your desktop's Region & Language settings, then log in again"}},
		}}
	}
	return []doctor.Finding{{Status: doctor.OK, Title: "Your language settings are installed and use UTF-8"}}
}

func localeFixes(h *doctor.Host, missing []string) []doctor.Fix {
	gen := func(l string) string {
		name, codeset, _ := strings.Cut(l, ".")
		if codeset == "" {
			codeset = "UTF-8"
		}
		return name + "." + strings.ToUpper(strings.ReplaceAll(strings.ToLower(codeset), "utf8", "utf-8"))
	}
	var fx []doctor.Fix
	for _, l := range missing {
		g := gen(l)
		lang, _, _ := strings.Cut(l, "_")
		switch {
		case h.ID == "ubuntu" || h.Like("ubuntu"):
			fx = append(fx, doctor.Fix{Title: "Install " + g, Command: "sudo locale-gen " + g, NeedsRoot: true})
		case h.PackageMgr == "apt" || h.PackageMgr == "pacman":
			fx = append(fx, doctor.Fix{
				Title:     "Turn on " + g + " in /etc/locale.gen and build it",
				Command:   "sudo sed -i 's/^# *" + g + " /" + g + " /' /etc/locale.gen && sudo locale-gen",
				NeedsRoot: true,
			})
		case h.PackageMgr == "dnf" || h.PackageMgr == "rpm-ostree":
			fx = append(fx, installFix(h, "glibc-langpack-"+lang))
		case h.PackageMgr == "zypper" || h.PackageMgr == "transactional-update":
			fx = append(fx, installFix(h, "glibc-locale"))
		}
	}
	return append(fx,
		doctor.Fix{Title: "Or pick an installed language in your desktop's Region & Language settings"},
		doctor.Fix{Title: "Over SSH, the setting comes from the computer you connect from; set LANG=C.UTF-8 there for this host, or install the language here"},
	)
}
