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
	fs := append(languages(ctx, s, h), emojiFont(ctx, s, h)...)
	return append(fs, officeFonts(ctx, s, h)...)
}

// officeFonts checks that the fonts most documents are written in resolve
// to themselves or to a font with the same letter widths. Any other
// substitute reflows the text, so pages break in different places.
func officeFonts(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Desktop == "" || !s.Has("fc-match") || !s.Has("libreoffice") && !s.Has("soffice") {
		return nil
	}
	same := map[string][]string{
		"Arial":           {"Arial", "Liberation Sans", "Arimo"},
		"Times New Roman": {"Times New Roman", "Liberation Serif", "Tinos"},
		"Calibri":         {"Calibri", "Carlito"},
		"Cambria":         {"Cambria", "Caladea"},
	}
	var missing []string
	for _, font := range []string{"Arial", "Times New Roman", "Calibri", "Cambria"} {
		out, err := s.Run(ctx, "fc-match", "-f", "%{family}", font)
		if err != nil {
			return nil
		}
		got, _, _ := strings.Cut(strings.TrimSpace(string(out)), ",")
		if !slices.Contains(same[font], got) {
			missing = append(missing, font+" (shown as "+got+")")
		}
	}
	if len(missing) == 0 {
		return nil
	}
	pkgs := map[string]string{
		"apt":    "sudo apt install fonts-liberation fonts-crosextra-carlito fonts-crosextra-caladea",
		"dnf":    "sudo dnf install liberation-fonts google-carlito-fonts google-caladea-fonts",
		"pacman": "sudo pacman -S ttf-liberation ttf-carlito ttf-caladea",
		"zypper": "sudo zypper install liberation-fonts google-carlito-fonts google-caladea-fonts",
	}
	fix := doctor.Fix{Title: "Install the Liberation, Carlito and Caladea fonts"}
	if c := pkgs[h.PackageMgr]; c != "" {
		fix.Command, fix.NeedsRoot = c, true
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  "Word documents may look different here: " + plural(len(missing), "common font has", "common fonts have") + " no match",
		Explain: "Documents from Windows and Mac mostly use Arial, Times New Roman, Calibri and Cambria. Free fonts with the exact same letter widths exist; " +
			"without them, a substitute takes the space differently and text moves to other lines and pages.",
		Fixes:    []doctor.Fix{fix},
		Evidence: missing,
	}}
}

// emojiFont looks for a font that declares emoji (fontconfig's und-zsye).
// A charset query isn't enough: DejaVu has a few black-and-white smileys.
func emojiFont(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Desktop == "" || !s.Has("fc-list") {
		return nil
	}
	out, err := s.Run(ctx, "fc-list", ":lang=und-zsye", "family")
	if err != nil || strings.TrimSpace(string(out)) != "" {
		return nil
	}
	return []doctor.Finding{{
		Status:  doctor.Info,
		Title:   "Emoji show up as empty boxes: no emoji font is installed",
		Explain: "Chat apps, websites and the terminal need a font with emoji. Without one, each emoji is drawn as a box or a question mark.",
		Fixes:   []doctor.Fix{installFix(h, "emoji-font"), {Title: "Then close and reopen the apps that show boxes"}},
	}}
}

func languages(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
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
