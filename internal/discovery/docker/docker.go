package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	execx "github.com/ParthKadam11/WTFisRunning/internal/exec"
	"github.com/ParthKadam11/WTFisRunning/internal/model"
)

const collectorName = "docker"

// Collect discovers running Docker containers and networks.
func Collect(ctx context.Context, runner execx.Runner) ([]model.Container, []model.Network, model.CollectorResult) {
	ctx, cancel := execx.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	useSudo := false
	version := dockerCmd(ctx, runner, false, "version", "--format", "{{.Server.Version}}")
	if version.Err != nil && execx.DockerPermDenied(version) {
		// Passwordless sudo fallback (common on locked-down deploy users).
		if alt := dockerCmd(ctx, runner, true, "version", "--format", "{{.Server.Version}}"); alt.Err == nil {
			version = alt
			useSudo = true
		}
	}
	if version.Err != nil {
		kind := execx.ClassifyError(version)
		status := model.CollectorUnavailable
		msg := "docker unavailable"
		switch {
		case execx.DockerPermDenied(version) || kind == "permission_denied":
			status = model.CollectorPermissionDenied
			msg = "permission denied — run: sudo usermod -aG docker $USER"
		case kind == "not_installed":
			status = model.CollectorNotInstalled
			msg = "not installed"
		case kind == "unavailable":
			status = model.CollectorUnavailable
			msg = "daemon unavailable"
		case kind == "timeout":
			status = model.CollectorTimeout
			msg = "timed out"
		}
		return nil, nil, model.CollectorResult{Name: collectorName, Status: status, Message: msg}
	}

	ps := dockerCmd(ctx, runner, useSudo, "ps", "-a", "--format", "{{json .}}")
	if ps.Err != nil {
		return nil, nil, model.CollectorResult{
			Name:    collectorName,
			Status:  model.CollectorError,
			Message: strings.TrimSpace(ps.Stderr),
		}
	}

	containers, err := ParsePSJSON(ps.Stdout)
	if err != nil {
		return nil, nil, model.CollectorResult{
			Name:    collectorName,
			Status:  model.CollectorParseError,
			Message: err.Error(),
		}
	}

	// Enrich with network info via inspect (batch)
	if len(containers) > 0 {
		ids := make([]string, len(containers))
		for i, c := range containers {
			ids[i] = c.ID
		}
		inspectArgs := append([]string{"inspect"}, ids...)
		inspect := dockerCmd(ctx, runner, useSudo, inspectArgs...)
		if inspect.Err == nil {
			enrichFromInspect(containers, inspect.Stdout)
		}
	}

	networks := collectNetworks(ctx, runner, useSudo, containers)

	running := 0
	for _, c := range containers {
		if c.Status == model.StatusRunning {
			running++
		}
	}

	msg := fmt.Sprintf("%d containers (%d running)", len(containers), running)
	if useSudo {
		msg += " via sudo"
	}

	return containers, networks, model.CollectorResult{
		Name:    collectorName,
		Status:  model.CollectorOK,
		Count:   len(containers),
		Message: msg,
	}
}

func dockerCmd(ctx context.Context, runner execx.Runner, sudo bool, args ...string) execx.Result {
	if sudo {
		return runner.Run(ctx, "sudo", append([]string{"-n", "docker"}, args...)...)
	}
	return runner.Run(ctx, "docker", args...)
}

type psLine struct {
	ID       string `json:"ID"`
	Names    string `json:"Names"`
	Image    string `json:"Image"`
	Status   string `json:"Status"`
	State    string `json:"State"`
	Ports    string `json:"Ports"`
	Created  string `json:"CreatedAt"`
	Command  string `json:"Command"`
	Labels   string `json:"Labels"`
	Networks string `json:"Networks"`
}

