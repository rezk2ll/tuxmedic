// Package ui is the interactive terminal interface.
package ui

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/report"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

type Options struct {
	Version string
	ASCII   bool
	// Only, when set, limits the session to these check IDs.
	Only []string
}

var categoryOrder = []string{"Stability", "Network", "Devices", "Power", "Storage", "Performance", "System"}

type focus int

const (
	focusList focus = iota
	focusDetail
)

type (
	reportMsg struct{ r doctor.Report }
	tickMsg   struct{}
	toastMsg  struct{ id int }
	fixDone   struct {
		checkID string
		err     error
	}
)

type entry struct {
	overview bool
	check    int
}

type paletteItem struct {
	label, hint string
	run         func(m *Model) tea.Cmd
}

type Model struct {
	ctx    context.Context
	cancel context.CancelFunc
	sys    sys.System
	host   *doctor.Host
	opts   Options

	checks  []doctor.Check
	reports map[string]doctor.Report
	running map[string]bool
	// verifying holds a check's report from before a fix ran, until the
	// re-check after it says whether the fix worked.
	verifying map[string]doctor.Report
	started   time.Time
	elapsed   time.Duration

	width, height int
	dark          bool
	theme         theme

	entries  []entry
	sel      int
	sideTop  int
	sideRows []int
	focus    focus
	inDetail bool // single-pane layouts: the detail replaces the list

	detail      viewport.Model
	evidence    map[string]bool
	actions     []action
	frame       int
	ticking     bool
	toast       string
	toastID     int
	help        bool
	confirm     *action
	filtering   bool
	filter      textinput.Model
	palette     bool
	palInput    textinput.Model
	palSel      int
	quitting    bool
	lastContent string
}

// action is something the user can trigger with a number key in the detail
// pane: a fix to run or copy, or a check to jump to from the overview.
type action struct {
	fix     *doctor.Fix
	checkID string
	jump    int
}

func New(s sys.System, h *doctor.Host, checks []doctor.Check, o Options) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Model{
		ctx: ctx, cancel: cancel, sys: s, host: h, opts: o,
		checks: ordered(checks), reports: map[string]doctor.Report{}, running: map[string]bool{},
		verifying: map[string]doctor.Report{},
		evidence:  map[string]bool{}, dark: true,
		detail: viewport.New(),
	}
	m.theme = newTheme(true, o.ASCII)
	m.filter = textinput.New()
	m.filter.Prompt = "/ "
	m.filter.Placeholder = "describe the problem, e.g. zoom can't hear me"
	m.palInput = textinput.New()
	m.palInput.Prompt = "> "
	m.palInput.Placeholder = "type a command or a check name"
	m.rebuildEntries()
	return m
}

func ordered(cs []doctor.Check) []doctor.Check {
	rank := map[string]int{}
	for i, c := range categoryOrder {
		rank[c] = i
	}
	out := make([]doctor.Check, 0, len(cs))
	for _, cat := range categoryOrder {
		for _, c := range cs {
			if c.Category == cat {
				out = append(out, c)
			}
		}
	}
	for _, c := range cs {
		if _, ok := rank[c.Category]; !ok {
			out = append(out, c)
		}
	}
	return out
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, m.runAll())
}

func (m *Model) runAll() tea.Cmd {
	m.started = time.Now()
	cmds := make([]tea.Cmd, 0, len(m.checks)+1)
	for _, c := range m.checks {
		cmds = append(cmds, m.runCheck(c))
	}
	return tea.Batch(append(cmds, m.startTicking())...)
}

func (m *Model) runCheck(c doctor.Check) tea.Cmd {
	m.running[c.ID] = true
	ctx, s, h := m.ctx, m.sys, m.host
	return func() tea.Msg { return reportMsg{doctor.RunOne(ctx, c, s, h)} }
}

func (m *Model) startTicking() tea.Cmd {
	if m.ticking {
		return nil
	}
	m.ticking = true
	return tick()
}

