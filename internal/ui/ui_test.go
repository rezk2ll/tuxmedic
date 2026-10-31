package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/sys"
)

func fakeChecks() []doctor.Check {
	mk := func(id, name, cat string, fs ...doctor.Finding) doctor.Check {
		return doctor.Check{ID: id, Name: name, Category: cat, Question: "Why does " + name + " misbehave on this machine today?",
			Run: func(context.Context, sys.System, *doctor.Host) []doctor.Finding { return fs }}
	}
	long := strings.Repeat("a long explanation that has to wrap nicely ", 8)
	return []doctor.Check{
		mk("crash", "Crashes & freezes", "Stability", doctor.Finding{Status: doctor.Fail, Title: "Your computer crashed 2 times", Explain: long,
			Fixes:    []doctor.Fix{{Title: "Add swap", Command: "sudo apt install systemd-zram-generator && sudo systemctl daemon-reload", NeedsRoot: true}},
			Evidence: []string{"kernel: Out of memory: Killed process 4242 (firefox) total-vm:123456kB, anon-rss:654321kB, file-rss:0kB"}}),
		mk("network", "Internet & Wi-Fi", "Network", doctor.Finding{Status: doctor.OK, Title: "You're online"}),
		mk("sound", "Sound", "Devices", doctor.Finding{Status: doctor.Warn, Title: "Sound is muted", Fixes: []doctor.Fix{{Title: "Unmute", Command: "wpctl set-mute @DEFAULT_AUDIO_SINK@ 0"}}}),
		mk("battery", "Battery & charging", "Power", doctor.Finding{Status: doctor.Info, Title: "Charging stops at 80%"}),
		mk("disk", "Disks & storage", "Storage", doctor.Finding{Status: doctor.OK, Title: "Enough free space"}),
		mk("memory", "Memory", "Performance", doctor.Finding{Status: doctor.OK, Title: "Plenty of free memory"}),
		mk("boot", "Startup & services", "System", doctor.Finding{Status: doctor.Skipped, Title: "No systemd"}),
	}
}

func newTestModel(t *testing.T, ascii bool) *Model {
	t.Helper()
	h := &doctor.Host{Distro: "Arch Linux", Kernel: "6.17.1-arch1-1", Desktop: "KDE", Session: "wayland", SoundServer: "pipewire", Network: "NetworkManager", PackageMgr: "pacman"}
	m := New(&sys.Fake{}, h, fakeChecks(), Options{Version: "test", ASCII: ascii})
	for _, c := range m.checks {
		m.Update(reportMsg{doctor.RunOne(context.Background(), c, &sys.Fake{}, h)})
	}
	return m
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "ctrl+p":
		return tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// Every screen must fill the window exactly: a line too wide wraps in the
// terminal and shifts everything below it.
func TestViewFitsEverySize(t *testing.T) {
	sizes := [][2]int{{40, 10}, {60, 15}, {80, 24}, {83, 30}, {84, 24}, {100, 30}, {120, 40}, {200, 60}}
	screens := map[string][]string{
		"overview": nil,
		"check":    {"down", "enter"},
		"evidence": {"down", "enter", "e"},
		"help":     {"?"},
		"palette":  {"ctrl+p"},
		"confirm":  {"down", "enter", "1"},
		"filter":   {"/", "s", "o"},
	}
	for _, ascii := range []bool{false, true} {
		for name, keys := range screens {
			for _, sz := range sizes {
				t.Run(fmt.Sprintf("%s/%dx%d/ascii=%v", name, sz[0], sz[1], ascii), func(t *testing.T) {
					m := newTestModel(t, ascii)
					m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
					for _, k := range keys {
						m.Update(key(k))
					}
					out := m.View().Content
					lines := strings.Split(out, "\n")
					if len(lines) > sz[1] {
						t.Errorf("%d lines, window has %d", len(lines), sz[1])
					}
					for i, l := range lines {
						if w := lipgloss.Width(l); w > sz[0] {
							t.Errorf("line %d is %d wide, window is %d: %q", i, w, sz[0], l)
						}
					}
				})
			}
		}
	}
}

func TestTooSmall(t *testing.T) {
	m := newTestModel(t, false)
	m.Update(tea.WindowSizeMsg{Width: 30, Height: 8})
	if out := m.View().Content; !strings.Contains(out, "40x10") || !strings.Contains(out, "--plain") {
		t.Errorf("want a too-small message, got:\n%s", out)
	}
}

func TestOverviewListsProblemsFirst(t *testing.T) {
	m := newTestModel(t, false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	out := m.View().Content
	crash, sound, mem := strings.Index(out, "Your computer crashed"), strings.Index(out, "Sound is muted"), strings.LastIndex(out, "Memory")
	if crash < 0 || sound < 0 || !(crash < sound && sound < mem) {
		t.Errorf("want problem, then warning, then healthy checks:\n%s", out)
	}
	if !strings.Contains(out, "1 problem and 1 warning found") {
		t.Errorf("missing summary:\n%s", out)
	}
}

func TestNumberOnOverviewOpensCheck(t *testing.T) {
	m := newTestModel(t, false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(key("2"))
	if c := m.current(); c == nil || c.ID != "sound" {
		t.Fatalf("want the second attention item (sound) open, got %+v", c)
	}
	m.Update(key("1"))
	if m.confirm == nil || !strings.Contains(m.confirm.fix.Command, "wpctl set-mute") {
		t.Fatalf("want the unmute fix to ask for confirmation, got %+v", m.confirm)
	}
	m.Update(key("esc"))
	if m.confirm != nil {
		t.Error("esc should cancel the confirmation")
	}
}

func TestFilterFindsByQuestionWords(t *testing.T) {
	m := newTestModel(t, false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.Update(key("/"))
	for _, r := range "wifi" {
		m.Update(key(string(r)))
	}
	if len(m.entries) != 1 || m.checks[m.entries[0].check].ID != "network" {
		t.Errorf("want only the network check, got %d entries", len(m.entries))
	}
}

func TestQuitCancelsRunningChecks(t *testing.T) {
	m := newTestModel(t, false)
	_, cmd := m.Update(key("q"))
	if cmd == nil || m.ctx.Err() == nil {
		t.Error("q should quit and cancel the checks' context")
	}
}
