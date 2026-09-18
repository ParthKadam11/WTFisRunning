package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/wtfisrunning/wtfisrunning/internal/discovery"
	execx "github.com/wtfisrunning/wtfisrunning/internal/exec"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
	"github.com/wtfisrunning/wtfisrunning/internal/output"
)

type viewState int

const (
	viewOverview viewState = iota
	viewInspect
	viewImpact
)

type discoverMsg struct {
	runtime *model.Runtime
	err     error
	seq     int
}

// Model is the Bubble Tea application model.
type Model struct {
	runner execx.Runner
	styles styles

	width  int
	height int

	runtime     *model.Runtime
	loading     bool
	loadErr     error
	refreshed   time.Time
	discoverSeq int
	activeSeq   int

	view      viewState
	cursor    int
	filter    string
	filtering bool

	selectedID string
}

// New creates the TUI model.
func New(runner execx.Runner) Model {
	return Model{
		runner:      runner,
		styles:      defaultStyles(),
		loading:     true,
		view:        viewOverview,
		discoverSeq: 1,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.discoverCmd(), tea.SetWindowTitle("wtfisrunning"))
}

func (m Model) discoverCmd() tea.Cmd {
	seq := m.discoverSeq
	runner := m.runner
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		rt := discovery.Discover(ctx, runner)
		return discoverMsg{runtime: rt, seq: seq}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case discoverMsg:
		if msg.seq != m.discoverSeq {
			return m, nil // stale refresh result
		}
		m.activeSeq = msg.seq
		m.loading = false
		m.loadErr = msg.err
		if msg.runtime != nil {
			m.runtime = msg.runtime
			m.refreshed = time.Now()
			m.clampCursor()
			if m.selectedID != "" && m.runtime.ServiceByID(m.selectedID) == nil {
				m.selectedID = ""
			}
		}
		return m, nil

	case tea.KeyMsg:
		if m.filtering {
			return m.updateFilter(msg)
		}
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "esc":
			if m.view != viewOverview {
				m.view = viewOverview
				return m, nil
			}
			if m.filter != "" {
				m.filter = ""
				m.clampCursor()
				return m, nil
			}
			return m, tea.Quit
		case "r":
			if m.loading {
				return m, nil
			}
			m.loading = true
			m.discoverSeq++
			return m, m.discoverCmd()
		case "up", "k":
			if m.view == viewOverview {
				if m.cursor > 0 {
					m.cursor--
				}
			}
			return m, nil
		case "down", "j":
			if m.view == viewOverview {
				items := m.visibleServices()
				if m.cursor < len(items)-1 {
					m.cursor++
				}
			}
			return m, nil
		case "enter":
			if m.view == viewOverview {
				items := m.visibleServices()
				if m.cursor >= 0 && m.cursor < len(items) {
					m.selectedID = items[m.cursor].ID
					m.view = viewInspect
				}
			}
			return m, nil
		case "i":
			if m.view == viewOverview {
				items := m.visibleServices()
				if m.cursor >= 0 && m.cursor < len(items) {
					m.selectedID = items[m.cursor].ID
					if m.hasImpact(m.selectedID) {
						m.view = viewImpact
					}
				}
			} else if m.view == viewInspect && m.hasImpact(m.selectedID) {
				m.view = viewImpact
			}
			return m, nil
		case "/":
			if m.view == viewOverview {
				m.filtering = true
			}
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateFilter(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		m.filtering = false
		return m, nil
	case "enter":
		m.filtering = false
		m.clampCursor()
		return m, nil
	case "backspace":
		if len(m.filter) > 0 {
			r := []rune(m.filter)
			m.filter = string(r[:len(r)-1])
		}
		m.clampCursor()
		return m, nil
	default:
		if len(msg.Runes) > 0 && msg.Type == tea.KeyRunes {
			m.filter += string(msg.Runes)
			m.clampCursor()
		}
		return m, nil
	}
}