func tick() tea.Cmd {
	return tea.Tick(90*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *Model) busy() bool { return len(m.running) > 0 }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil

	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.theme = newTheme(m.dark, m.opts.ASCII)
		m.refreshDetail()
		return m, nil

	case reportMsg:
		m.reports[msg.r.CheckID] = msg.r
		delete(m.running, msg.r.CheckID)
		if !m.busy() && !m.started.IsZero() {
			m.elapsed = time.Since(m.started)
		}
		m.refreshDetail()
		if before, ok := m.verifying[msg.r.CheckID]; ok {
			delete(m.verifying, msg.r.CheckID)
			return m, m.notify(verdict(before, msg.r))
		}
		return m, nil

	case tickMsg:
		m.frame++
		if !m.busy() {
			m.ticking = false
			return m, nil
		}
		return m, tick()

	case toastMsg:
		if msg.id == m.toastID {
			m.toast = ""
		}
		return m, nil

	case fixDone:
		c := m.checkByID(msg.checkID)
		var cmd tea.Cmd
		if c != nil {
			cmd = tea.Batch(m.runCheck(*c), m.startTicking())
		}
		if msg.err != nil {
			return m, tea.Batch(cmd, m.notify("The command failed: "+msg.err.Error()))
		}
		if c != nil {
			m.verifying[c.ID] = m.reports[c.ID]
		}
		return m, tea.Batch(cmd, m.notify("Done. Checking whether it worked…"))

	case tea.MouseWheelMsg:
		m.wheel(msg.Mouse())
		return m, nil

	case tea.MouseClickMsg:
		m.click(msg.Mouse())
		return m, nil

	case tea.KeyPressMsg:
		return m, m.key(msg)
	}

	if m.filtering {
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		return m, cmd
	}
	if m.palette {
		var cmd tea.Cmd
		m.palInput, cmd = m.palInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) key(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()
	if k == "ctrl+c" {
		return m.quit()
	}

	switch {
	case m.confirm != nil:
		return m.confirmKey(k)
	case m.palette:
		return m.paletteKey(msg)
	case m.filtering:
		return m.filterKey(msg)
	case m.help:
		if k == "?" || k == "esc" || k == "q" || k == "enter" {
			m.help = false
		}
		return nil
	}

	switch k {
	case "q":
		return m.quit()
	case "?", "f1":
		m.help = true
	case "ctrl+p", ":":
		return m.openPalette()
	case "/":
		m.filtering = true
		m.focus = focusList
		m.inDetail = false
		return m.filter.Focus()
	case "tab", "shift+tab":
		if m.singlePane() {
			m.inDetail = !m.inDetail
		} else if m.focus == focusList {
			m.focus = focusDetail
		} else {
			m.focus = focusList
		}
	case "enter", "right", "l":
		if m.singlePane() {
			m.inDetail = true
		} else {
			m.focus = focusDetail
		}
	case "esc", "left", "h", "backspace":
		switch {
		case m.singlePane() && m.inDetail:
			m.inDetail = false
		case m.focus == focusDetail:
			m.focus = focusList
		case m.filter.Value() != "":
			m.filter.SetValue("")
			m.rebuildEntries()
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "home", "g":
		if m.detailFocused() {
			m.detail.GotoTop()
		} else {
			m.selectIndex(0)
		}
	case "end", "G":
		if m.detailFocused() {
			m.detail.GotoBottom()
		} else {
			m.selectIndex(len(m.entries) - 1)
		}
	case "pgdown", "space", "ctrl+d", "f":
		m.detail.HalfPageDown()
	case "pgup", "ctrl+u", "b":
		m.detail.HalfPageUp()
	case "e":
		if c := m.current(); c != nil {
			m.evidence[c.ID] = !m.evidence[c.ID]
			m.refreshDetail()
		}
	case "r":
		if c := m.current(); c != nil {
			return tea.Batch(m.runCheck(*c), m.startTicking(), m.notify("Checking "+c.Name+" again…"))
		}
		return tea.Batch(m.runAll(), m.notify("Running every check again…"))
	case "R":
		return tea.Batch(m.runAll(), m.notify("Running every check again…"))
	case "x":
		return m.export()
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		return m.trigger(int(k[0] - '1'))
	}
	return nil
}

func (m *Model) quit() tea.Cmd {
	m.quitting = true
	m.cancel()
	return tea.Quit
}

func (m *Model) detailFocused() bool {
	if m.singlePane() {
		return m.inDetail
	}
	return m.focus == focusDetail
}

func (m *Model) move(d int) {
	if m.detailFocused() {
		if d < 0 {
			m.detail.ScrollUp(1)
		} else {
			m.detail.ScrollDown(1)
		}
		return
	}
	m.selectIndex(m.sel + d)
}

func (m *Model) selectIndex(i int) {
	if len(m.entries) == 0 {
		return
	}
	i = max(0, min(i, len(m.entries)-1))
	if i != m.sel {
		m.sel = i
		m.detail.GotoTop()
	}
	m.refreshDetail()
}

func (m *Model) current() *doctor.Check {
	if m.sel < 0 || m.sel >= len(m.entries) || m.entries[m.sel].overview {
		return nil
	}
	return &m.checks[m.entries[m.sel].check]
}

func problems(r doctor.Report) int {
	n := 0
	for _, f := range r.Findings {
		if f.Status >= doctor.Warn {
			n++
		}
	}
	return n
}

// verdict tells the user whether the fix they just ran worked, judged by
// re-running the check: the one question they have after running it.
func verdict(before, after doctor.Report) string {
	b, a := problems(before), problems(after)
	switch {
	case b > 0 && a == 0:
		return "Fixed: " + after.Name + " looks good now"
	case a < b:
		return fmt.Sprintf("Better: %d of %d problems fixed", b-a, b)
	case a > b:
		return "Something new came up, see the details"
	}
	return "No change yet. Some fixes need a restart; otherwise try the next fix"
}

func (m *Model) checkByID(id string) *doctor.Check {
	for i := range m.checks {
		if m.checks[i].ID == id {
			return &m.checks[i]
		}
	}
	return nil
}

func (m *Model) rebuildEntries() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.entries = m.entries[:0]
	if q == "" {
		m.entries = append(m.entries, entry{overview: true})
	}
	for i, c := range m.checks {
		if q == "" || doctor.Score(c, q) > 0 {
			m.entries = append(m.entries, entry{check: i})
		}
	}
	m.sel = min(m.sel, max(0, len(m.entries)-1))
	m.refreshDetail()
}

