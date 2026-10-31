package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Apps = doctor.Check{
	ID:       "apps",
	Name:     "Screen sharing & app sandbox",
	Category: "System",
	Question: "Why is screen sharing black, or why don't Flatpak apps open files?",
	Run:      runApps,
}

const (
	ifaceScreenCast  = "org.freedesktop.impl.portal.ScreenCast"
	ifaceFileChooser = "org.freedesktop.impl.portal.FileChooser"
	portalDir        = "/usr/share/xdg-desktop-portal"
)

func runApps(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Container {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No desktop session inside a container"}}
	}
	var fs []doctor.Finding
	fs = append(fs, portals(ctx, s, h)...)
	fs = append(fs, snaps(ctx, s)...)
	fs = append(fs, fileWatches(s)...)
	fs = append(fs, brokenExtensions(ctx, s, h)...)
	return fs
}

// brokenExtensions lists GNOME Shell extensions the running shell refused
// to load, the usual aftermath of a GNOME upgrade: a missing dock or tray
// icons, or a panel that looks different.
func brokenExtensions(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Desktop != "GNOME" || !s.Has("gnome-extensions") {
		return nil
	}
	out, err := s.Run(ctx, "gnome-extensions", "list", "--enabled", "--details")
	if err != nil {
		return nil
	}
	var errored, outdated []string
	var uuid, name, firstErr string
	for _, l := range strings.Split(string(out), "\n") {
		t := strings.TrimSpace(l)
		switch {
		case t == "":
		case !strings.HasPrefix(l, " ") && !strings.HasPrefix(l, "\t"):
			uuid, name = t, t
		case strings.HasPrefix(t, "Name:"):
			name = strings.TrimSpace(strings.TrimPrefix(t, "Name:"))
		case t == "State: ERROR":
			if firstErr == "" {
				firstErr = uuid
			}
			errored = append(errored, name+" ("+uuid+")")
		case t == "State: OUT OF DATE":
			outdated = append(outdated, name+" ("+uuid+")")
		}
	}
	var fs []doctor.Finding
	fixes := []doctor.Fix{
		{Title: "Update them in the Extension Manager app, or on extensions.gnome.org", URL: "https://extensions.gnome.org/local/"},
	}
	if len(errored) > 0 {
		f := doctor.Finding{
			Status: doctor.Warn,
			Title:  "GNOME couldn't load " + plural(len(errored), "extension", "extensions"),
			Explain: "These extensions crashed while starting, so whatever they add (a dock, tray icons, a panel tweak) is missing. " +
				"A broken extension can also make the desktop slow or unstable until it's switched off.",
			Fixes:    fixes,
			Evidence: errored,
		}
		f.Fixes = append(f.Fixes, doctor.Fix{Title: "Switch off " + strings.SplitN(errored[0], " (", 2)[0], Command: "gnome-extensions disable " + firstErr})
		fs = append(fs, f)
	}
	if len(outdated) > 0 {
		fs = append(fs, doctor.Finding{
			Status: doctor.Info,
			Title:  plural(len(outdated), "extension doesn't", "extensions don't") + " support this GNOME version yet",
			Explain: "GNOME updated and these extensions haven't caught up, so they're switched on but not running. " +
				"An update usually arrives within weeks of a GNOME release.",
			Fixes:    fixes,
			Evidence: outdated,
		})
	}
	return fs
}

// watchHungry apps watch every file in a project or synced folder, one
// inotify watch each.
var watchHungry = []string{"code", "code-oss", "codium", "cursor", "idea", "pycharm", "webstorm", "goland",
	"clion", "rider", "phpstorm", "rustrover", "dropbox", "syncthing", "nextcloud", "insync", "zed-editor", "zed"}

func fileWatches(s sys.System) []doctor.Finding {
	limit, err := strconv.Atoi(readTrim(s, "/proc/sys/fs/inotify/max_user_watches"))
	if err != nil || limit >= 100000 {
		return nil
	}
	app := running(s, watchHungry...)
	if app == "" {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Info,
		Title:  fmt.Sprintf("%s can only watch %d files for changes", app, limit),
		Explain: "Editors and sync apps ask Linux to tell them when files change, one file at a time. With a big project or " +
			"synced folder they hit this limit, then show \"Unable to watch for file changes\" or stop syncing new files.",
		Fixes: []doctor.Fix{{
			Title:     "Raise the limit to 524288 (uses at most about 500 MB of memory, only if every watch is in use)",
			Command:   "echo fs.inotify.max_user_watches=524288 | sudo tee /etc/sysctl.d/90-inotify.conf && sudo sysctl --system",
			NeedsRoot: true,
		}},
	}}
}