func (m *Model) clampCursor() {
	items := m.visibleServices()
	if len(items) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m Model) visibleServices() []model.Service {
	if m.runtime == nil {
		return nil
	}
	if m.filter == "" {
		return m.runtime.Services
	}
	f := strings.ToLower(m.filter)
	var out []model.Service
	for _, s := range m.runtime.Services {
		if strings.Contains(strings.ToLower(s.Name), f) ||
			strings.Contains(strings.ToLower(s.Kind), f) ||
			strings.Contains(strings.ToLower(s.Image), f) {
			out = append(out, s)
		}
	}
	return out
}

func (m Model) hasImpact(id string) bool {
	if m.runtime == nil {
		return false
	}
	svc := m.runtime.ServiceByID(id)
	if svc == nil {
		return false
	}
	for _, r := range m.runtime.Relations {
		if r.Type != model.RelProxiesTo && r.Type != model.RelSameNetwork && r.Type != model.RelDependsOn {
			continue
		}
		if relationTouches(r, svc) {
			return true
		}
	}
	return false
}

func relationTouches(r model.Relation, svc *model.Service) bool {
	if r.Source == svc.Name || r.Source == svc.ID {
		return true
	}
	if r.Destination == svc.Name || r.Destination == svc.ID {
		return true
	}
	if strings.HasPrefix(r.Destination, svc.Name+":") {
		return true
	}
	if strings.HasPrefix(r.Source, svc.Name+":") {
		return true
	}
	return false
}

func (m Model) View() string {
	if m.width == 0 {
		m.width = 80
	}
	if m.height == 0 {
		m.height = 24
	}

	var body string
	switch m.view {
	case viewInspect:
		body = m.renderInspect()
	case viewImpact:
		body = m.renderImpact()
	default:
		body = m.renderOverview()
	}

	content := m.styles.Box.
		Width(m.width - 2).
		Height(m.height - 2).
		Render(body)

	return content
}

func (m Model) renderHeader() string {
	host := "unknown"
	if m.runtime != nil && m.runtime.System.Hostname != "" {
		host = m.runtime.System.Hostname
	}
	title := m.styles.Title.Render("WTF IS RUNNING?")
	sub := m.styles.Subtitle.Render("runtime topology · " + host)
	return title + "\n" + sub
}

func (m Model) renderOverview() string {
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	if m.loading && m.runtime == nil {
		b.WriteString("\n")
		b.WriteString(m.styles.Loading.Render("  discovering runtime…"))
		b.WriteString("\n\n")
		b.WriteString(m.renderCollectorsPlaceholder())
		b.WriteString("\n")
		b.WriteString(m.renderFooter([]string{"q quit"}))
		return b.String()
	}

	innerWidth := m.width - 6
	if innerWidth < 40 {
		innerWidth = 40
	}

	wide := m.width >= 88

	servicesBlock := m.renderServices(innerWidth)
	systemBlock := m.renderSystem(28)
	topoBlock := m.renderTopology(innerWidth)
	collectorsBlock := m.renderCollectors(innerWidth)

	if wide {
		leftW := innerWidth * 55 / 100
		rightW := innerWidth - leftW - 3
		if rightW < 24 {
			rightW = 24
			leftW = innerWidth - rightW - 3
		}
		left := lipgloss.NewStyle().Width(leftW).Render(servicesBlock)
		right := lipgloss.NewStyle().Width(rightW).Render(systemBlock)
		row := lipgloss.JoinHorizontal(lipgloss.Top, left, "   ", right)
		b.WriteString(row)
	} else {
		b.WriteString(servicesBlock)
		b.WriteString("\n")
		b.WriteString(systemBlock)
	}

	b.WriteString("\n")
	b.WriteString(topoBlock)
	b.WriteString("\n")
	b.WriteString(collectorsBlock)

	if m.filtering || m.filter != "" {
		b.WriteString("\n")
		prompt := "/"
		if m.filtering {
			prompt = "/" + m.filter + "▌"
		} else {
			prompt = "filter: " + m.filter
		}
		b.WriteString(m.styles.Secondary.Render(prompt))
	}

	if m.loading {
		b.WriteString("\n")
		b.WriteString(m.styles.Loading.Render("refreshing…"))
	}

	b.WriteString("\n")
	b.WriteString(m.renderStats())
	b.WriteString("\n")
	hints := []string{"↑↓ navigate", "enter inspect", "i impact", "r refresh", "/ filter", "q quit"}
	if m.width < 70 {
		hints = []string{"↑↓", "enter", "r", "q"}
	}
	b.WriteString(m.renderFooter(hints))
	return b.String()
}