// fuzzy matches when every word of the query appears in hay, so "wifi drop"
// finds "Why does my Wi-Fi keep dropping?".
func fuzzy(hay, q string) bool {
	hay = strings.ReplaceAll(hay, "-", "")
	for _, w := range strings.Fields(strings.ReplaceAll(q, "-", "")) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

func (m *Model) filterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.rebuildEntries()
		return nil
	case "enter", "down", "up":
		m.filtering = false
		m.filter.Blur()
		if msg.String() == "enter" {
			if m.singlePane() {
				m.inDetail = true
			} else {
				m.focus = focusDetail
			}
		}
		return nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	m.sel = 0
	m.rebuildEntries()
	return cmd
}

func (m *Model) notify(s string) tea.Cmd {
	m.toastID++
	id := m.toastID
	m.toast = s
	return tea.Tick(3*time.Second, func(time.Time) tea.Msg { return toastMsg{id} })
}

func (m *Model) trigger(i int) tea.Cmd {
	if i < 0 || i >= len(m.actions) {
		return nil
	}
	a := m.actions[i]
	if a.fix == nil {
		m.selectIndex(a.jump)
		m.focus = focusDetail
		if m.singlePane() {
			m.inDetail = true
		}
		return nil
	}
	if a.fix.Command == "" && a.fix.URL == "" {
		return m.notify("This one is a step to do yourself: " + a.fix.Title)
	}
	m.confirm = &a
	return nil
}

func (m *Model) confirmKey(k string) tea.Cmd {
	a := m.confirm
	switch k {
	case "esc", "n", "q":
		m.confirm = nil
	case "c", "y":
		m.confirm = nil
		text := a.fix.Command
		if text == "" {
			text = a.fix.URL
		}
		return tea.Batch(tea.SetClipboard(text), m.notify("Copied to the clipboard"))
	case "o":
		if a.fix.URL != "" {
			m.confirm = nil
			_ = exec.Command("xdg-open", a.fix.URL).Start()
			return m.notify("Opening in your browser…")
		}
	case "enter":
		if a.fix.Command == "" {
			return nil
		}
		m.confirm = nil
		script := a.fix.Command + "\nstatus=$?\nprintf '\\n\\033[1mtuxmedic:\\033[0m press Enter to go back… '\nread _\nexit $status"
		c := exec.Command("sh", "-c", script)
		id := a.checkID
		return tea.ExecProcess(c, func(err error) tea.Msg { return fixDone{checkID: id, err: err} })
	}
	return nil
}

