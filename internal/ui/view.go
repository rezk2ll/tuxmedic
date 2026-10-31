package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/report"
)

const (
	headerHeight = 2
	footerHeight = 1
	minWidth     = 40
	minHeight    = 10
)

func (m *Model) singlePane() bool { return m.width < 84 }

func (m *Model) sidebarWidth() int {
	if m.width >= 120 {
		return 34
	}
	return 28
}

func (m *Model) detailWidth() int {
	if m.singlePane() {
		return m.width
	}
	return m.width - m.sidebarWidth() - 1
}

func (m *Model) bodyHeight() int { return max(1, m.height-headerHeight-footerHeight) }

func (m *Model) layout() {
	m.detail.SetWidth(m.detailWidth())
	m.detail.SetHeight(m.bodyHeight())
	m.refreshDetail()
}

func (m *Model) refreshDetail() {
	if m.width == 0 {
		return
	}
	w := max(20, m.detailWidth()-4)
	var content string
	if m.watcher != nil {
		content, m.actions = m.renderWatch(w), nil
	} else if c := m.current(); c != nil {
		content, m.actions = m.renderCheck(*c, w)
	} else {
		content, m.actions = m.renderOverview(w)
	}
	content = lipgloss.NewStyle().Padding(0, 2).Render(content)
	if content != m.lastContent {
		m.lastContent = content
		y := m.detail.YOffset()
		m.detail.SetContent(content)
		m.detail.SetYOffset(y)
	}
}

func (m *Model) View() tea.View {
	v := tea.NewView("")
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "tuxmedic"
	if m.quitting {
		return v
	}
	if m.width == 0 {
		v.Content = "Starting tuxmedic…"
		return v
	}
	if m.width < minWidth || m.height < minHeight {
		msg := fmt.Sprintf("tuxmedic needs a window of at least %dx%d. This one is %dx%d.\n\nMake it bigger, or run:\ntuxmedic --plain", minWidth, minHeight, m.width, m.height)
		msg = lipgloss.NewStyle().Width(m.width).Align(lipgloss.Center).Render(msg)
		v.Content = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, msg)
		return v
	}

	var body string
	switch {
	case m.singlePane() && m.inDetail:
		body = m.detail.View()
	case m.singlePane():
		body = m.renderSidebar(m.width)
	default:
		side := m.renderSidebar(m.sidebarWidth())
		div := m.theme.subtle.Render(strings.Repeat(m.vline()+"\n", m.bodyHeight()-1) + m.vline())
		body = lipgloss.JoinHorizontal(lipgloss.Top, side, div, m.detail.View())
	}
	body = lipgloss.NewStyle().Height(m.bodyHeight()).MaxHeight(m.bodyHeight()).Render(body)
	screen := lipgloss.JoinVertical(lipgloss.Left, m.renderHeader(), body, m.renderFooter())

	if box := m.overlay(); box != "" {
		box = lipgloss.NewStyle().MaxHeight(m.height).MaxWidth(m.width).Render(box)
		x := max(0, (m.width-lipgloss.Width(box))/2)
		y := max(0, (m.height-lipgloss.Height(box))/3)
		screen = lipgloss.NewCompositor(
			lipgloss.NewLayer(screen),
			lipgloss.NewLayer(box).X(x).Y(y).Z(1),
		).Render()
	}
	v.Content = screen
	return v
}

func (m *Model) vline() string {
	if m.theme.ascii {
		return "|"
	}
	return "│"
}

func (m *Model) hline(n int) string {
	if m.theme.ascii {
		return strings.Repeat("-", n)
	}
	return strings.Repeat("─", n)
}

func (m *Model) reportsList() []doctor.Report {
	rs := make([]doctor.Report, 0, len(m.reports))
	for _, c := range m.checks {
		if r, ok := m.reports[c.ID]; ok {
			rs = append(rs, r)
		}
	}
	return rs
}

