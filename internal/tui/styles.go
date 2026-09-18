package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

var (
	colorBg       = lipgloss.Color("#0c0c0e")
	colorFg       = lipgloss.Color("#e8e6e3")
	colorMuted    = lipgloss.Color("#6b6860")
	colorDim      = lipgloss.Color("#3d3b38")
	colorAccent   = lipgloss.Color("#c4b5a0")
	colorBorder   = lipgloss.Color("#2a2826")
	colorSelect   = lipgloss.Color("#1a1917")
	colorSelectFg = lipgloss.Color("#f5f0e8")
	colorHealthy  = lipgloss.Color("#7d9b76")
	colorWarn     = lipgloss.Color("#c4a35a")
	colorError    = lipgloss.Color("#b56a5a")
	colorUnknown  = lipgloss.Color("#6b6860")
)

type styles struct {
	App        lipgloss.Style
	Title      lipgloss.Style
	Subtitle   lipgloss.Style
	Section    lipgloss.Style
	Rule       lipgloss.Style
	Primary    lipgloss.Style
	Secondary  lipgloss.Style
	Muted      lipgloss.Style
	Selected   lipgloss.Style
	Cursor     lipgloss.Style
	Footer     lipgloss.Style
	Help       lipgloss.Style
	StatusRun  lipgloss.Style
	StatusStop lipgloss.Style
	StatusWarn lipgloss.Style
	StatusFail lipgloss.Style
	StatusUnk  lipgloss.Style
	Label      lipgloss.Style
	Value      lipgloss.Style
	Loading    lipgloss.Style
	Box        lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		App: lipgloss.NewStyle().
			Foreground(colorFg),
		Title: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),
		Subtitle: lipgloss.NewStyle().
			Foreground(colorMuted),
		Section: lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true).
			MarginTop(1),
		Rule: lipgloss.NewStyle().
			Foreground(colorDim),
		Primary: lipgloss.NewStyle().
			Foreground(colorFg),
		Secondary: lipgloss.NewStyle().
			Foreground(colorMuted),
		Muted: lipgloss.NewStyle().
			Foreground(colorMuted),
		Selected: lipgloss.NewStyle().
			Foreground(colorSelectFg).
			Background(colorSelect).
			Bold(true),
		Cursor: lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true),
		Footer: lipgloss.NewStyle().
			Foreground(colorMuted).
			MarginTop(1),
		Help: lipgloss.NewStyle().
			Foreground(colorDim),
		StatusRun: lipgloss.NewStyle().
			Foreground(colorHealthy),
		StatusStop: lipgloss.NewStyle().
			Foreground(colorMuted),
		StatusWarn: lipgloss.NewStyle().
			Foreground(colorWarn),
		StatusFail: lipgloss.NewStyle().
			Foreground(colorError),
		StatusUnk: lipgloss.NewStyle().
			Foreground(colorUnknown),
		Label: lipgloss.NewStyle().
			Foreground(colorMuted).
			Bold(true).
			Width(14),
		Value: lipgloss.NewStyle().
			Foreground(colorFg),
		Loading: lipgloss.NewStyle().
			Foreground(colorMuted).
			Italic(true),
		Box: lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1),
	}
}

func statusGlyph(s model.Status) string {
	switch s {
	case model.StatusRunning:
		return "●"
	case model.StatusStopped:
		return "○"
	case model.StatusWarning:
		return "!"
	case model.StatusFailed:
		return "✕"
	default:
		return "?"
	}
}

func (s styles) statusStyle(st model.Status) lipgloss.Style {
	switch st {
	case model.StatusRunning:
		return s.StatusRun
	case model.StatusStopped:
		return s.StatusStop
	case model.StatusWarning:
		return s.StatusWarn
	case model.StatusFailed:
		return s.StatusFail
	default:
		return s.StatusUnk
	}
}

func formatPorts(ports []int) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf(":%d", p))
	}
	return strings.Join(parts, " ")
}

func padRight(s string, width int) string {
	r := []rune(s)
	if len(r) >= width {
		if width <= 1 {
			return string(r[:width])
		}
		return string(r[:width-1]) + "…"
	}
	return s + strings.Repeat(" ", width-len(r))
}

func truncate(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width <= 1 {
		return string(r[:width])
	}
	return string(r[:width-1]) + "…"
}

func rule(width int) string {
	if width < 1 {
		width = 1
	}
	return strings.Repeat("─", width)
}