func (m *Model) export() tea.Cmd {
	var rs []doctor.Report
	for _, r := range m.reports {
		rs = append(rs, r)
	}
	if len(rs) == 0 {
		return m.notify("Wait for the checks to finish first")
	}
	var buf bytes.Buffer
	report.Markdown(&buf, m.opts.Version, m.host, rs, report.NewRedactor(m.host))
	dir := m.host.Home
	if dir == "" {
		dir = os.TempDir()
	}
	name := filepath.Join(dir, "tuxmedic-report-"+time.Now().Format("2006-01-02-1504")+".md")
	if err := os.WriteFile(name, buf.Bytes(), 0o600); err != nil {
		return m.notify("Couldn't save the report: " + err.Error())
	}
	shown := strings.Replace(name, m.host.Home, "~", 1)
	return tea.Batch(tea.SetClipboard(buf.String()), m.notify("Saved a shareable report to "+shown+" and copied it"))
}

func (m *Model) openPalette() tea.Cmd {
	m.palette = true
	m.palSel = 0
	m.palInput.SetValue("")
	return m.palInput.Focus()
}

func (m *Model) paletteItems() []paletteItem {
	items := []paletteItem{
		{"Run every check again", "R", func(m *Model) tea.Cmd { return tea.Batch(m.runAll(), m.notify("Running every check again…")) }},
		{"Save a report to share (names and addresses removed)", "x", func(m *Model) tea.Cmd { return m.export() }},
		{"Show or hide log details", "e", func(m *Model) tea.Cmd {
			if c := m.current(); c != nil {
				m.evidence[c.ID] = !m.evidence[c.ID]
				m.refreshDetail()
			}
			return nil
		}},
		{"Keyboard shortcuts", "?", func(m *Model) tea.Cmd { m.help = true; return nil }},
		{"Switch between Unicode and plain ASCII symbols", "", func(m *Model) tea.Cmd {
			m.opts.ASCII = !m.opts.ASCII
			m.theme = newTheme(m.dark, m.opts.ASCII)
			m.refreshDetail()
			return nil
		}},
		{"Quit", "q", func(m *Model) tea.Cmd { return m.quit() }},
	}
	for i, c := range m.checks {
		idx := i
		items = append(items, paletteItem{"Go to " + c.Name + ": " + c.Question, "", func(m *Model) tea.Cmd {
			m.filter.SetValue("")
			m.rebuildEntries()
			for j, e := range m.entries {
				if !e.overview && e.check == idx {
					m.selectIndex(j)
				}
			}
			if m.singlePane() {
				m.inDetail = true
			}
			return nil
		}})
	}
	q := strings.ToLower(m.palInput.Value())
	if q == "" {
		return items
	}
	var out []paletteItem
	for _, it := range items {
		if fuzzy(strings.ToLower(it.label), q) {
			out = append(out, it)
		}
	}
	return out
}

func (m *Model) paletteKey(msg tea.KeyPressMsg) tea.Cmd {
	items := m.paletteItems()
	switch msg.String() {
	case "esc", "ctrl+p":
		m.palette = false
		m.palInput.Blur()
		return nil
	case "up", "ctrl+k":
		m.palSel = max(0, m.palSel-1)
		return nil
	case "down", "ctrl+j":
		m.palSel = min(len(items)-1, m.palSel+1)
		return nil
	case "enter":
		m.palette = false
		m.palInput.Blur()
		if m.palSel < len(items) {
			return items[m.palSel].run(m)
		}
		return nil
	}
	var cmd tea.Cmd
	m.palInput, cmd = m.palInput.Update(msg)
	m.palSel = 0
	return cmd
}

func (m *Model) overList(x int) bool {
	if m.singlePane() {
		return !m.inDetail
	}
	return x < m.sidebarWidth()
}

func (m *Model) wheel(ms tea.Mouse) {
	up := ms.Button == tea.MouseWheelUp
	switch {
	case m.overList(ms.X) && up:
		m.selectIndex(m.sel - 1)
	case m.overList(ms.X):
		m.selectIndex(m.sel + 1)
	case up:
		m.detail.ScrollUp(3)
	default:
		m.detail.ScrollDown(3)
	}
}

func (m *Model) click(ms tea.Mouse) {
	if ms.Button != tea.MouseLeft || m.palette || m.help || m.confirm != nil {
		return
	}
	if !m.overList(ms.X) {
		m.focus = focusDetail
		return
	}
	row := ms.Y - headerHeight
	if row < 0 || row >= len(m.sideRows) || m.sideRows[row] < 0 {
		return
	}
	m.focus = focusList
	m.selectIndex(m.sideRows[row])
	if m.singlePane() {
		m.inDetail = true
	}
}

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%d ms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1f s", d.Seconds())
}
