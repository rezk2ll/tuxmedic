package checks

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

var Crash = doctor.Check{
	ID:       "crash",
	Name:     "Crashes & freezes",
	Category: "Stability",
	Question: "Why did my computer crash, freeze or restart by itself?",
	Run:      runCrash,
}

const bootsToInspect = 10

var cleanEnd = []string{
	"Journal stopped",
	"Reached target shutdown.target",
	"Reached target reboot.target",
	"Reached target poweroff.target",
	"Reached target halt.target",
	"Reached target kexec.target",
	"reboot: Power down",
	"reboot: Restarting system",
	"Shutting down.",
}

type cause struct {
	pattern *regexp.Regexp
	title   string
	explain string
	fixes   []doctor.Fix
}

// Ordered from most to least specific: the first cause found in the last
// minutes of a boot is reported.
var causes = []cause{
	{
		pattern: regexp.MustCompile(`(?i)kernel panic|Oops:|BUG: unable to handle|general protection fault`),
		title:   "the Linux kernel crashed (kernel panic)",
		explain: "The core of the system hit an error it could not recover from. This is usually a driver bug or faulty hardware (often memory).",
		fixes: []doctor.Fix{
			{Title: "Install pending updates, kernel fixes often solve this"},
			{Title: "Test your memory with memtest86+ from the boot menu if it happens again"},
		},
	},
	{
		pattern: regexp.MustCompile(`(?i)GPU HANG|ring \w+ timeout|gpu reset|NVRM: Xid|amdgpu.*(timeout|fault)|i915.*(hang|reset)|nouveau.*(fault|timeout)`),
		title:   "the graphics driver stopped responding",
		explain: "The graphics card or its driver hung. The screen freezes even though the rest of the system may still be running.",
		fixes: []doctor.Fix{
			{Title: "Install pending updates, graphics drivers get frequent fixes"},
			{Title: "If you use NVIDIA's own driver, make sure its version matches your kernel"},
		},
	},
	{
		pattern: regexp.MustCompile(`(?i)soft lockup|hard LOCKUP|hung_task|blocked for more than \d+ seconds|rcu_sched self-detected stall|watchdog: BUG`),
		title:   "the system locked up",
		explain: "Part of the system stopped responding for so long that the kernel's watchdog noticed. This is often a driver or storage problem.",
		fixes:   []doctor.Fix{{Title: "Check the Disks check for a failing drive, then install pending updates"}},
	},
	{
		pattern: regexp.MustCompile(`(?i)critical temperature|temperature above threshold|thermal shutdown|Thermal Zone .* critical`),
		title:   "the computer overheated",
		explain: "A temperature sensor hit its safety limit, so the hardware slowed down or switched off to protect itself.",
		fixes: []doctor.Fix{
			{Title: "Clean the air vents and keep the laptop on a hard surface"},
			{Title: "Run the Temperature check to see current readings"},
		},
	},
	{
		pattern: regexp.MustCompile(`(?i)Out of memory|oom-kill|invoked oom-killer|Killed process \d+|Under memory pressure|systemd-oomd.*Killed`),
		title:   "the computer ran out of memory",
		explain: "Programs asked for more memory than the computer has. Linux tries to close a program to free memory, but if that comes too late the whole system freezes.",
		fixes: []doctor.Fix{
			{Title: "Close memory-hungry apps (browser tabs, containers, virtual machines)"},
			{Title: "Run the Memory check to see if you have swap or zram"},
		},
	},
	{
		pattern: regexp.MustCompile(`(?i)PM: suspend entry|PM: hibernation entry|Reached target sleep.target`),
		title:   "it went to sleep and never woke up properly",
		explain: "The last thing the system did was start suspending or hibernating. Waking up failed, so the computer had to be forced off.",
		fixes:   []doctor.Fix{{Title: "Run the Sleep check to see how this system suspends"}},
	},
}

var coredumpRE = regexp.MustCompile(`Process \d+ \((.+?)\) of user \S+ dumped core`)
var memoryKillRE = regexp.MustCompile(`(?i)Out of memory: Killed process \d+ \((.+?)\)|oom-kill:.*task=([^,]+)|systemd-oomd.*Killed (\S+)`)

