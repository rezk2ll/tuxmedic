// Package doctor defines what a check is, what it reports, and how all checks
// run together.
package doctor

import (
	"context"
	"fmt"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/sys"
)

type Status int

// Ordered by severity, so the worst status of a set is its maximum.
const (
	OK Status = iota
	Info
	Skipped
	Warn
	Fail
)

func (s Status) String() string {
	switch s {
	case OK:
		return "ok"
	case Info:
		return "info"
	case Skipped:
		return "skipped"
	case Warn:
		return "warning"
	case Fail:
		return "problem"
	}
	return "unknown"
}

func (s Status) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

func (s *Status) UnmarshalText(b []byte) error {
	for st := OK; st <= Fail; st++ {
		if st.String() == string(b) {
			*s = st
			return nil
		}
	}
	return fmt.Errorf("unknown status %q", b)
}

type Fix struct {
	Title     string `json:"title"`
	Command   string `json:"command,omitempty"`
	NeedsRoot bool   `json:"needs_root,omitempty"`
	// URL points to the distro or upstream docs when a command alone is not enough.
	URL string `json:"url,omitempty"`
}

// Finding is one thing a check noticed, written for someone who is not a Linux
// expert: Title says what is going on, Explain says why it matters.
type Finding struct {
	Status   Status   `json:"status"`
	Title    string   `json:"title"`
	Explain  string   `json:"explain,omitempty"`
	Fixes    []Fix    `json:"fixes,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
}

type Check struct {
	ID       string
	Name     string
	Category string
	// Question is how a user would ask for this check, e.g. "Why is there no sound?".
	Question string
	// Keywords are words people use when describing this problem in their own
	// terms ("zoom", "airpods", "hot"), lowercase and without hyphens.
	Keywords []string
	Run      func(ctx context.Context, s sys.System, h *Host) []Finding
}

type Report struct {
	CheckID  string        `json:"id"`
	Name     string        `json:"name"`
	Category string        `json:"category"`
	Question string        `json:"question"`
	Status   Status        `json:"status"`
	Findings []Finding     `json:"findings"`
	Duration time.Duration `json:"duration_ns"`
}

func (r Report) Headline() string {
	for _, f := range r.Findings {
		if f.Status == r.Status {
			return f.Title
		}
	}
	return "Nothing wrong found"
}

func Worst(fs []Finding) Status {
	w := OK
	for _, f := range fs {
		w = max(w, f.Status)
	}
	return w
}

// RunOne never panics: a broken check reports itself as skipped instead of
// taking the whole program down on a machine nobody tested.
func RunOne(ctx context.Context, c Check, s sys.System, h *Host) (r Report) {
	start := time.Now()
	r = Report{CheckID: c.ID, Name: c.Name, Category: c.Category, Question: c.Question}
	defer func() {
		if p := recover(); p != nil {
			r.Findings = []Finding{{
				Status:   Skipped,
				Title:    "This check crashed on your system",
				Explain:  "That is a bug in tuxmedic, not a problem with your computer. Please report it with the details below.",
				Evidence: []string{fmt.Sprint(p), string(debug.Stack())},
			}}
		}
		r.Status = Worst(r.Findings)
		r.Duration = time.Since(start)
		sortFindings(r.Findings)
	}()
	r.Findings = c.Run(ctx, s, h)
	return r
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Status > fs[j].Status })
}

// RunAll runs every check concurrently and sends each report as it finishes.
func RunAll(ctx context.Context, checks []Check, s sys.System, h *Host, out chan<- Report) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for _, c := range checks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out <- RunOne(ctx, c, s, h)
		}()
	}
	wg.Wait()
	close(out)
}