type portalFile struct {
	name   string
	ifaces map[string]bool
	useIn  []string
}

func desktopNames(h *doctor.Host) []string {
	var out []string
	for _, d := range strings.Split(strings.ToLower(h.Desktop), ":") {
		if d = strings.TrimSpace(d); d != "" {
			out = append(out, d)
		}
	}
	return out
}

// portalProvides reports whether some installed portal backend offers iface to
// one of the desktops, either through its UseIn line or a <desktop>-portals.conf.
// It errs on the side of "yes": a missing warning beats a wrong one.
func portalProvides(s sys.System, desktops []string, iface string) bool {
	files, _ := s.Glob(portalDir + "/portals/*.portal")
	var ps []portalFile
	for _, f := range files {
		kv := parseKV(readTrim(s, f))
		p := portalFile{name: strings.TrimSuffix(filepath.Base(f), ".portal"), ifaces: map[string]bool{}}
		for _, i := range strings.Split(kv["Interfaces"], ";") {
			p.ifaces[strings.TrimSpace(i)] = true
		}
		p.useIn = strings.Split(strings.ToLower(kv["UseIn"]), ";")
		ps = append(ps, p)
	}
	preferred := map[string]bool{}
	for _, d := range desktops {
		for _, dir := range []string{portalDir, "/etc/xdg/xdg-desktop-portal"} {
			kv := parseKV(readTrim(s, dir+"/"+d+"-portals.conf"))
			list := kv[iface]
			if list == "" {
				list = kv["default"]
			}
			for _, n := range strings.Split(list, ";") {
				preferred[strings.TrimSpace(n)] = true
			}
		}
	}
	for _, p := range ps {
		if !p.ifaces[iface] {
			continue
		}
		if preferred[p.name] || preferred["*"] {
			return true
		}
		for _, u := range p.useIn {
			for _, d := range desktops {
				if u == d {
					return true
				}
			}
		}
	}
	return false
}

// portalPackage names the backend package that implements portals for a desktop.
func portalPackage(desktops []string) string {
	for _, d := range desktops {
		switch d {
		case "gnome", "ubuntu", "unity", "pop", "budgie":
			return "xdg-desktop-portal-gnome"
		case "kde", "plasma":
			return "xdg-desktop-portal-kde"
		case "hyprland":
			return "xdg-desktop-portal-hyprland"
		case "cosmic":
			return "xdg-desktop-portal-cosmic"
		case "sway", "river", "wayfire", "labwc", "niri", "wlroots":
			return "xdg-desktop-portal-wlr"
		}
	}
	return ""
}