func runCrash(ctx context.Context, s sys.System, h *doctor.Host) []doctor.Finding {
	if !s.Has("journalctl") {
		return []doctor.Finding{pstoreOrSkip(s, h)}
	}
	var fs []doctor.Finding

	boots, err := listBoots(ctx, s)
	if err != nil {
		return []doctor.Finding{{
			Status:   doctor.Skipped,
			Title:    "Couldn't read the system log",
			Explain:  "tuxmedic needs permission to read the system journal to find past crashes.",
			Fixes:    journalAccessFixes(h),
			Evidence: []string{err.Error()},
		}}
	}
	sort.Slice(boots, func(i, j int) bool { return boots[i].Index < boots[j].Index })

	past := make([]boot, 0, len(boots))
	for _, b := range boots {
		if b.Index < 0 {
			past = append(past, b)
		}
	}
	if len(past) == 0 {
		fs = append(fs, volatileJournal(s))
	}
	if len(past) > bootsToInspect {
		past = past[len(past)-bootsToInspect:]
	}

	var events []crashEvent
	for i := len(past) - 1; i >= 0; i-- {
		if e, ok := inspectBoot(ctx, s, past[i]); ok {
			events = append(events, e)
		}
	}
	unclean := groupCrashes(events, updateBlame(ctx, s, h))
	switch {
	case len(unclean) > 0:
		fs = append(fs, unclean...)
	case len(past) > 0:
		fs = append(fs, doctor.Finding{
			Status: doctor.OK,
			Title:  fmt.Sprintf("The last %s ended normally", plural(len(past), "session", "sessions")),
		})
	}

	if f, ok := pstoreFinding(s); ok {
		fs = append(fs, f)
	}
	fs = append(fs, memoryKills(ctx, s)...)
	fs = append(fs, appCrashes(ctx, s)...)
	return fs
}

// crashEvent is one session that ended badly. cause is empty when the log
// shows no reason.
type crashEvent struct {
	f     doctor.Finding
	cause string
	at    string
	end   time.Time
}

// groupCrashes folds sessions that ended for the same reason into one finding,
// so three out-of-memory freezes read as one pattern, not three repeats.
// events are newest first. blame, when it names an update made shortly before
// the first crash of a group, is put in front of that group's explanation.
func groupCrashes(events []crashEvent, blame func(first time.Time) (string, []doctor.Fix)) []doctor.Finding {
	var order []string
	byCause := map[string][]crashEvent{}
	for _, e := range events {
		if _, seen := byCause[e.cause]; !seen {
			order = append(order, e.cause)
		}
		byCause[e.cause] = append(byCause[e.cause], e)
	}
	out := make([]doctor.Finding, 0, len(order))
	for _, cause := range order {
		es := byCause[cause]
		f := es[0].f
		if n := len(es); n > 1 {
			if cause == "" {
				f.Title = fmt.Sprintf("Your computer stopped without shutting down %d times (last %s)", n, es[0].at)
			} else {
				f.Title = fmt.Sprintf("Your computer crashed %d times: %s (last %s)", n, cause, es[0].at)
			}
			var ats []string
			for _, e := range es {
				ats = append(ats, e.at)
			}
			f.Evidence = append([]string{"Sessions that ended this way: " + strings.Join(ats, ", ")}, f.Evidence...)
		}
		if note, fixes := blame(es[len(es)-1].end); note != "" {
			f.Explain = strings.TrimSpace(note + " " + f.Explain)
			f.Fixes = append(fixes, f.Fixes...)
		}
		out = append(out, f)
	}
	return out
}

// updateBlame reads the package history once, on the first crash group that
// asks, and names the update made just before a group's first crash.
func updateBlame(ctx context.Context, s sys.System, h *doctor.Host) func(time.Time) (string, []doctor.Fix) {
	var history []pkgChange
	loaded := false
	return func(first time.Time) (string, []doctor.Fix) {
		if !loaded {
			history, _ = packageHistory(ctx, s, h)
			loaded = true
		}
		a, c, ok := suspectUpdate(history, first)
		if !ok {
			return "", nil
		}
		gap := first.Sub(c.at)
		after := "within an hour of"
		switch {
		case gap >= 48*time.Hour:
			after = plural(int(gap.Hours()/24), "day", "days") + " after"
		case gap >= time.Hour:
			after = plural(int(gap.Hours()), "hour", "hours") + " after"
		}
		note := fmt.Sprintf("This started %s an update to %s %s (%s).",
			after, strings.ToLower(a.label[:1])+a.label[1:], when(c.at, s.Now()), describeChange(c))
		if fx, ok := undoFix(h, a, c); ok {
			return note, []doctor.Fix{fx}
		}
		return note, nil
	}
}

