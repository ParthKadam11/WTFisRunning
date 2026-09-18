package tui

import (
	"fmt"
	"strings"

	"github.com/ParthKadam11/WTFisRunning/internal/model"
	"github.com/charmbracelet/lipgloss"
)

// Brand palette:
//
//	#8ecae6  sky
//	#219ebc  teal
//	#ffb703  gold
//	#fb8500  orange
var (
	cSky    = lipgloss.Color("#8ECAE6")
	cTeal   = lipgloss.Color("#219EBC")
	cGold   = lipgloss.Color("#FFB703")
	cOrange = lipgloss.Color("#FB8500")

	cBg       = lipgloss.Color("#0B1620")
	cPanel    = lipgloss.Color("#122533")
	cFg       = lipgloss.Color("#E8F4F8")
	cMuted    = lipgloss.Color("#6B8A9A")
	cDim      = lipgloss.Color("#1E3240")
	cSelectBg = lipgloss.Color("#1A3544")
	cBorder   = cTeal
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
			Foreground(cOrange).
			Bold(true),
		TitleRest: lipgloss.NewStyle().
			Foreground(cGold).
			Bold(true),
		Subtitle: lipgloss.NewStyle().
			Foreground(cTeal),
		Section: lipgloss.NewStyle().
			Foreground(cSky).
			Bold(true).
			MarginTop(1),
		Rule: lipgloss.NewStyle().
			Foreground(cDim),
		Primary: lipgloss.NewStyle().
			Foreground(cFg),
		Secondary: lipgloss.NewStyle().
			Foreground(cMuted),
		Muted: lipgloss.NewStyle().
			Foreground(cMuted),
		Port: lipgloss.NewStyle().
			Foreground(cGold),
		Selected: lipgloss.NewStyle().
			Foreground(cFg).
			Background(cSelectBg).
			Bold(true),
		Cursor: lipgloss.NewStyle().
			Foreground(cOrange).
			Bold(true),
		Footer: lipgloss.NewStyle().
			Foreground(cMuted).
			MarginTop(1),
		HelpKey: lipgloss.NewStyle().
			Foreground(cOrange).
			Bold(true),
		HelpLabel: lipgloss.NewStyle().
			Foreground(cMuted),
		StatusRun: lipgloss.NewStyle().
			Foreground(cTeal),
		StatusStop: lipgloss.NewStyle().
			Foreground(cMuted),
		StatusWarn: lipgloss.NewStyle().
			Foreground(cGold),
		StatusFail: lipgloss.NewStyle().
			Foreground(cOrange),
		StatusUnk: lipgloss.NewStyle().
			Foreground(cMuted),
		Label: lipgloss.NewStyle().
			Foreground(cTeal).
			Bold(true).
			Width(14),
		Value: lipgloss.NewStyle().
			Foreground(cFg),
		Loading: lipgloss.NewStyle().
			Foreground(cSky).
			Italic(true),
		Box: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBorder).
			Padding(0, 1),
		ChipOK: lipgloss.NewStyle().
			Foreground(cTeal).
			Background(cPanel).
			Padding(0, 1),
		ChipBad: lipgloss.NewStyle().
			Foreground(cOrange).
			Background(cPanel).
			Padding(0, 1),
		BadgeCtr: lipgloss.NewStyle().
			Foreground(cBg).
			Background(cTeal).
			Bold(true).
			Padding(0, 1),
		BadgeSys: lipgloss.NewStyle().
			Foreground(cFg).
			Background(cDim).
			Bold(true).
			Padding(0, 1),
		BadgeNgx: lipgloss.NewStyle().
			Foreground(cBg).
			Background(cGold).
			Bold(true).
			Padding(0, 1),
		BadgeProc: lipgloss.NewStyle().
			Foreground(cBg).
			Background(cOrange).
			Bold(true).
			Padding(0, 1),
		BadgePort: lipgloss.NewStyle().
			Foreground(cBg).
			Background(cSky).
			Bold(true).
			Padding(0, 1),
		Tree: lipgloss.NewStyle().
			Foreground(cMuted),
		Arrow: lipgloss.NewStyle().
			Foreground(cOrange),
		MetricVal: lipgloss.NewStyle().
			Foreground(cGold).
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
	// sky → teal → gold → orange
	q := width / 4
	r := width - 3*q
	return lipgloss.NewStyle().Foreground(cSky).Render(strings.Repeat("─", q)) +
		lipgloss.NewStyle().Foreground(cTeal).Render(strings.Repeat("─", q)) +
		lipgloss.NewStyle().Foreground(cGold).Render(strings.Repeat("─", q)) +
		lipgloss.NewStyle().Foreground(cOrange).Render(strings.Repeat("─", r))
}