func (m Model) renderServices(width int) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Section.Render("SERVICES"))
	b.WriteString("\n")
	ruleW := width
	if ruleW > 28 {
		ruleW = 28
	}
	b.WriteString(s.Rule.Render(rule(ruleW)))
	b.WriteString("\n")

	items := m.visibleServices()
	if len(items) == 0 {
		msg := "No services discovered."
		if m.filter != "" {
			msg = "No matches."
		}
		b.WriteString(s.Muted.Render(msg))
		return b.String()
	}

	nameW := 16
	if width > 50 {
		nameW = 20
	}
	for i, svc := range items {
		glyph := s.statusStyle(svc.Status).Render(statusGlyph(svc.Status))
		name := padRight(svc.Name, nameW)
		ports := formatPorts(svc.Ports)
		line := fmt.Sprintf("%s %s  %s", glyph, name, s.Secondary.Render(ports))
		if i == m.cursor {
			cursor := s.Cursor.Render("›")
			line = cursor + " " + s.Selected.Render(fmt.Sprintf("%s %s  %s", statusGlyph(svc.Status), padRight(svc.Name, nameW), ports))
		} else {
			line = "  " + line
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderSystem(width int) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Section.Render("SYSTEM"))
	b.WriteString("\n")
	b.WriteString(s.Rule.Render(rule(22)))
	b.WriteString("\n")

	if m.runtime == nil {
		b.WriteString(s.Muted.Render("—"))
		return b.String()
	}
	sys := m.runtime.System
	rows := [][2]string{
		{"uptime", dash(sys.Uptime)},
		{"cpu", formatCPU(sys.CPUPercent)},
		{"memory", formatMem(sys.MemUsedGB, sys.MemTotalGB)},
		{"disk", formatDisk(sys.DiskPercent)},
	}
	if m.width >= 88 && sys.OS != "" {
		rows = append([][2]string{{"os", truncate(sys.OS, 28)}}, rows...)
	}
	for _, row := range rows {
		b.WriteString(fmt.Sprintf("%s  %s\n",
			s.Secondary.Render(padRight(row[0], 8)),
			s.Primary.Render(row[1]),
		))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderTopology(width int) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Section.Render("RUNTIME"))
	b.WriteString("\n")
	rw := width
	if rw > 50 {
		rw = 50
	}
	b.WriteString(s.Rule.Render(rule(rw)))
	b.WriteString("\n")

	if m.runtime == nil {
		return b.String()
	}
	lines := output.TopologyLines(m.runtime)
	if len(lines) == 0 {
		b.WriteString(s.Muted.Render("No topology relationships discovered."))
		return b.String()
	}
	maxLines := 12
	if m.height < 30 {
		maxLines = 6
	}
	for i, line := range lines {
		if i >= maxLines {
			b.WriteString(s.Muted.Render(fmt.Sprintf("  … %d more", len(lines)-maxLines)))
			break
		}
		b.WriteString(s.Primary.Render(line))
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderCollectors(width int) string {
	if m.runtime == nil {
		return ""
	}
	s := m.styles
	order := []string{"docker", "ports", "nginx", "systemd"}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		c, ok := m.runtime.Collectors[name]
		if !ok {
			continue
		}
		label := name
		val := collectorShort(c)
		style := s.Secondary
		if c.Status != model.CollectorOK {
			style = s.Muted
		}
		parts = append(parts, style.Render(label+"  "+val))
	}
	if len(parts) == 0 {
		return ""
	}
	return s.Muted.Render(strings.Join(parts, "   ·   "))
}

func (m Model) renderCollectorsPlaceholder() string {
	return m.styles.Muted.Render("  docker · ports · nginx · systemd · system")
}

func (m Model) renderStats() string {
	s := m.styles
	if m.runtime == nil {
		return ""
	}
	nRel := 0
	for _, r := range m.runtime.Relations {
		if r.Type == model.RelProxiesTo || r.Type == model.RelSameNetwork || r.Type == model.RelDependsOn {
			nRel++
		}
	}
	age := "just now"
	if !m.refreshed.IsZero() {
		d := time.Since(m.refreshed).Round(time.Second)
		if d < time.Second {
			age = "just now"
		} else {
			age = d.String() + " ago"
		}
	}
	left := fmt.Sprintf("%d services · %d ports · %d relationships",
		len(m.runtime.Services), len(m.runtime.Ports), nRel)
	right := "refreshed " + age
	gap := m.width - 8 - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return s.Muted.Render(left)
	}
	return s.Muted.Render(left + strings.Repeat(" ", gap) + right)
}

