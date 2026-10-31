package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/rezk2ll/tuxmedic/internal/checks"
	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/report"
	"github.com/rezk2ll/tuxmedic/internal/sys"
	"github.com/rezk2ll/tuxmedic/internal/ui"
)

// Set by the release build.
var version = ""

const usage = `tuxmedic finds out what is wrong with your Linux computer and how to fix it.

Usage:
  tuxmedic                  open the interactive screen
  tuxmedic ask QUESTION     check what your question is about, e.g. tuxmedic ask "no sound in zoom"
  tuxmedic check [CHECK…]   print the results (all checks, or the ones named)
  tuxmedic report           print a report to share on a forum, personal details removed
  tuxmedic list             list the checks
  tuxmedic version          print the version

Options:
  --plain      print results instead of opening the interactive screen
  --json       print results as JSON (with check)
  --verbose    include passing checks and the log lines behind each finding
  --ascii      use plain ASCII symbols, for consoles without Unicode
  --no-redact  keep names and addresses in --json output

Exit status: 0 when nothing is broken, 1 when a problem was found, 2 on bad usage.
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tuxmedic", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	plain := fs.Bool("plain", false, "")
	asJSON := fs.Bool("json", false, "")
	verbose := fs.Bool("verbose", false, "")
	ascii := fs.Bool("ascii", false, "")
	noRedact := fs.Bool("no-redact", false, "")
	showVersion := fs.Bool("version", false, "")

	// Accept flags after the subcommand too: "tuxmedic check --json".
	var flags, rest []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			rest = append(rest, a)
		}
	}
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}

	cmd := ""
	if len(rest) > 0 {
		cmd, rest = rest[0], rest[1:]
	}
	if *showVersion {
		cmd = "version"
	}

	all := checks.All()
	switch cmd {
	case "version":
		fmt.Fprintln(stdout, "tuxmedic", versionString())
		return 0
	case "help":
		fmt.Fprint(stdout, usage)
		return 0
	case "list":
		for _, c := range all {
			fmt.Fprintf(stdout, "%-10s %s\n", c.ID, c.Question)
		}
		return 0
	case "", "check", "report", "ask":
	default:
		fmt.Fprintf(stderr, "tuxmedic: unknown command %q\n\n%s", cmd, usage)
		return 2
	}

	var selected []doctor.Check
	if cmd == "ask" {
		q := strings.Join(rest, " ")
		if strings.TrimSpace(q) == "" {
			fmt.Fprint(stderr, "tuxmedic: ask what? For example: tuxmedic ask \"my wifi keeps dropping\"\n")
			return 2
		}
		selected = doctor.Match(all, q)
		if len(selected) == 0 {
			fmt.Fprintln(stderr, "None of the checks are about that, so tuxmedic will run all of them.")
			selected = all
		} else if !*asJSON {
			names := make([]string, len(selected))
			for i, c := range selected {
				names[i] = c.Name
			}
			fmt.Fprintf(stderr, "Checking: %s\n\n", strings.Join(names, ", "))
		}
	} else {
		var err error
		if selected, err = pick(all, rest); err != nil {
			fmt.Fprintf(stderr, "tuxmedic: %v\nRun \"tuxmedic list\" to see the checks.\n", err)
			return 2
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	s := sys.Host{}
	h := doctor.Probe(ctx, s)

	term := os.Getenv("TERM")
	if term == "linux" || os.Getenv("TUXMEDIC_ASCII") != "" {
		*ascii = true
	}
	interactive := (cmd == "" || cmd == "ask") && !*plain && !*asJSON && isTTY(os.Stdin) && isTTY(os.Stdout) && term != "dumb"

	if interactive {
		m := ui.New(s, h, selected, ui.Options{Version: versionString(), ASCII: *ascii})
		if _, err := tea.NewProgram(m, tea.WithContext(ctx)).Run(); err != nil && !errors.Is(err, tea.ErrProgramKilled) {
			fmt.Fprintln(stderr, "tuxmedic:", err)
			return 1
		}
		return 0
	}

	results := runAll(ctx, s, h, selected, stderr, cmd != "report" && !*asJSON && isTTY(os.Stderr))
	switch {
	case cmd == "report":
		report.Markdown(stdout, versionString(), h, results, report.NewRedactor(h))
	case *asJSON:
		var red *report.Redactor
		if !*noRedact {
			red = report.NewRedactor(h)
		}
		if err := report.JSON(stdout, versionString(), h, results, red); err != nil {
			fmt.Fprintln(stderr, "tuxmedic:", err)
			return 1
		}
	default:
		g := report.Unicode
		if *ascii {
			g = report.ASCII
		}
		report.Plain(stdout, h, results, g, *verbose)
	}
	if fail, _, _ := report.Counts(results); fail > 0 {
		return 1
	}
	return 0
}

func pick(all []doctor.Check, ids []string) ([]doctor.Check, error) {
	if len(ids) == 0 {
		return all, nil
	}
	var out []doctor.Check
	for _, id := range ids {
		i := slices.IndexFunc(all, func(c doctor.Check) bool { return strings.EqualFold(c.ID, id) })
		if i < 0 {
			return nil, fmt.Errorf("there is no check called %q", id)
		}
		out = append(out, all[i])
	}
	return out, nil
}

func runAll(ctx context.Context, s sys.System, h *doctor.Host, cs []doctor.Check, progress io.Writer, show bool) []doctor.Report {
	ch := make(chan doctor.Report)
	go doctor.RunAll(ctx, cs, s, h, ch)
	var out []doctor.Report
	for r := range ch {
		out = append(out, r)
		if show {
			fmt.Fprintf(progress, "\rChecking… %d/%d", len(out), len(cs))
		}
	}
	if show {
		fmt.Fprint(progress, "\r\033[K")
	}
	return out
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

func versionString() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return "dev"
}
