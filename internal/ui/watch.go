package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/watch"
)

const watchEvery = 3 * time.Second

// sampleMsg carries one reading. gen ties it to the watch session that asked
// for it, so readings still in flight after a stop are dropped.
type sampleMsg struct {
	s   watch.Sample
	gen int
}

func (m *Model) toggleWatch() tea.Cmd {
	if m.watching {
		m.watching = false
		m.watchEnd = time.Now()
		m.refreshDetail()
		return m.notify("Stopped watching. The summary is in the details")
	}
	m.watching = true
	m.watcher = &watch.Watcher{}
	m.watchStart, m.watchEnd = time.Now(), time.Time{}
	m.watchGen++
	m.inDetail, m.focus = true, focusDetail
	m.detail.GotoTop()
	m.refreshDetail()
	return m.sample(0)
}

// closeWatch hides a stopped watch, or stops a running one first.
func (m *Model) closeWatch() tea.Cmd {
	if m.watching {
		return m.toggleWatch()
	}
	m.watcher = nil
	m.refreshDetail()
	return nil
}

func (m *Model) sample(after time.Duration) tea.Cmd {
	ctx, s, gen := m.ctx, m.sys, m.watchGen
	take := func() tea.Msg { return sampleMsg{watch.Take(ctx, s), gen} }
	if after == 0 {
		return take
	}
	return tea.Tick(after, func(time.Time) tea.Msg { return take() })
}

func (m *Model) onSample(msg sampleMsg) tea.Cmd {
	if !m.watching || msg.gen != m.watchGen || m.ctx.Err() != nil {
		return nil
	}
	m.watcher.Add(msg.s)
	m.refreshDetail()
	return m.sample(watchEvery)
}

func (m *Model) renderWatch(width int) string {
	t := m.theme
	w := m.watcher
	var b strings.Builder
	b.WriteString("\n")
	end := m.watchEnd
	if m.watching {
		b.WriteString(t.title.Render(t.spinner[m.frame%len(t.spinner)]+" Watching your connection and temperature") + "\n")
		b.WriteString(t.subtle.Render(wrap("Leave this open until the problem happens again. Press w to stop.", width)) + "\n\n")
		end = time.Now()
		if w.Last.At.IsZero() {
			b.WriteString(t.subtle.Render("Taking the first reading…") + "\n\n")
		} else {
			b.WriteString(t.bold.Render("Now: ") + watch.Status(w.Last) + "\n\n")
		}
	} else {
		b.WriteString(t.title.Render("Watch stopped") + "\n")
		b.WriteString(t.subtle.Render(wrap("Press w to watch again, or esc to close.", width)) + "\n\n")
	}

	b.WriteString(t.header.Render("What happened") + "\n")
	if len(w.Events) == 0 {
		b.WriteString(t.subtle.Render("Nothing yet.") + "\n")
	}
	for _, e := range w.Events {
		st := doctor.Warn
		switch e.Kind {
		case watch.Back, watch.WifiOn, watch.DNSBack:
			st = doctor.OK
		case watch.Lost, watch.WifiOff:
			st = doctor.Fail
		}
		line := e.At.Format("15:04:05") + "  " + e.Text
		b.WriteString(t.glyph(st) + " " + wrapIndent(line, width-2, "  ") + "\n")
	}

	b.WriteString("\n" + t.header.Render("Summary") + "\n")
	for _, line := range watch.Summary(w.Events, end.Sub(m.watchStart).Round(time.Second)) {
		b.WriteString(wrap(line, width) + "\n")
	}
	return b.String()
}
