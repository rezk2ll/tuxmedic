package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
)

type Glyphs struct{ OK, Info, Skipped, Warn, Fail string }

var Unicode = Glyphs{OK: "✓", Info: "i", Skipped: "–", Warn: "!", Fail: "✗"}
var ASCII = Glyphs{OK: "[ok]", Info: "[i]", Skipped: "[-]", Warn: "[!]", Fail: "[x]"}

func (g Glyphs) For(s doctor.Status) string {
	switch s {
	case doctor.OK:
		return g.OK
	case doctor.Info:
		return g.Info
	case doctor.Warn:
		return g.Warn
	case doctor.Fail:
		return g.Fail
	}
	return g.Skipped
}

// Sorted orders reports worst first, then by name, which is the order a user
// should read them in.
func Sorted(rs []doctor.Report) []doctor.Report {
	out := append([]doctor.Report(nil), rs...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Status != out[j].Status {
			return out[i].Status > out[j].Status
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func Counts(rs []doctor.Report) (fail, warn, ok int) {
	for _, r := range rs {
		switch r.Status {
		case doctor.Fail:
			fail++
		case doctor.Warn:
			warn++
		case doctor.OK, doctor.Info:
			ok++
		}
	}
	return
}

func Summary(rs []doctor.Report) string {
	fail, warn, _ := Counts(rs)
	switch {
	case fail > 0 && warn > 0:
		return fmt.Sprintf("%s and %s found", plural(fail, "problem"), plural(warn, "warning"))
	case fail > 0:
		return plural(fail, "problem") + " found"
	case warn > 0:
		return plural(warn, "warning") + ", nothing broken"
	}
	return "Everything looks healthy"
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// Plain writes labelled lines with no color or box drawing, for pipes, screen
// readers, and terminals that can't show the TUI.
func Plain(w io.Writer, h *doctor.Host, rs []doctor.Report, g Glyphs, verbose bool) {
	fmt.Fprintf(w, "tuxmedic: %s\n%s\n\n", Summary(rs), hostLine(h))
	for _, r := range Sorted(rs) {
		detailed := verbose || r.Status >= doctor.Warn
		if !detailed {
			fmt.Fprintf(w, "%s %s: %s\n", g.For(r.Status), r.Name, r.Headline())
			continue
		}
		fmt.Fprintf(w, "%s %s\n", g.For(r.Status), r.Name)
		for _, f := range r.Findings {
			if !verbose && f.Status == doctor.OK {
				continue
			}
			fmt.Fprintf(w, "  %s %s\n", g.For(f.Status), wrap(f.Title, 76, "    "))
			if !verbose && f.Status < doctor.Warn {
				continue
			}
			if f.Explain != "" {
				fmt.Fprintf(w, "    %s\n", wrap(f.Explain, 76, "    "))
			}
			for _, x := range f.Fixes {
				fmt.Fprintf(w, "    fix: %s\n", x.Title)
				if x.Command != "" {
					fmt.Fprintf(w, "         $ %s\n", x.Command)
				}
			}
			if verbose {
				for _, e := range f.Evidence {
					fmt.Fprintf(w, "    | %s\n", e)
				}
			}
		}
		fmt.Fprintln(w)
	}
}

func hostLine(h *doctor.Host) string {
	parts := []string{h.Distro}
	if h.Kernel != "" {
		parts = append(parts, "kernel "+h.Kernel)
	}
	if h.Desktop != "" {
		d := h.Desktop
		if h.Session != "" {
			d += " (" + h.Session + ")"
		}
		parts = append(parts, d)
	}
	for _, p := range []string{h.SoundServer, h.Network, h.PackageMgr} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	if h.Virtual != "" {
		parts = append(parts, "virtual machine: "+h.Virtual)
	}
	return strings.Join(parts, " · ")
}

func HostLine(h *doctor.Host) string { return hostLine(h) }

type jsonOut struct {
	Version string          `json:"version"`
	Time    time.Time       `json:"time"`
	Host    jsonHost        `json:"host"`
	Summary string          `json:"summary"`
	Checks  []doctor.Report `json:"checks"`
}

type jsonHost struct {
	Distro, ID, Kernel, Desktop, Session, SoundServer, Network, PackageMgr, Virtual string
	Laptop                                                                          bool
}

// JSON never includes the hostname or user name, even unredacted.
func JSON(w io.Writer, version string, h *doctor.Host, rs []doctor.Report, red *Redactor) error {
	out := jsonOut{
		Version: version, Time: time.Now().UTC(), Summary: Summary(rs),
		Host: jsonHost{h.Distro, h.ID, h.Kernel, h.Desktop, h.Session, h.SoundServer, h.Network, h.PackageMgr, h.Virtual, h.Laptop},
	}
	for _, r := range Sorted(rs) {
		if red != nil {
			r = red.Report(r)
		}
		out.Checks = append(out.Checks, r)
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// Markdown is the report a user pastes into a forum or bug tracker. It is
// always redacted.
func Markdown(w io.Writer, version string, h *doctor.Host, rs []doctor.Report, red *Redactor) {
	fmt.Fprintf(w, "## tuxmedic report: %s\n\n", Summary(rs))
	fmt.Fprintf(w, "- System: %s\n", red.String(hostLine(h)))
	fmt.Fprintf(w, "- tuxmedic %s, %s\n\n", version, time.Now().UTC().Format("2006-01-02 15:04 UTC"))
	for _, r := range Sorted(rs) {
		r = red.Report(r)
		fmt.Fprintf(w, "### %s %s: %s\n\n", ASCII.For(r.Status), r.Name, r.Headline())
		for _, f := range r.Findings {
			fmt.Fprintf(w, "- %s %s\n", ASCII.For(f.Status), f.Title)
		}
		var ev []string
		for _, f := range r.Findings {
			if f.Status >= doctor.Warn {
				ev = append(ev, f.Evidence...)
			}
		}
		if len(ev) > 0 {
			fmt.Fprintf(w, "\n<details><summary>Log lines</summary>\n\n```\n%s\n```\n</details>\n", strings.Join(ev, "\n"))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "_Names, addresses and serial numbers were removed before sharing._")
}

func wrap(s string, width int, indent string) string {
	var out strings.Builder
	col := 0
	for i, word := range strings.Fields(s) {
		if i > 0 {
			if col+1+len(word) > width {
				out.WriteString("\n" + indent)
				col = 0
			} else {
				out.WriteByte(' ')
				col++
			}
		}
		out.WriteString(word)
		col += len(word)
	}
	return out.String()
}
