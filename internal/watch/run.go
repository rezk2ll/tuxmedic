package watch

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/rezk2ll/tuxmedic/internal/sys"
)

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
	var prev Sample
	var lostAt time.Time
	var events []Event
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
		if prev.At.IsZero() && !cur.Online {
			lostAt = cur.At
			clear()
			fmt.Fprintf(w, "%s  Offline right now\n", cur.At.Format("15:04:05"))
		}
		for _, e := range Diff(prev, cur, lostAt) {
			switch e.Kind {
			case Lost:
				lostAt = e.At
			case Back:
				lostAt = time.Time{}
			}
			events = append(events, e)
			clear()
			fmt.Fprintf(w, "%s  %s\n", e.At.Format("15:04:05"), e.Text)
			status = ""
		}
		if live {
			clear()
			status = Status(cur)
			fmt.Fprintf(w, "%s  %s", cur.At.Format("15:04:05"), status)
		}
		prev = cur
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
	for _, line := range Summary(events, s.Now().Sub(start)) {
		fmt.Fprintln(w, line)
	}
	return events
}