func inspectBoot(ctx context.Context, s sys.System, b boot) (crashEvent, bool) {
	tail, err := journal(ctx, s, "-b", b.ID, "-n", "300")
	if err != nil || len(tail) == 0 {
		return crashEvent{}, false
	}
	last := tail[max(0, len(tail)-40):]
	for _, e := range last {
		for _, marker := range cleanEnd {
			if strings.Contains(e.Message, marker) {
				return crashEvent{}, false
			}
		}
	}

	end := tail[len(tail)-1].Time
	at := when(end, s.Now())
	f := doctor.Finding{
		Status: doctor.Warn,
		Title:  "Your computer stopped without shutting down " + at,
	}
	// Look for a cause in the final minutes only; older errors are usually noise.
	window := end.Add(-10 * 60e9)
	for _, c := range causes {
		for i := len(tail) - 1; i >= 0; i-- {
			e := tail[i]
			if e.Time.Before(window) {
				break
			}
			if c.pattern.MatchString(e.Message) {
				f.Status = doctor.Fail
				f.Title = fmt.Sprintf("Your computer crashed %s: %s", at, c.title)
				f.Explain = c.explain
				f.Fixes = c.fixes
				f.Evidence = evidence(tail, i)
				return crashEvent{f: f, cause: c.title, at: at, end: end}, true
			}
		}
	}
	f.Explain = "Nothing was written to the log before it stopped. That usually means the power was cut, the power button was held down, or the system froze too hard to log anything. Once is nothing to worry about; if it keeps happening, it points to hardware, power or a hard freeze."
	f.Evidence = evidence(tail, len(tail)-1)
	return crashEvent{f: f, at: at, end: end}, true
}

func evidence(es []entry, at int) []string {
	from := max(0, at-4)
	to := min(len(es), at+3)
	out := make([]string, 0, to-from)
	for _, e := range es[from:to] {
		out = append(out, fmt.Sprintf("%s %s: %s", e.Time.Local().Format("Jan 02 15:04:05"), e.Ident, firstLineOf(e.Message)))
	}
	return out
}

func firstLineOf(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 240 {
		s = s[:240] + "…"
	}
	return s
}

func memoryKills(ctx context.Context, s sys.System) []doctor.Finding {
	es, err := journal(ctx, s, "--since", "-7d", "-p", "0..4", "-k")
	if err != nil {
		return nil
	}
	oomd, _ := journal(ctx, s, "--since", "-7d", "SYSLOG_IDENTIFIER=systemd-oomd")
	es = append(es, oomd...)
	killed := map[string]int{}
	var lines []string
	for _, e := range es {
		m := memoryKillRE.FindStringSubmatch(e.Message)
		if m == nil {
			continue
		}
		name := firstNonEmpty(m[1:]...)
		killed[name]++
		lines = append(lines, firstLineOf(e.Message))
	}
	if len(killed) == 0 {
		return nil
	}
	total := 0
	for _, n := range killed {
		total += n
	}
	return []doctor.Finding{{
		Status: doctor.Warn,
		Title:  fmt.Sprintf("Linux closed programs %s this week to free memory: %s", plural(total, "time", "times"), topNames(killed, 3)),
		Explain: "When memory runs out, Linux force-closes the program using the most. You may have seen an app or browser tab vanish. " +
			"If this happens often, the computer needs more memory, more swap, or fewer apps open at once.",
		Fixes:    []doctor.Fix{{Title: "Run the Memory check for swap and zram advice"}},
		Evidence: lastN(lines, 5),
	}}
}

