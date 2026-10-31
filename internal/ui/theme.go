package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/rezk2ll/tuxmedic/internal/doctor"
	"github.com/rezk2ll/tuxmedic/internal/report"
)

// theme uses the terminal's own 16 ANSI colors, so it matches whatever palette
// the user picked and stays readable on light and dark backgrounds alike.
type theme struct {
	ascii  bool
	glyphs report.Glyphs
	border lipgloss.Border

	accent, muted, ok, info, warn, fail, skip color.Color

	title, subtle, bold, code, key, selected, selectedDim, header, chip lipgloss.Style
	spinner                                                             []string
}

func newTheme(dark, ascii bool) theme {
	t := theme{ascii: ascii}
	if ascii {
		t.glyphs = report.ASCII
		t.border = lipgloss.ASCIIBorder()
		t.spinner = []string{"|", "/", "-", "\\"}
	} else {
		t.glyphs = report.Unicode
		t.border = lipgloss.RoundedBorder()
		t.spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	}
	t.accent = lipgloss.Color("6")
	t.ok = lipgloss.Color("2")
	t.info = lipgloss.Color("4")
	t.warn = lipgloss.Color("3")
	t.fail = lipgloss.Color("1")
	t.muted = lipgloss.Color("8")
	if !dark {
		// Bright black is nearly invisible on some light palettes.
		t.muted = lipgloss.Color("244")
	}
	t.skip = t.muted

	t.title = lipgloss.NewStyle().Bold(true)
	t.subtle = lipgloss.NewStyle().Foreground(t.muted)
	t.bold = lipgloss.NewStyle().Bold(true)
	t.code = lipgloss.NewStyle().Foreground(t.accent)
	t.key = lipgloss.NewStyle().Bold(true).Foreground(t.accent)
	t.selected = lipgloss.NewStyle().Reverse(true).Bold(true)
	t.selectedDim = lipgloss.NewStyle().Bold(true).Underline(true)
	t.header = lipgloss.NewStyle().Bold(true).Foreground(t.accent)
	t.chip = lipgloss.NewStyle().Bold(true).Padding(0, 1)
	return t
}

func (t theme) color(s doctor.Status) color.Color {
	switch s {
	case doctor.OK:
		return t.ok
	case doctor.Info:
		return t.info
	case doctor.Warn:
		return t.warn
	case doctor.Fail:
		return t.fail
	}
	return t.skip
}

func (t theme) glyph(s doctor.Status) string {
	return lipgloss.NewStyle().Foreground(t.color(s)).Bold(true).Render(t.glyphs.For(s))
}

func (t theme) badge(s doctor.Status) string {
	label := map[doctor.Status]string{
		doctor.OK: "OK", doctor.Info: "NOTE", doctor.Skipped: "SKIPPED", doctor.Warn: "WARNING", doctor.Fail: "PROBLEM",
	}[s]
	return lipgloss.NewStyle().Foreground(t.color(s)).Bold(true).Render(t.glyphs.For(s) + " " + label)
}