func (m Model) renderFooter(hints []string) string {
	return m.styles.Help.Render(strings.Join(hints, "   "))
}

func (m Model) renderInspect() string {
	s := m.styles
	var b strings.Builder
	svc := m.runtime.ServiceByID(m.selectedID)
	if svc == nil {
		b.WriteString(m.renderHeader())
		b.WriteString("\n\n")
		b.WriteString(s.Muted.Render("Service not found."))
		b.WriteString("\n\n")
		b.WriteString(m.renderFooter([]string{"esc back"}))
		return b.String()
	}

	b.WriteString(s.Title.Render(strings.ToUpper(svc.Name)))
	b.WriteString("\n")
	b.WriteString(s.Subtitle.Render(svc.Kind))
	b.WriteString("\n\n")

	section := func(title, body string) {
		if body == "" {
			return
		}
		b.WriteString(s.Section.MarginTop(0).Render(title))
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n\n")
	}

	stLine := s.statusStyle(svc.Status).Render(statusGlyph(svc.Status) + " " + string(svc.Status))
	section("STATUS", "  "+stLine)

	if svc.ContainerID != "" {
		section("CONTAINER", "  "+s.Primary.Render(svc.Name)+"  "+s.Muted.Render(svc.ContainerID))
	}
	if svc.Image != "" {
		section("IMAGE", "  "+s.Primary.Render(svc.Image))
	}
	if svc.Unit != "" && svc.Kind == "systemd" {
		section("UNIT", "  "+s.Primary.Render(svc.Unit))
	}
	if svc.PID > 0 {
		section("PID", "  "+s.Primary.Render(fmt.Sprintf("%d", svc.PID)))
	}
	if len(svc.Ports) > 0 {
		var lines []string
		if svc.ContainerID != "" {
			for _, c := range m.runtime.Containers {
				if c.Name == svc.Name {
					for _, pm := range c.Ports {
						if pm.HostPort > 0 {
							lines = append(lines, fmt.Sprintf("  %d → %d/%s", pm.HostPort, pm.ContainerPort, pm.Protocol))
						} else {
							lines = append(lines, fmt.Sprintf("  %d/%s", pm.ContainerPort, pm.Protocol))
						}
					}
					break
				}
			}
		}
		if len(lines) == 0 {
			for _, p := range svc.Ports {
				lines = append(lines, fmt.Sprintf("  :%d", p))
			}
		}
		section("PORTS", strings.Join(lines, "\n"))
	}
	if len(svc.Networks) > 0 {
		section("NETWORKS", "  "+strings.Join(svc.Networks, ", "))
	}

	deps := m.outgoingFor(svc)
	if len(deps) > 0 {
		var lines []string
		for _, r := range deps {
			label := r.Destination
			if r.Label != "" && r.Type == model.RelSameNetwork {
				label = r.Destination + "  " + s.Muted.Render("("+r.Label+")")
			}
			lines = append(lines, "  → "+label)
		}
		section("RELATED", strings.Join(lines, "\n"))
	}

	exposed := m.incomingFor(svc)
	if len(exposed) > 0 {
		var lines []string
		for _, r := range exposed {
			line := "  → " + r.Source
			if r.Label != "" {
				line += "\n      " + s.Muted.Render(r.Label)
			}
			lines = append(lines, line)
		}
		section("EXPOSED THROUGH", strings.Join(lines, "\n"))
	}

	hints := []string{"esc back", "r refresh", "q quit"}
	if m.hasImpact(svc.ID) {
		hints = []string{"esc back", "i impact", "r refresh", "q quit"}
	}
	b.WriteString(m.renderFooter(hints))
	return b.String()
}

