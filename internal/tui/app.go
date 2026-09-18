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
	spinFrame   int

	view      viewState
	cursor    int
	filter    string
	filtering bool

	selectedID string

	logsLoading bool
	logsForID   string
	logsLines   []string
}

type tickMsg time.Time

type logsMsg struct {
	serviceID string
	lines     []string
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
	return tea.Batch(m.discoverCmd(), tea.SetWindowTitle("wtfisrunning"), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) discoverCmd() tea.Cmd {
	seq := m.discoverSeq
	runner := m.runner
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		rt := discovery.Discover(ctx, runner)
		return discoverMsg{runtime: rt, seq: seq}
	}
}

func (m Model) fetchLogsCmd(svc model.Service) tea.Cmd {
	runner := m.runner
	id := svc.ID
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		lines := discovery.FetchLogs(ctx, runner, svc)
		return logsMsg{serviceID: id, lines: lines}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tickMsg:
		m.spinFrame++
		if m.loading || m.logsLoading {
			return m, tickCmd()
		}
		return m, nil

	case logsMsg:
		if msg.serviceID == m.selectedID {
			m.logsLoading = false
			m.logsForID = msg.serviceID
			m.logsLines = msg.lines
		}
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
			return m, tea.Batch(m.discoverCmd(), tickCmd())
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
					m.logsLines = nil
					m.logsForID = ""
					m.logsLoading = true
					return m, tea.Batch(m.fetchLogsCmd(items[m.cursor]), tickCmd())
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
	s := m.styles
	title := s.TitleWTF.Render("WTF") + " " + s.TitleRest.Render("IS RUNNING?")
	sub := s.Subtitle.Render("✦ runtime topology · " + host)
	bar := gradientRule(min(m.width-6, 42), s)
	return title + "\n" + sub + "\n" + bar
}

func (m Model) spinner() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return frames[m.spinFrame%len(frames)]
}

