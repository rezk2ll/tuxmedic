package watch

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/sys"
)

// Watcher turns a stream of samples into events, remembering when the
// connection went away so the event for its return says how long it was gone.
type Watcher struct {
	Last   Sample
	Events []Event
	lostAt time.Time
}

// Add records a sample and returns the events it caused.
func (w *Watcher) Add(cur Sample) []Event {
	if w.Last.At.IsZero() && !cur.Online {
		w.lostAt = cur.At
	}
	es := Diff(w.Last, cur, w.lostAt)
	for _, e := range es {
		switch e.Kind {
		case Lost:
			w.lostAt = e.At
		case Back:
			w.lostAt = time.Time{}
		}
	}
	w.Events = append(w.Events, es...)
	w.Last = cur
	return es
}

// Run samples every interval until ctx ends or dur passes (0 means until
// Ctrl+C), printing events as they happen and a summary at the end. live
// redraws a one-line status in place, for terminals.
func Run(ctx context.Context, s sys.System, w io.Writer, interval, dur time.Duration, live bool) []Event {
	if dur > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, dur)
		defer cancel()
	}
	fmt.Fprintln(w, "Watching your connection and temperature. Leave this running until the problem happens,")
	fmt.Fprintln(w, "then press Ctrl+C for a summary.")
	fmt.Fprintln(w)

	start := s.Now()
	var wt Watcher
	status := ""
	clear := func() {
		if live && status != "" {
			fmt.Fprint(w, "\r\033[K")
		}
	}
	for {
		cur := Take(ctx, s)
		if ctx.Err() != nil {
			break
		}
		if wt.Last.At.IsZero() && !cur.Online {
			clear()
			fmt.Fprintf(w, "%s  Offline right now\n", cur.At.Format("15:04:05"))
		}
		for _, e := range wt.Add(cur) {
			clear()
			fmt.Fprintf(w, "%s  %s\n", e.At.Format("15:04:05"), e.Text)
			status = ""
		}
		if live {
			clear()
			status = Status(cur)
			fmt.Fprintf(w, "%s  %s", cur.At.Format("15:04:05"), status)
		}
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
		if ctx.Err() != nil {
			break
		}
	}
	clear()
	fmt.Fprintln(w)
	for _, line := range Summary(wt.Events, s.Now().Sub(start)) {
		fmt.Fprintln(w, line)
	}
	return wt.Events
}
