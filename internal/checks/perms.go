package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Permissions = doctor.Check{
	ID:       "permissions",
	Name:     "Permissions",
	Category: "System",
	Question: `Why do I get "permission denied" even after adding myself to a group?`,
	Run:      runPermissions,
}

// groupUse says what each well-known group unlocks, in words a user would recognise.
var groupUse = map[string]string{
	"docker":          "running Docker without sudo",
	"dialout":         "Arduino boards and serial devices",
	"uucp":            "Arduino boards and serial devices",
	"libvirt":         "virtual machines in virt-manager",
	"kvm":             "fast virtual machines",
	"vboxusers":       "USB devices in VirtualBox",
	"wireshark":       "capturing network traffic in Wireshark",
	"plugdev":         "USB gadgets like phones and security keys",
	"lp":              "printers",
	"lpadmin":         "managing printers",
	"input":           "game controllers and input remapping tools",
	"video":           "the webcam and screen brightness",
	"audio":           "sound devices",
	"adm":             "reading system logs",
	"systemd-journal": "reading system logs",
}

func runPermissions(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if s.IsRoot() {
		return []doctor.Finding{{Status: doctor.Skipped, Title: "Run without sudo to check your own permissions"}}
	}
	var fs []doctor.Finding
	fs = append(fs, pendingGroups(s, h)...)
	fs = append(fs, rootOwnedHome(ctx, s, h)...)
	fs = append(fs, ownHostname(ctx, s)...)
	fs = append(fs, lockedOut(ctx, s, h)...)
	if len(fs) == 0 {
		fs = append(fs, doctor.Finding{Status: doctor.OK, Title: "Your group memberships are active and your home folder belongs to you"})
	}
	return fs
}

// lockedOut catches pam_faillock locking the account after a few wrong
// passwords: sudo and the lock screen then reject the right password too,
// with no message saying why.
func lockedOut(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !h.Systemd || h.User == "" {
		return nil
	}
	es, err := journal(ctx, s, "--since", "-15min", "-p", "0..5", "-g", "account temporarily locked")
	if err != nil {
		return nil
	}
	var last *entry
	for i, e := range es {
		if strings.Contains(e.Message, "for user "+h.User+" account temporarily locked") {
			last = &es[i]
		}
	}
	if last == nil {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Warn,
		Title:  "Your account is locked for a few minutes after wrong passwords",
		Explain: "After several wrong passwords in a row, Linux refuses even the right one for a while, usually 10 minutes, and doesn't say so. " +
			"sudo and the lock screen just answer \"wrong password\". Typing it again restarts the wait.",
		Fixes: []doctor.Fix{
			{Title: "Wait 10 minutes without trying, then type it once, slowly"},
			{Title: "Or restart: the lock is cleared at boot"},
			{Title: "See the failed attempts", Command: "faillock --user " + h.User},
		},
		Evidence: []string{last.Time.Format("15:04:05") + " " + last.Message},
	}}
}

// ownHostname catches a computer that can't look up its own name, usually
// after a rename that missed /etc/hosts. sudo then waits for DNS on every
// run and prints "unable to resolve host". glibc's myhostname and resolve
// modules answer it without /etc/hosts, so they count.
func ownHostname(ctx context.Context, s sys.System) []doctor.Finding {
	name := readTrim(s, "/proc/sys/kernel/hostname")
	if name == "" || name == "localhost" {
		return nil
	}
	for _, l := range strings.Split(readTrim(s, "/etc/hosts"), "\n") {
		l, _, _ = strings.Cut(l, "#")
		f := strings.Fields(l)
		for _, n := range f[min(1, len(f)):] {
			if strings.EqualFold(n, name) || strings.EqualFold(strings.Split(n, ".")[0], name) {
				return nil
			}
		}
	}
	for _, l := range strings.Split(readTrim(s, "/etc/nsswitch.conf"), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(l), "hosts:"); ok {
			for _, m := range strings.Fields(rest) {
				if m == "myhostname" || m == "resolve" {
					return nil
				}
			}
		}
	}
	if s.Lookup(ctx, name) == nil {
		return nil
	}
	return []doctor.Finding{{
		Status: doctor.Warn,
		Title:  "This computer can't find its own name (" + name + "), so sudo is slow",
		Explain: "The name isn't in /etc/hosts, often after renaming the computer. sudo and some apps wait for the network to look it up, " +
			"then print \"unable to resolve host\".",
		Fixes: []doctor.Fix{{
			Title:     "Add the name to /etc/hosts",
			Command:   "echo \"127.0.1.1 " + name + "\" | sudo tee -a /etc/hosts",
			NeedsRoot: true,
		}},
	}}
}