func (m Model) renderImpact() string {
	s := m.styles
	var b strings.Builder
	svc := m.runtime.ServiceByID(m.selectedID)
	if svc == nil {
		b.WriteString(s.Muted.Render("Service not found."))
		return b.String()
	}

	b.WriteString(s.Title.Render("IMPACT · " + svc.Name))
	b.WriteString("\n\n")
	ports := formatPorts(svc.Ports)
	b.WriteString(s.Primary.Render(svc.Name))
	if ports != "" {
		b.WriteString(s.Secondary.Render("  " + ports))
	}
	b.WriteString("\n\n")

	usedBy := m.incomingFor(svc)
	related := m.networkPeers(svc)

	if len(usedBy) > 0 {
		b.WriteString(s.Section.MarginTop(0).Render("USED BY"))
		b.WriteString("\n")
		for _, r := range usedBy {
			line := "  → " + r.Source
			if r.Label != "" {
				line += s.Muted.Render("  (" + r.Label + ")")
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(related) > 0 {
		b.WriteString(s.Section.MarginTop(0).Render("SAME NETWORK"))
		b.WriteString("\n")
		for _, name := range related {
			b.WriteString("  → " + name + "\n")
		}
		b.WriteString("\n")
	}

	if len(usedBy) == 0 && len(related) == 0 {
		b.WriteString(s.Muted.Render("No discovered dependents."))
		b.WriteString("\n\n")
	} else {
		n := len(usedBy) + len(related)
		b.WriteString(s.Muted.Render(fmt.Sprintf("%d services potentially affected", n)))
		b.WriteString("\n\n")
	}

	b.WriteString(m.renderFooter([]string{"esc back", "q quit"}))
	return b.String()
}

func (m Model) outgoingFor(svc *model.Service) []model.Relation {
	var out []model.Relation
	for _, r := range m.runtime.Relations {
		if r.Type == model.RelListensOn {
			continue
		}
		if r.Source == svc.Name || r.Source == svc.ID {
			out = append(out, r)
		}
	}
	return out
}

func (m Model) incomingFor(svc *model.Service) []model.Relation {
	var out []model.Relation
	for _, r := range m.runtime.Relations {
		if r.Type != model.RelProxiesTo && r.Type != model.RelDependsOn {
			continue
		}
		if r.Destination == svc.Name || r.Destination == svc.ID ||
			strings.HasPrefix(r.Destination, svc.Name+":") {
			out = append(out, r)
		}
	}
	return out
}

func (m Model) networkPeers(svc *model.Service) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range m.runtime.Relations {
		if r.Type != model.RelSameNetwork {
			continue
		}
		var peer string
		if r.Source == svc.Name {
			peer = r.Destination
		} else if r.Destination == svc.Name {
			peer = r.Source
		}
		if peer == "" || seen[peer] {
			continue
		}
		seen[peer] = true
		out = append(out, peer)
	}
	return out
}

func collectorShort(c model.CollectorResult) string {
	switch c.Status {
	case model.CollectorOK:
		if c.Message != "" {
			return c.Message
		}
		return "ok"
	case model.CollectorNotInstalled:
		return "not installed"
	case model.CollectorUnavailable:
		return "unavailable"
	case model.CollectorPermissionDenied:
		return "denied"
	case model.CollectorTimeout:
		return "timeout"
	default:
		return string(c.Status)
	}
}

func formatCPU(v float64) string {
	if v <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", v)
}

func formatMem(used, total float64) string {
	if total <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f / %.1f GB", used, total)
}

func formatDisk(pct float64) string {
	if pct <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", pct)
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// Run starts the Bubble Tea program.
func Run(runner execx.Runner) error {
	p := tea.NewProgram(New(runner), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