func (m *Model) renderHeader() string {
	t := m.theme
	logo := " tuxmedic "
	left := lipgloss.NewStyle().Bold(true).Reverse(true).Foreground(t.accent).Render(logo)

	var right string
	if m.busy() {
		done := len(m.checks) - len(m.running)
		right = t.code.Render(t.spinner[m.frame%len(t.spinner)]) + fmt.Sprintf(" Checking %d/%d", done, len(m.checks))
	} else {
		fail, warn, ok := report.Counts(m.reportsList())
		right = lipgloss.NewStyle().Foreground(t.fail).Bold(true).Render(fmt.Sprintf("%s %d", t.glyphs.Fail, fail)) + "  " +
			lipgloss.NewStyle().Foreground(t.warn).Bold(true).Render(fmt.Sprintf("%s %d", t.glyphs.Warn, warn)) + "  " +
			lipgloss.NewStyle().Foreground(t.ok).Bold(true).Render(fmt.Sprintf("%s %d", t.glyphs.OK, ok))
	}
	right += " "
	room := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	host := ""
	if room > 10 {
		host = " " + truncate(report.HostLine(m.host), room)
	}
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(host)-lipgloss.Width(right))
	line := left + t.subtle.Render(host) + strings.Repeat(" ", gap) + right
	return line + "\n" + t.subtle.Render(m.hline(m.width))
}

// renderSidebar also records which entry each visible row shows, so mouse
// clicks land on exactly what was drawn, scrolling and filter line included.
func (m *Model) renderSidebar(width int) string {
	t := m.theme
	var lines []string
	var rows []int
	add := func(line string, entry int) {
		lines = append(lines, line)
		rows = append(rows, entry)
	}
	lastCat := ""
	listFocused := !m.detailFocused()
	selRow := 0
	for i, e := range m.entries {
		// The selected row is styled as a whole from plain text: wrapping
		// already-colored text in reverse or underline garbles it.
		var label, plain string
		if e.overview {
			plain = "  Overview"
			label = "  " + t.bold.Render("Overview")
			if m.busy() {
				frame := t.spinner[m.frame%len(t.spinner)]
				plain += " " + frame
				label += " " + t.subtle.Render(frame)
			}
		} else {
			c := m.checks[e.check]
			if c.Category != lastCat {
				add(t.subtle.Render(" "+strings.ToUpper(c.Category)), -1)
				lastCat = c.Category
			}
			glyph, styled := m.statusGlyph(c.ID)
			plain = " " + glyph + " " + c.Name
			label = " " + styled + " " + c.Name
		}
		switch {
		case i == m.sel && listFocused:
			label = t.selected.Render(padRight(truncate(plain, width-1), width-1))
		case i == m.sel:
			label = t.selectedDim.Render(truncate(plain, width-1))
		default:
			label = truncate(label, width-1)
		}
		if i == m.sel {
			selRow = len(lines)
		}
		add(label, i)
		if e.overview {
			add("", -1)
		}
	}
	if len(m.entries) == 0 {
		add(t.subtle.Render("  No check matches."), -1)
		add(t.subtle.Render("  esc clears the filter"), -1)
	}

	room := m.bodyHeight()
	var head []string
	if m.filtering || m.filter.Value() != "" {
		m.filter.SetWidth(width - 4)
		head = append(head, " "+m.filter.View())
		room--
	}
	// Keep the selection in view, with its category heading when possible.
	if selRow-1 < m.sideTop {
		m.sideTop = max(0, selRow-1)
	}
	if selRow >= m.sideTop+room {
		m.sideTop = selRow - room + 1
	}
	m.sideTop = max(0, min(m.sideTop, len(lines)-room))
	end := min(len(lines), m.sideTop+room)

	m.sideRows = make([]int, len(head), len(head)+end-m.sideTop)
	for i := range m.sideRows {
		m.sideRows[i] = -1
	}
	m.sideRows = append(m.sideRows, rows[m.sideTop:end]...)
	head = append(head, lines[m.sideTop:end]...)
	return lipgloss.NewStyle().Width(width).Render(strings.Join(head, "\n"))
}