func appCrashes(ctx context.Context, s sys.System) []doctor.Finding {
	es, err := journal(ctx, s, "--since", "-7d", "SYSLOG_IDENTIFIER=systemd-coredump")
	if err != nil {
		return nil
	}
	crashed := map[string]int{}
	var lines []string
	for _, e := range es {
		if m := coredumpRE.FindStringSubmatch(e.Message); m != nil {
			crashed[m[1]]++
			lines = append(lines, firstLineOf(e.Message))
		}
	}
	if len(crashed) == 0 {
		return nil
	}
	total, worst := 0, 0
	for _, n := range crashed {
		total += n
		worst = max(worst, n)
	}
	status := doctor.Info
	if worst >= 3 {
		status = doctor.Warn
	}
	return []doctor.Finding{{
		Status:  status,
		Title:   fmt.Sprintf("Apps crashed %s this week: %s", plural(total, "time", "times"), topNames(crashed, 4)),
		Explain: "These programs crashed on their own. The rest of the system kept running. An app that crashes again and again usually needs an update or a reset of its settings.",
		Fixes: []doctor.Fix{
			{Title: "See the crash details", Command: "coredumpctl list --since=-7d"},
			{Title: "Update the apps that keep crashing"},
		},
		Evidence: lastN(lines, 5),
	}}
}

func pstoreFinding(s sys.System) (doctor.Finding, bool) {
	files, _ := s.Glob("/sys/fs/pstore/*")
	var dumps []string
	for _, f := range files {
		if strings.Contains(f, "dmesg") {
			dumps = append(dumps, f)
		}
	}
	if len(dumps) == 0 {
		return doctor.Finding{}, false
	}
	return doctor.Finding{
		Status:   doctor.Fail,
		Title:    "Your firmware saved a record of a kernel crash",
		Explain:  "When the kernel crashes, it can store its last messages in firmware memory so they survive the restart. One is waiting to be read.",
		Fixes:    []doctor.Fix{{Title: "Read the crash record", Command: "sudo cat " + dumps[0], NeedsRoot: true}},
		Evidence: dumps,
	}, true
}

func pstoreOrSkip(s sys.System, h *doctor.Host) doctor.Finding {
	if f, ok := pstoreFinding(s); ok {
		return f
	}
	return doctor.Finding{
		Status:  doctor.Skipped,
		Title:   "Crash history isn't available on this system",
		Explain: fmt.Sprintf("tuxmedic reads crash history from the systemd journal, and %s doesn't use it. Your system logs are probably in /var/log.", h.Distro),
	}
}

func volatileJournal(s sys.System) doctor.Finding {
	f := doctor.Finding{
		Status:  doctor.Info,
		Title:   "Your system forgets its logs every time it restarts",
		Explain: "Only this session's log exists, so tuxmedic can't see why the computer crashed or restarted before. Keeping logs on disk fixes that and uses at most a few hundred MB.",
	}
	if !s.Exists("/var/log/journal") {
		f.Fixes = []doctor.Fix{{
			Title:     "Keep logs across restarts",
			Command:   "sudo mkdir -p /var/log/journal && sudo systemctl restart systemd-journald",
			NeedsRoot: true,
		}}
	}
	return f
}

func journalAccessFixes(h *doctor.Host) []doctor.Fix {
	group := "systemd-journal"
	if h.Like("debian", "ubuntu") {
		group = "adm"
	}
	return []doctor.Fix{
		{Title: "Run tuxmedic with sudo once", Command: "sudo tuxmedic", NeedsRoot: true},
		{Title: "Or let your user read logs (log out and back in after)", Command: "sudo usermod -aG " + group + " $USER", NeedsRoot: true},
	}
}

func topNames(m map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range m {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return all[i].k < all[j].k
	})
	var parts []string
	for i, e := range all {
		if i == n {
			parts = append(parts, fmt.Sprintf("and %d more", len(all)-n))
			break
		}
		if e.v > 1 {
			parts = append(parts, fmt.Sprintf("%s (%d×)", e.k, e.v))
		} else {
			parts = append(parts, e.k)
		}
	}
	return strings.Join(parts, ", ")
}

func lastN(ss []string, n int) []string {
	if len(ss) > n {
		return ss[len(ss)-n:]
	}
	return ss
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return "unknown"
}
