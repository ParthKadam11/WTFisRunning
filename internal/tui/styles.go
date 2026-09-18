package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/ParthKadam11/WTFisRunning/internal/model"
)

// User palette:
//
//	#F24236  coral red
//	#2E86AB  steel blue
//	#F6F5AE  soft cream
//	#F5F749  bright yellow
//	#565554  warm gray
var (
	cRed    = lipgloss.Color("#F24236")
	cBlue   = lipgloss.Color("#2E86AB")
	cCream  = lipgloss.Color("#F6F5AE")
	cYellow = lipgloss.Color("#F5F749")
	cGray   = lipgloss.Color("#565554")

	cBg       = lipgloss.Color("#1a1a19")
	cPanel    = lipgloss.Color("#242423")
	cFg       = cCream
	cMuted    = cGray
	cDim      = lipgloss.Color("#3d3d3c")
	cSelectBg = lipgloss.Color("#2a2a28")
	cBorder   = cGray
)

type styles struct {
	App        lipgloss.Style
	TitleWTF   lipgloss.Style
	TitleRest  lipgloss.Style
	Subtitle   lipgloss.Style
	Section    lipgloss.Style
	Rule       lipgloss.Style
	Primary    lipgloss.Style
	Secondary  lipgloss.Style
	Muted      lipgloss.Style
	Port       lipgloss.Style
	Selected   lipgloss.Style
	Cursor     lipgloss.Style
	Footer     lipgloss.Style
	HelpKey    lipgloss.Style
	HelpLabel  lipgloss.Style
	StatusRun  lipgloss.Style
	StatusStop lipgloss.Style
	StatusWarn lipgloss.Style
	StatusFail lipgloss.Style
	StatusUnk  lipgloss.Style
	Label      lipgloss.Style
	Value      lipgloss.Style
	Loading    lipgloss.Style
	Box        lipgloss.Style
	ChipOK     lipgloss.Style
	ChipBad    lipgloss.Style
	BadgeCtr   lipgloss.Style
	BadgeSys   lipgloss.Style
	BadgeNgx   lipgloss.Style
	BadgeProc  lipgloss.Style
	BadgePort  lipgloss.Style
	Tree       lipgloss.Style
	Arrow      lipgloss.Style
	MetricVal  lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		App: lipgloss.NewStyle().Foreground(cFg),
		TitleWTF: lipgloss.NewStyle().
			Foreground(cRed).
			Bold(true),
		TitleRest: lipgloss.NewStyle().
			Foreground(cYellow).
			Bold(true),
		Subtitle: lipgloss.NewStyle().
			Foreground(cBlue),
		Section: lipgloss.NewStyle().
			Foreground(cBlue).
			Bold(true).
			MarginTop(1),
		Rule: lipgloss.NewStyle().
			Foreground(cGray),
		Primary: lipgloss.NewStyle().
			Foreground(cCream),
		Secondary: lipgloss.NewStyle().
			Foreground(cGray),
		Muted: lipgloss.NewStyle().
			Foreground(cGray),
		Port: lipgloss.NewStyle().
			Foreground(cYellow),
		Selected: lipgloss.NewStyle().
			Foreground(cCream).
			Background(cSelectBg).
			Bold(true),
		Cursor: lipgloss.NewStyle().
			Foreground(cRed).
			Bold(true),
		Footer: lipgloss.NewStyle().
			Foreground(cGray).
			MarginTop(1),
		HelpKey: lipgloss.NewStyle().
			Foreground(cRed).
			Bold(true),
		HelpLabel: lipgloss.NewStyle().
			Foreground(cGray),
		StatusRun: lipgloss.NewStyle().
			Foreground(cYellow),
		StatusStop: lipgloss.NewStyle().
			Foreground(cGray),
		StatusWarn: lipgloss.NewStyle().
			Foreground(cYellow),
		StatusFail: lipgloss.NewStyle().
			Foreground(cRed),
		StatusUnk: lipgloss.NewStyle().
			Foreground(cGray),
		Label: lipgloss.NewStyle().
			Foreground(cBlue).
			Bold(true).
			Width(14),
		Value: lipgloss.NewStyle().
			Foreground(cCream),
		Loading: lipgloss.NewStyle().
			Foreground(cBlue).
			Italic(true),
		Box: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBlue).
			Padding(0, 1),
		ChipOK: lipgloss.NewStyle().
			Foreground(cYellow).
			Background(cPanel).
			Padding(0, 1),
		ChipBad: lipgloss.NewStyle().
			Foreground(cRed).
			Background(cPanel).
			Padding(0, 1),
		BadgeCtr: lipgloss.NewStyle().
			Foreground(cBg).
			Background(cBlue).
			Bold(true).
			Padding(0, 1),
		BadgeSys: lipgloss.NewStyle().
			Foreground(cCream).
			Background(cGray).
			Bold(true).
			Padding(0, 1),
		BadgeNgx: lipgloss.NewStyle().
			Foreground(cBg).
			Background(cYellow).
			Bold(true).
			Padding(0, 1),
		BadgeProc: lipgloss.NewStyle().
			Foreground(cCream).
			Background(cRed).
			Bold(true).
			Padding(0, 1),
		BadgePort: lipgloss.NewStyle().
			Foreground(cBg).
			Background(cCream).
			Bold(true).
			Padding(0, 1),
		Tree: lipgloss.NewStyle().
			Foreground(cGray),
		Arrow: lipgloss.NewStyle().
			Foreground(cRed),
		MetricVal: lipgloss.NewStyle().
			Foreground(cYellow).
			Bold(true),
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

func (s styles) kindBadge(kind string) string {
	switch kind {
	case "container":
		return s.BadgeCtr.Render("ctr")
	case "systemd":
		return s.BadgeSys.Render("sys")
	case "nginx":
		return s.BadgeNgx.Render("ngx")
	case "process":
		return s.BadgeProc.Render("proc")
	case "port":
		return s.BadgePort.Render("port")
	default:
		return s.BadgePort.Render(kind)
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

func gradientRule(width int, s styles) string {
	if width < 4 {
		return s.Rule.Render(rule(width))
	}
	// red → blue → yellow
	a := width / 3
	b := width / 3
	c := width - a - b
	return lipgloss.NewStyle().Foreground(cRed).Render(strings.Repeat("─", a)) +
		lipgloss.NewStyle().Foreground(cBlue).Render(strings.Repeat("─", b)) +
		lipgloss.NewStyle().Foreground(cYellow).Render(strings.Repeat("─", c))
}
