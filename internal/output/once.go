package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/ParthKadam11/WTFisRunning/internal/model"
)

// WriteJSON writes the runtime model as pretty JSON.
func WriteJSON(w io.Writer, rt *model.Runtime) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rt)
}

// WriteOnce writes a clean human-readable snapshot.
func WriteOnce(w io.Writer, rt *model.Runtime) {
	host := rt.System.Hostname
	if host == "" {
		host = "unknown"
	}
	fmt.Fprintf(w, "WTF IS RUNNING?\n")
	fmt.Fprintf(w, "runtime topology · %s\n\n", host)

	fmt.Fprintf(w, "SYSTEM\n")
	fmt.Fprintf(w, "  os        %s\n", dash(rt.System.OS))
	fmt.Fprintf(w, "  uptime    %s\n", dash(rt.System.Uptime))
	fmt.Fprintf(w, "  cpu       %s\n", formatCPU(rt.System.CPUPercent))
	fmt.Fprintf(w, "  memory    %s\n", formatMem(rt.System.MemUsedGB, rt.System.MemTotalGB))
	if len(rt.System.Disks) > 0 {
		fmt.Fprintf(w, "  disks\n")
		for _, d := range rt.System.Disks {
			fmt.Fprintf(w, "    %-18s %5.0f%%  %.1f / %.1f GB\n", d.Mount, d.Percent, d.UsedGB, d.TotalGB)
		}
	} else {
		fmt.Fprintf(w, "  disk      %s\n", formatDisk(rt.System.DiskPercent))
	}
	fmt.Fprintln(w)

	fmt.Fprintf(w, "COLLECTORS\n")
	order := []string{"docker", "ports", "nginx", "systemd", "tls", "system"}
	for _, name := range order {
		if c, ok := rt.Collectors[name]; ok {
			fmt.Fprintf(w, "  %-10s %s\n", name, collectorLine(c))
		}
	}
	fmt.Fprintln(w)

	if len(rt.FailedUnits) > 0 {
		fmt.Fprintf(w, "FAILED UNITS\n")
		for _, s := range rt.FailedUnits {
			fmt.Fprintf(w, "  ✕ %s\n", s.Name)
		}
		fmt.Fprintln(w)
	}

	if len(rt.ComposeProjects) > 0 {
		fmt.Fprintf(w, "COMPOSE\n")
		for _, p := range rt.ComposeProjects {
			fmt.Fprintf(w, "  %s  %d/%d running  [%s]\n",
				p.Name, p.Running, p.Total, strings.Join(p.Services, ", "))
		}
		fmt.Fprintln(w)
	}

	if len(rt.Services) > 0 {
		fmt.Fprintf(w, "SERVICES\n")
		for _, s := range rt.Services {
			exp := ""
			if s.Exposure != "" {
				exp = " [" + string(s.Exposure) + "]"
			}
			fmt.Fprintf(w, "  %s %-16s %-10s %s%s\n",
				statusGlyph(s.Status),
				s.Name,
				string(s.Status),
				formatPorts(s.Ports),
				exp,
			)
		}
		fmt.Fprintln(w)
	}

	topo := buildTopologyLines(rt)
	if len(topo) > 0 {
		fmt.Fprintf(w, "RUNTIME\n")
		for _, line := range topo {
			fmt.Fprintf(w, "  %s\n", line)
		}
		fmt.Fprintln(w)
	}

	if len(rt.TLSCerts) > 0 {
		fmt.Fprintf(w, "TLS\n")
		for _, c := range rt.TLSCerts {
			if !c.Accessible {
				fmt.Fprintf(w, "  :%-5d  unreachable\n", c.Port)
				continue
			}
			fmt.Fprintf(w, "  :%-5d  %s  expires %s\n", c.Port, dash(c.CN), dash(c.ExpiresIn))
		}
		fmt.Fprintln(w)
	}

	if len(rt.Ports) > 0 {
		fmt.Fprintf(w, "PORTS\n")
		seen := map[string]bool{}
		for _, p := range rt.Ports {
			key := fmt.Sprintf("%s:%d", p.Protocol, p.Port)
			if seen[key] {
				continue
			}
			seen[key] = true
			proc := p.Process
			if proc == "" {
				proc = "-"
			}
			owner := proc
			if p.User != "" {
				owner = p.User + "/" + proc
			}
			fmt.Fprintf(w, "  :%-5d  %-6s  %-8s  %s\n", p.Port, p.Protocol, string(p.Exposure), owner)
			if p.Cmdline != "" {
				fmt.Fprintf(w, "           %s\n", p.Cmdline)
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "%d services · %d ports · %d relationships\n",
		len(rt.Services), len(rt.Ports), countMeaningfulRelations(rt))
}

func collectorLine(c model.CollectorResult) string {
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
		return "permission denied"
	case model.CollectorTimeout:
		return "timeout"
	default:
		if c.Message != "" {
			return c.Message
		}
		return string(c.Status)
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

func formatPorts(ports []int) string {
	if len(ports) == 0 {
		return ""
	}
	parts := make([]string, len(ports))
	for i, p := range ports {
		parts[i] = fmt.Sprintf(":%d", p)
	}
	return strings.Join(parts, " ")
}

func formatCPU(v float64) string {
	if v <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", v)
}

func formatMem(used, total float64) string {
	if total <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f / %.1f GB", used, total)
}

func formatDisk(pct float64) string {
	if pct <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", pct)
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func countMeaningfulRelations(rt *model.Runtime) int {
	n := 0
	for _, r := range rt.Relations {
		if r.Type == model.RelProxiesTo || r.Type == model.RelDependsOn || r.Type == model.RelSameNetwork {
			n++
		}
	}
	return n
}

// TopologyLines returns tree lines for the RUNTIME section.
func TopologyLines(rt *model.Runtime) []string {
	return buildTopologyLines(rt)
}

func buildTopologyLines(rt *model.Runtime) []string {
	var lines []string

	// Compose projects first
	for _, p := range rt.ComposeProjects {
		lines = append(lines, "compose:"+p.Name)
		for i, m := range p.Containers {
			prefix := "├──"
			if i == len(p.Containers)-1 {
				prefix = "└──"
			}
			lines = append(lines, fmt.Sprintf("%s %s", prefix, m))
		}
		lines = append(lines, "")
	}

	var nginxRels []model.Relation
	for _, r := range rt.Relations {
		if r.Type == model.RelProxiesTo && (r.Source == "nginx" || strings.HasPrefix(r.Source, "nginx")) {
			nginxRels = append(nginxRels, r)
		}
	}
	if len(nginxRels) > 0 {
		lines = append(lines, "nginx")
		for i, r := range nginxRels {
			prefix := "├──"
			if i == len(nginxRels)-1 {
				prefix = "└──"
			}
			label := r.Label
			if label == "" {
				label = r.Destination
			}
			lines = append(lines, fmt.Sprintf("%s %s ──► %s", prefix, label, r.Destination))
		}
		lines = append(lines, "")
	}

	netMembers := map[string][]string{}
	for _, n := range rt.Networks {
		if n.Name == "bridge" || n.Name == "host" || n.Name == "none" {
			continue
		}
		if len(n.Members) < 2 {
			continue
		}
		// skip if already shown via compose
		netMembers[n.Name] = n.Members
	}
	for name, members := range netMembers {
		lines = append(lines, name)
		for i, m := range members {
			prefix := "├──"
			if i == len(members)-1 {
				prefix = "└──"
			}
			lines = append(lines, fmt.Sprintf("%s %s", prefix, m))
		}
		lines = append(lines, "")
	}

	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