// statusGlyph returns the glyph as plain text and styled.
func (m *Model) statusGlyph(id string) (string, string) {
	t := m.theme
	if m.running[id] {
		g := t.spinner[m.frame%len(t.spinner)]
		return g, t.code.Render(g)
	}
	r, ok := m.reports[id]
	if !ok {
		return ".", t.subtle.Render(".")
	}
	g := t.glyphs.For(r.Status)
	styled := t.glyph(r.Status)
	if t.ascii {
		g, styled = padRight(g, 4), padRight(styled, 4)
	}
	return g, styled
}

func (m *Model) renderCheck(c doctor.Check, width int) (string, []action) {
	t := m.theme
	var b strings.Builder
	r, done := m.reports[c.ID]
	head := t.title.Render(c.Name)
	if done && !m.running[c.ID] {
		head += "  " + t.badge(r.Status)
	}
	b.WriteString("\n" + head + "\n")
	b.WriteString(t.subtle.Render(wrap(c.Question, width)) + "\n\n")

	if m.running[c.ID] || !done {
		b.WriteString(t.code.Render(t.spinner[m.frame%len(t.spinner)]) + " Checking…\n")
		return b.String(), nil
	}

	var acts []action
	for _, f := range r.Findings {
		b.WriteString(t.glyph(f.Status) + " " + wrapIndent(lipgloss.NewStyle().Bold(true).Foreground(t.color(f.Status)).Render(f.Title), width-2, "  ") + "\n")
		if f.Explain != "" {
			b.WriteString("  " + wrapIndent(f.Explain, width-2, "  ") + "\n")
		}
		if len(f.Fixes) > 0 {
			b.WriteString("\n  " + t.bold.Render("What you can do") + "\n")
			for i := range f.Fixes {
				fx := &f.Fixes[i]
				acts = append(acts, action{fix: fx, checkID: c.ID})
				n := len(acts)
				num := "   "
				if n <= 9 {
					num = t.key.Render(fmt.Sprintf("[%d]", n))
				}
				b.WriteString("  " + num + " " + wrapIndent(fx.Title, width-8, "      ") + "\n")
				if fx.Command != "" {
					cmd := t.code.Render("$ " + fx.Command)
					if fx.NeedsRoot {
						cmd += t.subtle.Render("   (asks for your password)")
					}
					b.WriteString("      " + wrapIndent(cmd, width-8, "        ") + "\n")
				}
				if fx.URL != "" {
					b.WriteString("      " + t.subtle.Render(truncate(fx.URL, width-6)) + "\n")
				}
			}
		}
		if len(f.Evidence) > 0 {
			if m.evidence[c.ID] {
				b.WriteString("\n  " + t.subtle.Render("From the logs:") + "\n")
				for _, e := range f.Evidence {
					b.WriteString(t.subtle.Render("  "+m.vline()+" "+truncate(e, width-4)) + "\n")
				}
			} else {
				b.WriteString("  " + t.subtle.Render(fmt.Sprintf("%s %s from the logs (press e)", m.arrow(), plural(len(f.Evidence), "line"))) + "\n")
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(t.subtle.Render(fmt.Sprintf("Checked in %s. Press r to check again.", fmtDuration(r.Duration))) + "\n")
	return b.String(), acts
}

func (m *Model) arrow() string {
	if m.theme.ascii {
		return ">"
	}
	return "▸"
}

func (m *Model) renderOverview(width int) (string, []action) {
	t := m.theme
	var b strings.Builder
	rs := m.reportsList()
	b.WriteString("\n")
	if m.busy() {
		done := len(m.checks) - len(m.running)
		b.WriteString(t.title.Render("Checking your computer…") + "\n")
		b.WriteString(t.subtle.Render(fmt.Sprintf("%d of %d checks done. Results appear as they finish.", done, len(m.checks))) + "\n\n")
		b.WriteString(progressBar(done, len(m.checks), min(40, width), t) + "\n\n")
	} else {
		fail, warn, _ := report.Counts(rs)
		st := doctor.OK
		switch {
		case fail > 0:
			st = doctor.Fail
		case warn > 0:
			st = doctor.Warn
		}
		b.WriteString(t.glyph(st) + " " + lipgloss.NewStyle().Bold(true).Foreground(t.color(st)).Render(report.Summary(rs)) + "\n")
		b.WriteString(t.subtle.Render(wrap(fmt.Sprintf("Checked %d areas of %s in %s.", len(rs), m.host.Distro, fmtDuration(m.elapsed)), width)) + "\n")
		if m.since != "" {
			b.WriteString(t.bold.Render(wrap(m.since, width)) + "\n")
		}
		b.WriteString("\n")
	}

	var acts []action
	section := func(title string, keep func(doctor.Status) bool) {
		var rows []string
		for i, e := range m.entries {
			if e.overview {
				continue
			}
			r, ok := m.reports[m.checks[e.check].ID]
			if !ok || !keep(r.Status) {
				continue
			}
			acts = append(acts, action{jump: i})
			num := "   "
			if len(acts) <= 9 {
				num = t.key.Render(fmt.Sprintf("[%d]", len(acts)))
			}
			rows = append(rows, fmt.Sprintf("%s %s %s\n      %s", num, t.glyph(r.Status), t.bold.Render(r.Name), wrapIndent(r.Headline(), width-6, "      ")))
		}
		if len(rows) > 0 {
			b.WriteString(t.header.Render(title) + "\n")
			b.WriteString(strings.Join(rows, "\n") + "\n\n")
		}
	}
	section("Needs your attention", func(s doctor.Status) bool { return s >= doctor.Warn })
	section("Good to know", func(s doctor.Status) bool { return s == doctor.Info })

	var good []string
	for _, r := range rs {
		if r.Status == doctor.OK {
			good = append(good, r.Name)
		}
	}
	if len(good) > 0 {
		b.WriteString(t.header.Render("All good") + "\n")
		b.WriteString(t.glyph(doctor.OK) + " " + wrapIndent(strings.Join(good, ", "), width-2, "  ") + "\n\n")
	}
	if !m.busy() {
		b.WriteString(t.subtle.Render(wrap("Press a number to open a check, or use the arrows. Press x to save a report you can share on a forum: names and addresses are removed.", width)) + "\n")
	}
	return b.String(), acts
}

func progressBar(done, total, width int, t theme) string {
	if total == 0 {
		return ""
	}
	full, empty := "█", "░"
	if t.ascii {
		full, empty = "#", "."
	}
	n := done * width / total
	return t.code.Render(strings.Repeat(full, n)) + t.subtle.Render(strings.Repeat(empty, width-n))
}

type hint struct{ key, label string }

func (m *Model) renderFooter() string {
	t := m.theme
	if m.toast != "" {
		return lipgloss.NewStyle().Bold(true).Foreground(t.accent).Render(" " + truncate(m.toast, m.width-2))
	}
	var hs []hint
	switch {
	case m.filtering:
		hs = []hint{{"enter", "open"}, {"esc", "cancel"}}
	case m.detailFocused():
		hs = []hint{{"↑↓", "scroll"}, {"1-9", "fix"}, {"e", "logs"}, {"r", "re-check"}, {"esc", "back"}, {"ctrl+p", "commands"}, {"?", "help"}, {"q", "quit"}}
	default:
		num := "fix"
		if m.current() == nil {
			num = "open"
		}
		hs = []hint{{"↑↓", "move"}, {"enter", "open"}, {"1-9", num}, {"/", "ask"}, {"w", "watch"}, {"x", "share report"}, {"ctrl+p", "commands"}, {"?", "help"}, {"q", "quit"}}
	}
	if m.watcher != nil {
		stop := "stop watching"
		if !m.watching {
			stop = "watch again"
		}
		hs = []hint{{"w", stop}, {"↑↓", "scroll"}, {"esc", "close"}, {"q", "quit"}}
	}
	var parts []string
	used := 1
	for _, h := range hs {
		k := h.key
		if t.ascii {
			k = strings.ReplaceAll(k, "↑↓", "up/down")
		}
		p := t.key.Render(k) + " " + t.subtle.Render(h.label)
		if used+lipgloss.Width(p)+2 > m.width {
			break
		}
		used += lipgloss.Width(p) + 2
		parts = append(parts, p)
	}
	return " " + strings.Join(parts, "  ")
}

func (m *Model) overlay() string {
	t := m.theme
	box := lipgloss.NewStyle().Border(t.border).BorderForeground(t.accent).Padding(1, 2)
	if m.height < 24 || m.width < 60 {
		box = box.Padding(0, 1)
	}
	w := min(m.width-4, 70)
	switch {
	case m.confirm != nil:
		f := m.confirm.fix
		var b strings.Builder
		b.WriteString(t.title.Render(wrap(f.Title, w-6)) + "\n\n")
		if f.Command != "" {
			b.WriteString(t.code.Render(wrap("$ "+f.Command, w-6)) + "\n\n")
			if f.NeedsRoot {
				b.WriteString(t.subtle.Render(wrap("This needs administrator rights, so you'll be asked for your password. tuxmedic never stores it.", w-6)) + "\n\n")
			}
			b.WriteString(t.key.Render("enter") + " run it   " + t.key.Render("c") + " copy   " + t.key.Render("esc") + " cancel")
		} else {
			b.WriteString(t.subtle.Render(f.URL) + "\n\n")
			b.WriteString(t.key.Render("o") + " open in browser   " + t.key.Render("c") + " copy link   " + t.key.Render("esc") + " cancel")
		}
		return box.Width(w).Render(b.String())

	case m.palette:
		items := m.paletteItems()
		var b strings.Builder
		m.palInput.SetWidth(w - 8)
		b.WriteString(m.palInput.View() + "\n\n")
		maxRows := max(3, min(12, m.height-10))
		start := max(0, m.palSel-maxRows+1)
		for i := start; i < len(items) && i < start+maxRows; i++ {
			line := padRight(truncate(" "+items[i].label, w-14), w-14)
			if items[i].hint != "" {
				line += " " + t.subtle.Render(items[i].hint)
			}
			if i == m.palSel {
				line = t.selected.Render(line)
			}
			b.WriteString(line + "\n")
		}
		if len(items) == 0 {
			b.WriteString(t.subtle.Render(" Nothing matches.") + "\n")
		}
		return box.Width(w).Render(strings.TrimRight(b.String(), "\n"))

	case m.help:
		rows := [][2]string{
			{"↑ ↓  or  j k", "move, or scroll the details"},
			{"enter  →", "open a check"},
			{"esc  ←", "go back"},
			{"tab", "switch between the list and the details"},
			{"1 to 9", "run or copy a fix; on the overview, open a check"},
			{"e", "show the log lines behind a finding"},
			{"r  /  R", "check this again  /  run every check again"},
			{"/", "describe the problem: try \"wifi keeps dropping\""},
			{"x", "save a report to share (personal details removed)"},
			{"w", "watch for drops and overheating as they happen"},
			{"ctrl+p  or  :", "every command, searchable"},
			{"mouse", "click a check, scroll with the wheel"},
			{"q", "quit"},
		}
		var b strings.Builder
		b.WriteString(t.title.Render("Keyboard shortcuts") + "\n\n")
		for _, r := range rows {
			b.WriteString(padRight(t.key.Render(r[0]), 18) + " " + r[1] + "\n")
		}
		b.WriteString("\n" + t.subtle.Render("tuxmedic only reads your system. Nothing changes unless you run a fix."))
		return box.Width(w).Render(b.String())
	}
	return ""
}

func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func padRight(s string, w int) string {
	if n := lipgloss.Width(s); n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

func wrap(s string, width int) string { return wrapIndent(s, width, "") }

func wrapIndent(s string, width int, indent string) string {
	if width < 10 {
		width = 10
	}
	return strings.ReplaceAll(lipgloss.NewStyle().Width(width).Render(s), "\n", "\n"+indent)
}

func plural(n int, w string) string {
	if n == 1 {
		return "1 " + w
	}
	return fmt.Sprintf("%d %ss", n, w)
}
