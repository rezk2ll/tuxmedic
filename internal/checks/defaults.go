package checks

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Defaults = doctor.Check{
	ID:       "defaults",
	Name:     "Default apps",
	Category: "System",
	Question: "Why do links or files open nothing, or open in the wrong app?",
	Run:      runDefaults,
}

var knownBrowsers = []string{
	"firefox.desktop", "org.mozilla.firefox.desktop", "firefox_firefox.desktop", "firefox-esr.desktop",
	"google-chrome.desktop", "com.google.Chrome.desktop", "chromium.desktop", "chromium_chromium.desktop",
	"org.chromium.Chromium.desktop", "brave-browser.desktop", "com.brave.Browser.desktop",
	"microsoft-edge.desktop", "vivaldi-stable.desktop", "librewolf.desktop", "io.gitlab.librewolf-community.desktop",
	"org.gnome.Epiphany.desktop", "zen.desktop", "app.zen_browser.zen.desktop",
}

func appDirs(s sys.System) []string {
	home := s.Getenv("HOME")
	data := s.Getenv("XDG_DATA_HOME")
	if data == "" && home != "" {
		data = filepath.Join(home, ".local", "share")
	}
	dirs := s.Getenv("XDG_DATA_DIRS")
	if dirs == "" {
		dirs = "/usr/local/share:/usr/share"
	}
	var out []string
	if data != "" {
		out = append(out, data+"/applications", data+"/flatpak/exports/share/applications")
	}
	for _, d := range strings.Split(dirs, ":") {
		if d != "" {
			out = append(out, d+"/applications")
		}
	}
	return append(out, "/var/lib/flatpak/exports/share/applications", "/var/lib/snapd/desktop/applications")
}

// installedApp finds a desktop file the way the desktop does, including the
// vendor-prefix form where "kde-okular.desktop" lives at kde/okular.desktop.
func installedApp(s sys.System, dirs []string, app string) bool {
	for _, d := range dirs {
		if _, err := s.ReadFile(d + "/" + app); err == nil {
			return true
		}
		if strings.Contains(app, "-") {
			if _, err := s.ReadFile(d + "/" + strings.Replace(app, "-", "/", 1)); err == nil {
				return true
			}
		}
	}
	return false
}

func runDefaults(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Desktop == "" || !s.Has("xdg-mime") {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No desktop session, so no default apps to check"}}
	}
	dirs := appDirs(s)
	kinds := []struct{ mime, what string }{
		{"x-scheme-handler/https", "Web links"},
		{"inode/directory", "Folders"},
		{"application/pdf", "PDF files"},
	}
	var fs []doctor.Finding
	for _, k := range kinds {
		out, _ := s.Run(ctx, "xdg-mime", "query", "default", k.mime)
		app := strings.TrimSpace(firstLineOf(string(out)))
		browser := k.mime == "x-scheme-handler/https"
		switch {
		case app == "" && browser:
			fs = append(fs, doctor.Finding{
				Status:  doctor.Warn,
				Title:   "No default web browser is set",
				Explain: "Links in other apps, like email or chat, have nothing to open them, so clicking them does nothing.",
				Fixes:   browserFixes(s, dirs),
			})
		case app == "":
		case !installedApp(s, dirs, app):
			f := doctor.Finding{
				Status: doctor.Warn,
				Title:  k.what + " open nothing: their default app (" + strings.TrimSuffix(app, ".desktop") + ") isn't installed anymore",
				Explain: "The setting still points to an app that was removed, or that moved to a Flatpak or Snap with a different name. " +
					"Clicking fails without a message.",
				Evidence: []string{k.mime + " → " + app},
			}
			if browser {
				f.Fixes = browserFixes(s, dirs)
			} else {
				f.Fixes = []doctor.Fix{{Title: "Pick another app in Settings, under Default Applications, or right-click a file and choose Open With"}}
			}
			fs = append(fs, f)
		}
	}
	if len(fs) == 0 {
		return []doctor.Finding{{Status: doctor.OK, Title: "Links, folders and PDFs open in apps that are installed"}}
	}
	return fs
}

func browserFixes(s sys.System, dirs []string) []doctor.Fix {
	for _, b := range knownBrowsers {
		if installedApp(s, dirs, b) {
			return []doctor.Fix{
				{Title: "Use " + strings.TrimSuffix(b, ".desktop") + " for links", Command: "xdg-settings set default-web-browser " + b},
				{Title: "Or pick another in Settings, under Default Applications"},
			}
		}
	}
	return []doctor.Fix{{Title: "Install a web browser, then pick it in Settings, under Default Applications"}}
}