// ParsePSJSON parses `docker ps --format {{json .}}` output.
func ParsePSJSON(output string) ([]model.Container, error) {
	var out []model.Container
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var row psLine
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			return nil, fmt.Errorf("parse docker ps line: %w", err)
		}
		name := row.Names
		if i := strings.Index(name, ","); i >= 0 {
			name = name[:i]
		}
		name = strings.TrimPrefix(name, "/")
		c := model.Container{
			ID:      shortID(row.ID),
			Name:    name,
			Image:   row.Image,
			State:   row.State,
			Status:  mapState(row.State, row.Status),
			Ports:   ParsePorts(row.Ports),
			Created: row.Created,
			Command: strings.Trim(row.Command, "\""),
			Labels:  parseLabels(row.Labels),
		}
		if row.Networks != "" {
			for _, n := range strings.Split(row.Networks, ",") {
				n = strings.TrimSpace(n)
				if n != "" {
					c.Networks = append(c.Networks, n)
				}
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// ParsePorts parses docker ps Ports field like "0.0.0.0:8080->80/tcp, :::8080->80/tcp".
func ParsePorts(s string) []model.PortMapping {
	if s == "" || s == "null" {
		return nil
	}
	var out []model.PortMapping
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		// 0.0.0.0:8080->80/tcp  or  80/tcp
		proto := "tcp"
		if i := strings.LastIndex(part, "/"); i >= 0 {
			proto = part[i+1:]
			part = part[:i]
		}
		if strings.Contains(part, "->") {
			sides := strings.SplitN(part, "->", 2)
			hostSide := sides[0]
			ctrSide := sides[1]
			hostIP := ""
			hostPort := 0
			if i := strings.LastIndex(hostSide, ":"); i >= 0 {
				hostIP = hostSide[:i]
				hostPort, _ = strconv.Atoi(hostSide[i+1:])
			} else {
				hostPort, _ = strconv.Atoi(hostSide)
			}
			ctrPort, _ := strconv.Atoi(ctrSide)
			out = append(out, model.PortMapping{
				HostIP:        hostIP,
				HostPort:      hostPort,
				ContainerPort: ctrPort,
				Protocol:      proto,
			})
		} else {
			ctrPort, _ := strconv.Atoi(part)
			if ctrPort > 0 {
				out = append(out, model.PortMapping{
					ContainerPort: ctrPort,
					Protocol:      proto,
				})
			}
		}
	}
	return dedupePorts(out)
}

func collectNetworks(ctx context.Context, runner execx.Runner, useSudo bool, containers []model.Container) []model.Network {
	res := dockerCmd(ctx, runner, useSudo, "network", "ls", "--format", "{{.ID}}\t{{.Name}}\t{{.Driver}}")
	if res.Err != nil {
		// Derive from containers
		seen := map[string]*model.Network{}
		for _, c := range containers {
			for _, n := range c.Networks {
				if _, ok := seen[n]; !ok {
					seen[n] = &model.Network{Name: n}
				}
				seen[n].Members = append(seen[n].Members, c.Name)
			}
		}
		var out []model.Network
		for _, n := range seen {
			out = append(out, *n)
		}
		return out
	}
	memberMap := map[string][]string{}
	for _, c := range containers {
		for _, n := range c.Networks {
			memberMap[n] = append(memberMap[n], c.Name)
		}
	}
	var out []model.Network
	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 2 {
			continue
		}
		n := model.Network{
			ID:      shortID(fields[0]),
			Name:    fields[1],
			Members: memberMap[fields[1]],
		}
		if len(fields) >= 3 {
			n.Driver = fields[2]
		}
		// Skip default bridge noise members unless used
		out = append(out, n)
	}
	return out
}

func enrichFromInspect(containers []model.Container, raw string) {
	var infos []struct {
		Id    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		NetworkSettings struct {
			Networks map[string]json.RawMessage `json:"Networks"`
			Ports    map[string][]struct {
				HostIP   string `json:"HostIp"`
				HostPort string `json:"HostPort"`
			} `json:"Ports"`
		} `json:"NetworkSettings"`
	}
	if err := json.Unmarshal([]byte(raw), &infos); err != nil {
		return
	}
	byID := map[string]*model.Container{}
	for i := range containers {
		byID[containers[i].ID] = &containers[i]
		byID[containers[i].Name] = &containers[i]
	}
	for _, info := range infos {
		id := shortID(info.Id)
		c := byID[id]
		if c == nil {
			name := strings.TrimPrefix(info.Name, "/")
			c = byID[name]
		}
		if c == nil {
			continue
		}
		var nets []string
		for n := range info.NetworkSettings.Networks {
			nets = append(nets, n)
		}
		if len(nets) > 0 {
			c.Networks = nets
		}
		if len(info.Config.Labels) > 0 {
			c.Labels = info.Config.Labels
		}
		if len(c.Ports) == 0 && len(info.NetworkSettings.Ports) > 0 {
			c.Ports = portsFromInspect(info.NetworkSettings.Ports)
		}
	}
}

func portsFromInspect(ports map[string][]struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}) []model.PortMapping {
	var out []model.PortMapping
	for key, binds := range ports {
		// "80/tcp"
		parts := strings.SplitN(key, "/", 2)
		ctrPort, _ := strconv.Atoi(parts[0])
		proto := "tcp"
		if len(parts) > 1 {
			proto = parts[1]
		}
		if len(binds) == 0 {
			out = append(out, model.PortMapping{ContainerPort: ctrPort, Protocol: proto})
			continue
		}
		for _, b := range binds {
			hp, _ := strconv.Atoi(b.HostPort)
			out = append(out, model.PortMapping{
				HostIP:        b.HostIP,
				HostPort:      hp,
				ContainerPort: ctrPort,
				Protocol:      proto,
			})
		}
	}
	return dedupePorts(out)
}

func mapState(state, status string) model.Status {
	s := strings.ToLower(state)
	if s == "" {
		s = strings.ToLower(status)
	}
	switch {
	case strings.Contains(s, "running"):
		return model.StatusRunning
	case strings.Contains(s, "exited"), strings.Contains(s, "dead"), strings.Contains(s, "created"):
		return model.StatusStopped
	case strings.Contains(s, "paused"), strings.Contains(s, "restarting"):
		return model.StatusWarning
	default:
		return model.StatusUnknown
	}
}

func parseLabels(s string) map[string]string {
	if s == "" {
		return nil
	}
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			out[kv[0]] = kv[1]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func dedupePorts(in []model.PortMapping) []model.PortMapping {
	seen := map[string]bool{}
	var out []model.PortMapping
	for _, p := range in {
		key := fmt.Sprintf("%s:%d:%d:%s", p.HostIP, p.HostPort, p.ContainerPort, p.Protocol)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}