func portals(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	wayland := h.Session == "wayland"
	flatpak := s.Has("flatpak")
	desktops := desktopNames(h)
	if !wayland && !flatpak || len(desktops) == 0 {
		return nil
	}
	pkg := portalPackage(desktops)
	install := func() []doctor.Fix {
		if pkg == "" {
			return []doctor.Fix{{Title: "Install the xdg-desktop-portal backend made for your desktop"}}
		}
		return []doctor.Fix{installFix(h, pkg)}
	}

	if !s.Exists(portalDir + "/portals") {
		return []doctor.Finding{{
			Status: doctor.Fail,
			Title:  "Desktop portals aren't installed",
			Explain: "Portals are how sandboxed and Wayland apps ask the desktop for things: sharing the screen, opening files, taking screenshots. " +
				"Without them, screen sharing shows a black window and Flatpak apps can't open or save files.",
			Fixes: append([]doctor.Fix{installFix(h, "xdg-desktop-portal")}, install()...),
		}}
	}

	var fs []doctor.Finding
	out, _ := s.Run(ctx, "systemctl", "--user", "is-active", "xdg-desktop-portal")
	if strings.TrimSpace(string(out)) == "failed" {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Fail,
			Title:   "The desktop portal service crashed",
			Explain: "Screen sharing, file dialogs in Flatpak apps and screenshots go through this service, so they all fail until it runs again.",
			Fixes:   []doctor.Fix{{Title: "Restart it", Command: "systemctl --user restart xdg-desktop-portal"}},
		})
	}

	if wayland {
		switch {
		case !portalProvides(s, desktops, ifaceScreenCast):
			fs = append(fs, doctor.Finding{
				Status: doctor.Warn,
				Title:  "Screen sharing won't work in this Wayland session",
				Explain: "On Wayland, apps like Zoom, Teams, Discord and browsers can only share your screen through a portal that supports it. " +
					"None of the installed portals does that for your desktop, so shared screens show up black or the share button does nothing.",
				Fixes: append(install(), doctor.Fix{Title: "Or log in with an X11 session from the login screen's options"}),
			})
		case !s.Has("pipewire"):
			fs = append(fs, doctor.Finding{
				Status:  doctor.Warn,
				Title:   "Screen sharing needs PipeWire, which isn't installed",
				Explain: "On Wayland the picture of your screen travels to the app through PipeWire.",
				Fixes:   []doctor.Fix{installFix(h, "pipewire")},
			})
		default:
			fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "Screen sharing is set up for Wayland"})
		}
	}

	if flatpak && !portalProvides(s, desktops, ifaceFileChooser) {
		fs = append(fs, doctor.Finding{
			Status:  doctor.Warn,
			Title:   "Flatpak apps can't show Open and Save dialogs",
			Explain: "Sandboxed apps ask a portal to show the file dialog. No installed portal provides one for your desktop, so Open and Save do nothing.",
			Fixes:   append(install(), installFix(h, "xdg-desktop-portal-gtk")),
		})
	}
	return fs
}

func snaps(ctx context.Context, s sys.System) []doctor.Finding {
	if !s.Has("snap") {
		return nil
	}
	var fs []doctor.Finding
	if out, err := s.Run(ctx, "snap", "changes", "--abs-time"); err == nil {
		for _, line := range strings.Split(string(out), "\n")[1:] {
			// ID Status Spawn Ready Summary...
			f := strings.Fields(line)
			if len(f) > 4 && f[1] == "Doing" {
				fs = append(fs, doctor.Finding{
					Status:  doctor.Info,
					Title:   "A snap change is still running: " + strings.Join(f[4:], " "),
					Explain: "Snap apps being updated can't start until it finishes. If it has been running for a long time, it may be stuck.",
					Fixes: []doctor.Fix{
						{Title: "See what it is doing", Command: "snap tasks " + f[0]},
						{Title: "Cancel it if it's stuck", Command: "sudo snap abort " + f[0], NeedsRoot: true},
					},
				})
			}
		}
	}
	if out, err := s.Run(ctx, "snap", "connections", "--all"); err == nil {
		var apps []string
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) >= 3 && f[0] == "removable-media" && f[2] == "-" {
				apps = append(apps, strings.TrimSuffix(f[1], ":removable-media"))
			}
		}
		if len(apps) > 0 {
			var fixes []doctor.Fix
			for _, a := range apps {
				fixes = append(fixes, doctor.Fix{Title: "Let " + a + " see USB drives and other disks", Command: "sudo snap connect " + a + ":removable-media", NeedsRoot: true})
			}
			fs = append(fs, doctor.Finding{
				Status: doctor.Info,
				Title:  "Some snap apps can't open files on USB drives or other disks: " + strings.Join(apps, ", "),
				Explain: "Snap apps only see your home folder unless they're allowed more. Files under /media and /mnt don't show up " +
					"in their Open dialog, and saving there fails.",
				Fixes: fixes,
			})
		}
	}
	if out, err := s.Run(ctx, "snap", "list", "--all"); err == nil {
		old := strings.Count(string(out), "disabled")
		if old >= 3 {
			fs = append(fs, doctor.Finding{
				Status:  doctor.Info,
				Title:   fmt.Sprintf("Snap keeps %s of apps you already updated", plural(old, "old version", "old versions")),
				Explain: "Snap keeps previous versions so it can roll back an update. They take up disk space and are safe to remove.",
				Fixes: []doctor.Fix{
					{
						Title:     "Remove the old versions",
						Command:   `snap list --all | awk '/disabled/{print $1, $3}' | while read -r name rev; do sudo snap remove "$name" --revision="$rev"; done`,
						NeedsRoot: true,
					},
					{Title: "Keep only 2 versions from now on", Command: "sudo snap set system refresh.retain=2", NeedsRoot: true},
				},
			})
		}
	}
	return fs
}
