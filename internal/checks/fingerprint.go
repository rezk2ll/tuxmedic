package checks

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Fingerprint = doctor.Check{
	ID:       "fingerprint",
	Name:     "Fingerprint reader",
	Category: "Devices",
	Question: "Why can't I log in with my fingerprint?",
	Run:      runFingerprint,
}

// fingerprintVendors make laptop fingerprint readers. Their readers show up
// as vendor-specific USB devices (interface class ff); the same vendors'
// touchscreens are HID devices and are left out by that.
var fingerprintVendors = map[string]string{
	"06cb": "Synaptics", "27c6": "Goodix", "138a": "Validity", "04f3": "Elan", "1c7a": "Egis",
	"10a5": "FPC", "2808": "FocalTech", "147e": "Upek", "08ff": "AuthenTec", "298d": "Next Biometrics",
}

type reader struct{ vendor, id string }

func findReader(s sys.System) (reader, bool) {
	devs, _ := s.Glob("/sys/bus/usb/devices/*")
	for _, d := range devs {
		v := readTrim(s, d+"/idVendor")
		name, ok := fingerprintVendors[v]
		if !ok {
			continue
		}
		product := strings.ToLower(readTrim(s, d+"/product"))
		ifaces, _ := s.Glob(d + "/" + filepath.Base(d) + ":*/bInterfaceClass")
		vendorClass := false
		for _, i := range ifaces {
			if readTrim(s, i) == "ff" {
				vendorClass = true
			}
		}
		if vendorClass || strings.Contains(product, "finger") || strings.Contains(product, "fpr") {
			return reader{name, v + ":" + readTrim(s, d+"/idProduct")}, true
		}
	}
	return reader{}, false
}

func runFingerprint(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	r, ok := findReader(s)
	if !ok {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "No fingerprint reader found"}}
	}
	what := "your " + r.vendor + " fingerprint reader (" + r.id + ")"
	if !s.Has("fprintd-list") {
		fix := installFix(h, "fprintd")
		if h.PackageMgr == "apt" {
			fix = doctor.Fix{Title: "Install fingerprint support", Command: "sudo apt install fprintd libpam-fprintd", NeedsRoot: true}
		}
		return []doctor.Finding{{
			Status:  doctor.Warn,
			Title:   "Fingerprint support isn't installed",
			Explain: "This laptop has " + what + ", but the fprintd service that reads it isn't installed, so the login screen can't offer it.",
			Fixes:   []doctor.Fix{fix},
		}}
	}

	user := h.User
	if user == "" {
		user = s.Getenv("USER")
	}
	out, _ := s.Run(ctx, "fprintd-list", user)
	text := string(out)
	switch {
	case strings.Contains(text, "No devices available") || strings.Contains(text, "found 0 devices"):
		return []doctor.Finding{{
			Status: doctor.Warn,
			Title:  "Linux doesn't support " + what + " yet",
			Explain: "fprintd is installed but has no driver for this reader. Some readers only work after a firmware update, " +
				"some have a driver from the laptop maker, and some don't work on Linux at all.",
			Fixes: []doctor.Fix{
				{Title: "Look up " + r.id + " in the list of supported readers", URL: "https://fprint.freedesktop.org/supported-devices.html"},
				{Title: "Check for a firmware update for the reader", Command: "fwupdmgr get-updates"},
			},
		}}
	case strings.Contains(text, "no fingers enrolled"):
		return []doctor.Finding{{
			Status:  doctor.Info,
			Title:   "No fingerprint is saved yet",
			Explain: "The reader works, but you haven't added a finger. Add one in Settings, under Users, or from here.",
			Fixes:   []doctor.Fix{{Title: "Add your right index finger", Command: "fprintd-enroll"}},
		}}
	case strings.Contains(text, "#0:"):
		if f, off := fingerprintPAM(s); off {
			return []doctor.Finding{f}
		}
		return []doctor.Finding{{Status: doctor.OK, Title: "Fingerprint login is set up on " + what}}
	}
	return []doctor.Finding{{
		Status:   doctor.Info,
		Title:    "Couldn't ask the fingerprint service about " + what,
		Explain:  "fprintd didn't answer as expected. Restarting it often helps.",
		Fixes:    []doctor.Fix{{Title: "Restart the fingerprint service", Command: "sudo systemctl restart fprintd", NeedsRoot: true}},
		Evidence: []string{strings.TrimSpace(firstLineOf(text))},
	}}
}

// fingerprintPAM reports a saved finger that sudo and the password prompts
// never ask for. GDM has its own gdm-fingerprint stack, so the login screen
// can work while sudo doesn't.
func fingerprintPAM(s sys.System) (doctor.Finding, bool) {
	for _, dir := range []string{"/etc/pam.d/", "/usr/lib/pam.d/"} {
		for _, f := range []string{"common-auth", "system-auth", "sudo", "system-local-login", "fingerprint-auth"} {
			if strings.Contains(readTrim(s, dir+f), "pam_fprintd") {
				return doctor.Finding{}, false
			}
		}
	}
	var fix doctor.Fix
	switch {
	case s.Has("pam-auth-update"):
		fix = doctor.Fix{Title: "Turn on fingerprint for sudo and logins", Command: "sudo pam-auth-update --enable fprintd", NeedsRoot: true}
	case s.Has("authselect"):
		fix = doctor.Fix{Title: "Turn on fingerprint for sudo and logins", Command: "sudo authselect enable-feature with-fingerprint && sudo authselect apply-changes", NeedsRoot: true}
	case s.Has("pam-config"):
		fix = doctor.Fix{Title: "Turn on fingerprint for sudo and logins", Command: "sudo pam-config -a --fprintd", NeedsRoot: true}
	default:
		fix = doctor.Fix{Title: "Add \"auth sufficient pam_fprintd.so\" as the first auth line of /etc/pam.d/sudo", URL: "https://wiki.archlinux.org/title/Fprint#Configuration"}
	}
	title := "Your fingerprint is saved, but nothing asks for it"
	if readTrim(s, "/etc/pam.d/gdm-fingerprint") != "" || readTrim(s, "/usr/lib/pam.d/gdm-fingerprint") != "" {
		title = "Your fingerprint works at the login screen, but sudo still asks for a password"
	}
	return doctor.Finding{
		Status:  doctor.Info,
		Title:   title,
		Explain: "Fingerprint use is switched on per program in the system's login settings (PAM), and it isn't on for sudo or the password prompts here.",
		Fixes:   []doctor.Fix{fix},
	}, true
}