func (m Model) renderOverview() string {
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	if m.loading && m.runtime == nil {
		b.WriteString("\n")
		spin := m.styles.TitleWTF.Render(m.spinner())
		b.WriteString("  " + spin + " " + m.styles.Loading.Render("sniffing what's running…"))
		b.WriteString("\n\n")
		b.WriteString(m.renderCollectorsPlaceholder())
		b.WriteString("\n")
		b.WriteString(m.renderFooterHints([]string{"q:quit"}))
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
	if failed := m.renderFailed(); failed != "" {
		b.WriteString("\n")
		b.WriteString(failed)
	}
	if tls := m.renderTLS(); tls != "" {
		b.WriteString("\n")
		b.WriteString(tls)
	}
	b.WriteString("\n")
	b.WriteString(collectorsBlock)

	if m.filtering || m.filter != "" {
		b.WriteString("\n")
		prompt := m.styles.TitleWTF.Render("／")
		if m.filtering {
			prompt += m.styles.Primary.Render(m.filter) + m.styles.TitleWTF.Render("▌")
		} else {
			prompt = m.styles.Secondary.Render("filter: ") + m.styles.Primary.Render(m.filter)
		}
		b.WriteString(prompt)
	}

	if m.loading {
		b.WriteString("\n")
		b.WriteString(m.styles.TitleWTF.Render(m.spinner()) + " " + m.styles.Loading.Render("refreshing…"))
	}

	b.WriteString("\n")
	b.WriteString(m.renderStats())
	b.WriteString("\n")
	hints := []string{"↑↓:nav", "enter:inspect", "i:impact", "r:refresh", "/:filter", "q:quit"}
	if m.width < 70 {
		hints = []string{"↑↓", "enter", "r", "q"}
	}
	b.WriteString(m.renderFooterHints(hints))
	return b.String()
}

func (m Model) renderServices(width int) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Section.Render("◆ SERVICES"))
	b.WriteString("\n")
	ruleW := width
	if ruleW > 28 {
		ruleW = 28
	}
	b.WriteString(gradientRule(ruleW, s))
	b.WriteString("\n")

	items := m.visibleServices()
	if len(items) == 0 {
		msg := "No services discovered."
		if m.filter != "" {
			msg = "No matches."
		} else if m.runtime != nil {
			msg = emptyServicesHint(m.runtime)
		}
		b.WriteString(s.Muted.Render(msg))
		return b.String()
	}

	nameW := 14
	if width > 50 {
		nameW = 18
	}
	for i, svc := range items {
		glyph := s.statusStyle(svc.Status).Render(statusGlyph(svc.Status))
		badge := s.kindBadge(svc.Kind)
		name := padRight(svc.Name, nameW)
		ports := s.Port.Render(formatPorts(svc.Ports))
		exp := exposureTag(svc.Exposure, s)
		if i == m.cursor {
			cursor := s.Cursor.Render("▶")
			inner := fmt.Sprintf("%s %s %s  %s %s", statusGlyph(svc.Status), badge, padRight(svc.Name, nameW), formatPorts(svc.Ports), exposurePlain(svc.Exposure))
			b.WriteString(cursor + " " + s.Selected.Render(inner))
		} else {
			b.WriteString("  " + glyph + " " + badge + " " + s.Primary.Render(name) + "  " + ports)
			if exp != "" {
				b.WriteString("  " + exp)
			}
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func exposureTag(e model.Exposure, s styles) string {
	switch e {
	case model.ExposurePublic:
		return s.StatusFail.Render("public")
	case model.ExposureLocal:
		return s.Muted.Render("local")
	case model.ExposurePrivate:
		return s.Secondary.Render("lan")
	default:
		return ""
	}
}

func exposurePlain(e model.Exposure) string {
	switch e {
	case model.ExposurePublic:
		return "public"
	case model.ExposureLocal:
		return "local"
	case model.ExposurePrivate:
		return "lan"
	default:
		return ""
	}
}

func (m Model) renderSystem(width int) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Section.Render("◆ SYSTEM"))
	b.WriteString("\n")
	b.WriteString(gradientRule(22, s))
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
	}
	if m.width >= 88 && sys.OS != "" {
		rows = append([][2]string{{"os", truncate(sys.OS, 28)}}, rows...)
	}
	for _, row := range rows {
		b.WriteString(fmt.Sprintf("%s  %s\n",
			s.Secondary.Render(padRight(row[0], 8)),
			s.MetricVal.Render(row[1]),
		))
	}
	if len(sys.Disks) > 0 {
		b.WriteString(s.Secondary.Render("disks") + "\n")
		maxDisks := 4
		for i, d := range sys.Disks {
			if i >= maxDisks {
				b.WriteString(s.Muted.Render(fmt.Sprintf("  … %d more\n", len(sys.Disks)-maxDisks)))
				break
			}
			pctStyle := s.MetricVal
			if d.Percent >= 90 {
				pctStyle = s.StatusFail
			} else if d.Percent >= 80 {
				pctStyle = s.StatusWarn
			}
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				s.Primary.Render(padRight(truncate(d.Mount, 14), 14)),
				pctStyle.Render(fmt.Sprintf("%3.0f%%", d.Percent)),
			))
		}
	} else {
		b.WriteString(fmt.Sprintf("%s  %s\n",
			s.Secondary.Render(padRight("disk", 8)),
			s.MetricVal.Render(formatDisk(sys.DiskPercent)),
		))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderFailed() string {
	if m.runtime == nil || len(m.runtime.FailedUnits) == 0 {
		return ""
	}
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Section.Render("◆ FAILED"))
	b.WriteString("\n")
	b.WriteString(gradientRule(22, s))
	b.WriteString("\n")
	for i, u := range m.runtime.FailedUnits {
		if i >= 6 {
			b.WriteString(s.Muted.Render(fmt.Sprintf("  … %d more", len(m.runtime.FailedUnits)-6)))
			break
		}
		b.WriteString(s.StatusFail.Render("  ✕ "+u.Name) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderTLS() string {
	if m.runtime == nil || len(m.runtime.TLSCerts) == 0 {
		return ""
	}
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Section.Render("◆ TLS"))
	b.WriteString("\n")
	b.WriteString(gradientRule(22, s))
	b.WriteString("\n")
	for _, c := range m.runtime.TLSCerts {
		if !c.Accessible {
			b.WriteString(fmt.Sprintf("  %s  %s\n",
				s.Port.Render(fmt.Sprintf(":%d", c.Port)),
				s.Muted.Render("unreachable"),
			))
			continue
		}
		expStyle := s.MetricVal
		if c.ExpiresIn == "EXPIRED" || (len(c.ExpiresIn) > 0 && c.ExpiresIn[len(c.ExpiresIn)-1] == 'd') {
			// parse rough days
			var days int
			fmt.Sscanf(c.ExpiresIn, "%d", &days)
			if c.ExpiresIn == "EXPIRED" || days < 14 {
				expStyle = s.StatusFail
			} else if days < 30 {
				expStyle = s.StatusWarn
			}
		}
		cn := c.CN
		if cn == "" && len(c.SANs) > 0 {
			cn = c.SANs[0]
		}
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			s.Port.Render(fmt.Sprintf(":%d", c.Port)),
			s.Primary.Render(truncate(cn, 28)),
			expStyle.Render(c.ExpiresIn),
		))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m Model) renderTopology(width int) string {
	s := m.styles
	var b strings.Builder
	b.WriteString(s.Section.Render("◆ RUNTIME"))
	b.WriteString("\n")
	rw := width
	if rw > 50 {
		rw = 50
	}
	b.WriteString(gradientRule(rw, s))
	b.WriteString("\n")

	if m.runtime == nil {
		return b.String()
	}
	lines := output.TopologyLines(m.runtime)
	if len(lines) == 0 {
		b.WriteString(s.Muted.Render("No topology yet — relationships show up when nginx/compose/networks talk."))
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
		colored := colorTopologyLine(line, s)
		b.WriteString(colored)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func colorTopologyLine(line string, s styles) string {
	if line == "" {
		return ""
	}
	// Root node (no tree prefix)
	if !strings.HasPrefix(line, "├") && !strings.HasPrefix(line, "└") && !strings.HasPrefix(line, "│") {
		return s.TitleRest.Render(line)
	}
	if strings.Contains(line, "──►") {
		parts := strings.SplitN(line, "──►", 2)
		left := s.Tree.Render(parts[0]) + s.Arrow.Render("──►")
		right := ""
		if len(parts) > 1 {
			right = s.Port.Render(parts[1])
		}
		return left + right
	}
	return s.Tree.Render(line)
}

func (m Model) renderCollectors(width int) string {
	if m.runtime == nil {
		return ""
	}
	s := m.styles
	order := []string{"docker", "ports", "nginx", "systemd", "tls"}
	parts := make([]string, 0, len(order))
	for _, name := range order {
		c, ok := m.runtime.Collectors[name]
		if !ok {
			continue
		}
		label := name + " " + collectorShort(c)
		if c.Status == model.CollectorOK {
			parts = append(parts, s.ChipOK.Render("✓ "+label))
		} else {
			parts = append(parts, s.ChipBad.Render("✗ "+label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func (m Model) renderCollectorsPlaceholder() string {
	s := m.styles
	return "  " + s.ChipOK.Render("docker") + " " +
		s.ChipOK.Render("ports") + " " +
		s.ChipOK.Render("nginx") + " " +
		s.ChipOK.Render("systemd")
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
	left := s.Port.Render(fmt.Sprintf("%d", len(m.runtime.Services))) + s.Muted.Render(" services · ") +
		s.Port.Render(fmt.Sprintf("%d", len(m.runtime.Ports))) + s.Muted.Render(" ports · ") +
		s.Port.Render(fmt.Sprintf("%d", nRel)) + s.Muted.Render(" relationships")
	right := s.Secondary.Render("refreshed " + age)
	gap := m.width - 8 - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 2 {
		return left
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m Model) renderFooterHints(hints []string) string {
	s := m.styles
	parts := make([]string, 0, len(hints))
	for _, h := range hints {
		key, label, ok := strings.Cut(h, ":")
		if !ok {
			parts = append(parts, s.HelpKey.Render(h))
			continue
		}
		parts = append(parts, s.HelpKey.Render(key)+s.HelpLabel.Render(" "+label))
	}
	return strings.Join(parts, s.Muted.Render("  ·  "))
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
		b.WriteString(m.renderFooterHints([]string{"esc:back"}))
		return b.String()
	}

	b.WriteString(s.TitleWTF.Render(strings.ToUpper(svc.Name)))
	b.WriteString("  ")
	b.WriteString(s.kindBadge(svc.Kind))
	if svc.Exposure != "" {
		b.WriteString("  " + exposureTag(svc.Exposure, s))
	}
	b.WriteString("\n")
	b.WriteString(gradientRule(min(32, m.width-6), s))
	b.WriteString("\n\n")

	section := func(title, body string) {
		if body == "" {
			return
		}
		b.WriteString(s.Section.MarginTop(0).Render("◆ " + title))
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n\n")
	}

	stLine := s.statusStyle(svc.Status).Render(statusGlyph(svc.Status) + " " + string(svc.Status))
	section("STATUS", "  "+stLine)

	if svc.ComposeProj != "" {
		comp := s.Primary.Render(svc.ComposeProj)
		if svc.ComposeSvc != "" {
			comp += " / " + s.Port.Render(svc.ComposeSvc)
		}
		section("COMPOSE", "  "+comp)
	}
	if svc.ContainerID != "" {
		section("CONTAINER", "  "+s.Primary.Render(svc.Name)+"  "+s.Muted.Render(svc.ContainerID))
	}
	if svc.Image != "" {
		section("IMAGE", "  "+s.Primary.Render(svc.Image))
	}
	if svc.Unit != "" && (svc.Kind == "systemd" || svc.Kind == "nginx") {
		section("UNIT", "  "+s.Primary.Render(svc.Unit))
	}
	if svc.PID > 0 || svc.User != "" {
		var parts []string
		if svc.PID > 0 {
			parts = append(parts, s.MetricVal.Render(fmt.Sprintf("pid %d", svc.PID)))
		}
		if svc.User != "" {
			parts = append(parts, s.Primary.Render("user "+svc.User))
		}
		section("PROCESS", "  "+strings.Join(parts, "  ·  "))
	}
	if svc.Cmdline != "" {
		section("CMDLINE", "  "+s.Muted.Render(truncate(svc.Cmdline, m.width-10)))
	}
	if len(svc.Ports) > 0 {
		var lines []string
		portDetails := m.runtime.PortsForService(svc)
		if len(portDetails) > 0 {
			seen := map[int]bool{}
			for _, p := range portDetails {
				if seen[p.Port] {
					continue
				}
				seen[p.Port] = true
				line := "  " + s.Port.Render(fmt.Sprintf(":%d", p.Port)) +
					"  " + exposureTag(p.Exposure, s) +
					"  " + s.Muted.Render(p.Address)
				if p.User != "" {
					line += "  " + s.Secondary.Render(p.User)
				}
				lines = append(lines, line)
			}
		} else if svc.ContainerID != "" {
			for _, c := range m.runtime.Containers {
				if c.Name == svc.Name {
					for _, pm := range c.Ports {
						if pm.HostPort > 0 {
							lines = append(lines, "  "+s.Port.Render(fmt.Sprintf("%d", pm.HostPort))+
								s.Arrow.Render(" → ")+
								s.Primary.Render(fmt.Sprintf("%d/%s", pm.ContainerPort, pm.Protocol)))
						} else {
							lines = append(lines, "  "+s.Port.Render(fmt.Sprintf("%d/%s", pm.ContainerPort, pm.Protocol)))
						}
					}
					break
				}
			}
		}
		if len(lines) == 0 {
			for _, p := range svc.Ports {
				lines = append(lines, "  "+s.Port.Render(fmt.Sprintf(":%d", p)))
			}
		}
		section("PORTS", strings.Join(lines, "\n"))
	}
	if len(svc.Networks) > 0 {
		section("NETWORKS", "  "+s.Primary.Render(strings.Join(svc.Networks, ", ")))
	}

	var tlsLines []string
	for _, c := range m.runtime.TLSCerts {
		for _, p := range svc.Ports {
			if c.Port == p && c.Accessible {
				tlsLines = append(tlsLines, fmt.Sprintf("  %s  %s  %s",
					s.Port.Render(fmt.Sprintf(":%d", c.Port)),
					s.Primary.Render(c.CN),
					s.MetricVal.Render(c.ExpiresIn),
				))
			}
		}
	}
	if len(tlsLines) > 0 {
		section("TLS", strings.Join(tlsLines, "\n"))
	}

	deps := m.outgoingFor(svc)
	if len(deps) > 0 {
		var lines []string
		for _, r := range deps {
			label := r.Destination
			if r.Label != "" && r.Type == model.RelSameNetwork {
				label = r.Destination + "  " + s.Muted.Render("("+r.Label+")")
			}
			lines = append(lines, "  "+s.Arrow.Render("→ ")+s.Primary.Render(label))
		}
		section("RELATED", strings.Join(lines, "\n"))
	}

	exposed := m.incomingFor(svc)
	if len(exposed) > 0 {
		var lines []string
		for _, r := range exposed {
			line := "  " + s.Arrow.Render("→ ") + s.Primary.Render(r.Source)
			if r.Label != "" {
				line += "\n      " + s.Muted.Render(r.Label)
			}
			lines = append(lines, line)
		}
		section("EXPOSED THROUGH", strings.Join(lines, "\n"))
	}

	if m.logsLoading {
		section("LOGS", "  "+s.TitleWTF.Render(m.spinner())+" "+s.Loading.Render("loading…"))
	} else if len(m.logsLines) > 0 {
		var lines []string
		max := 12
		if m.height < 30 {
			max = 6
		}
		for i, line := range m.logsLines {
			if i >= max {
				lines = append(lines, s.Muted.Render(fmt.Sprintf("  … %d more", len(m.logsLines)-max)))
				break
			}
			lines = append(lines, "  "+s.Muted.Render(truncate(line, m.width-10)))
		}
		section("LOGS", strings.Join(lines, "\n"))
	}

	hints := []string{"esc:back", "r:refresh", "q:quit"}
	if m.hasImpact(svc.ID) {
		hints = []string{"esc:back", "i:impact", "r:refresh", "q:quit"}
	}
	b.WriteString(m.renderFooterHints(hints))
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

	b.WriteString(s.TitleWTF.Render("IMPACT") + s.TitleRest.Render(" · "+svc.Name))
	b.WriteString("\n")
	b.WriteString(gradientRule(min(36, m.width-6), s))
	b.WriteString("\n\n")
	ports := formatPorts(svc.Ports)
	b.WriteString(s.Primary.Render(svc.Name))
	if ports != "" {
		b.WriteString("  " + s.Port.Render(ports))
	}
	b.WriteString("\n\n")

	usedBy := m.incomingFor(svc)
	related := m.networkPeers(svc)

	if len(usedBy) > 0 {
		b.WriteString(s.Section.MarginTop(0).Render("◆ USED BY"))
		b.WriteString("\n")
		for _, r := range usedBy {
			line := "  " + s.Arrow.Render("→ ") + s.Primary.Render(r.Source)
			if r.Label != "" {
				line += s.Muted.Render("  (" + r.Label + ")")
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	if len(related) > 0 {
		b.WriteString(s.Section.MarginTop(0).Render("◆ SAME NETWORK"))
		b.WriteString("\n")
		for _, name := range related {
			b.WriteString("  " + s.Arrow.Render("→ ") + s.Primary.Render(name) + "\n")
		}
		b.WriteString("\n")
	}

	if len(usedBy) == 0 && len(related) == 0 {
		b.WriteString(s.Muted.Render("No discovered dependents."))
		b.WriteString("\n\n")
	} else {
		n := len(usedBy) + len(related)
		b.WriteString(s.MetricVal.Render(fmt.Sprintf("%d", n)) + s.Muted.Render(" services potentially affected"))
		b.WriteString("\n\n")
	}

	b.WriteString(m.renderFooterHints([]string{"esc:back", "q:quit"}))
	return b.String()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

func emptyServicesHint(rt *model.Runtime) string {
	var parts []string
	if c, ok := rt.Collectors["docker"]; ok && c.Status != model.CollectorOK {
		parts = append(parts, "docker: "+c.Message)
	}
	if c, ok := rt.Collectors["ports"]; ok && c.Status != model.CollectorOK {
		parts = append(parts, "ports: "+c.Message)
	}
	if c, ok := rt.Collectors["systemd"]; ok && c.Status != model.CollectorOK {
		parts = append(parts, "systemd: "+c.Message)
	}
	if len(parts) == 0 {
		return "No services discovered."
	}
	return "No services yet — " + strings.Join(parts, " · ")
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