// pendingGroups finds groups the user was added to after logging in. Linux only
// reads group membership at login, so the new group does nothing until then.
func pendingGroups(s sys.System, h *doctor.Host) []doctor.Finding {
	if h.User == "" {
		return nil
	}
	status, err := s.ReadFile("/proc/self/status")
	if err != nil {
		return nil
	}
	active := map[string]bool{}
	for _, line := range strings.Split(string(status), "\n") {
		if rest, ok := strings.CutPrefix(line, "Groups:"); ok {
			for _, g := range strings.Fields(rest) {
				active[g] = true
			}
		}
	}
	groups, err := s.ReadFile("/etc/group")
	if err != nil {
		return nil
	}
	var pending, uses []string
	for _, line := range strings.Split(string(groups), "\n") {
		f := strings.Split(line, ":")
		if len(f) < 4 || active[f[2]] {
			continue
		}
		for _, m := range strings.Split(f[3], ",") {
			if m == h.User {
				pending = append(pending, f[0])
				if u := groupUse[f[0]]; u != "" {
					uses = append(uses, u)
				}
			}
		}
	}
	if len(pending) == 0 {
		return nil
	}
	explain := "You were added after you logged in. Linux only picks up group changes at login, so the new membership does nothing yet."
	if len(uses) > 0 {
		explain += " Until then, " + joinAnd(dedupe(uses)) + " will keep saying \"permission denied\"."
	}
	what := "the " + pending[0] + " group"
	if len(pending) > 1 {
		what = "the groups " + joinAnd(pending)
	}
	return []doctor.Finding{{
		Status:  doctor.Warn,
		Title:   "You were added to " + what + ", but it only works after you log out and back in",
		Explain: explain,
		Fixes: []doctor.Fix{
			{Title: "Log out and log back in (a restart also works)"},
			{Title: "Or try it in this terminal only", Command: "newgrp " + pending[0]},
		},
	}}
}

// rootOwnedHome finds files in the user's home that belong to root, usually
// left by running a graphical app with sudo. They break settings, logins and
// app updates in confusing ways.
func rootOwnedHome(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if h.Home == "" || h.User == "" || !s.Has("find") {
		return nil
	}
	var found []string
	for _, args := range [][]string{
		{h.Home, "-mindepth", "1", "-maxdepth", "1", "-user", "0"},
		{filepath.Join(h.Home, ".config"), filepath.Join(h.Home, ".cache"), filepath.Join(h.Home, ".local", "share"),
			"-mindepth", "1", "-maxdepth", "1", "-user", "0"},
	} {
		// find exits non-zero when one of the folders is missing; what it printed still counts.
		out, _ := s.Run(ctx, "find", args...)
		for _, p := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if p != "" {
				found = append(found, p)
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	shown := found
	if len(shown) > 8 {
		shown = shown[:8]
	}
	verb := "are"
	if len(found) == 1 {
		verb = "is"
	}
	st := doctor.Info
	for _, p := range found {
		if !strings.HasPrefix(p, filepath.Join(h.Home, ".cache")+"/") {
			st = doctor.Warn
		}
	}
	return []doctor.Finding{{
		Status: st,
		Title:  fmt.Sprintf("%s in your home folder %s owned by the administrator", plural(len(found), "item", "items"), verb),
		Explain: "This usually happens after running an app with sudo. Your normal account can't change these files, so settings don't save, apps fail to start, " +
			"and on some desktops the login screen loops back to itself.",
		Evidence: shown,
		Fixes: []doctor.Fix{{
			Title:   "Give them back to your account",
			Command: fmt.Sprintf("sudo chown -R %s: %s", h.User, strings.Join(quoteAll(found), " ")),
		}},
	}}
}

func joinAnd(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

func dedupe(xs []string) []string {
	seen := map[string]bool{}
	out := xs[:0]
	for _, x := range xs {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func quoteAll(ps []string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		if strings.ContainsAny(p, " '\"$`\\") {
			p = "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
		}
		out[i] = p
	}
	return out
}
