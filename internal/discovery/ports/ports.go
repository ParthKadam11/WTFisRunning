package ports

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	execx "github.com/wtfisrunning/wtfisrunning/internal/exec"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

const collectorName = "ports"

var (
	usersRe    = regexp.MustCompile(`users:\(\("([^"]+)",pid=(\d+)`)
	addrPortRe = regexp.MustCompile(`(?:\[)?([^\]:]+)(?:\])?:(\d+)$`)
)

// Collect discovers listening TCP/UDP ports via ss.
func Collect(ctx context.Context, runner execx.Runner) ([]model.Port, []model.Process, model.CollectorResult) {
	ctx, cancel := execx.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	res := runner.Run(ctx, "ss", "-lntupH")
	if res.Err != nil {
		// Fallback without -H (older ss)
		res = runner.Run(ctx, "ss", "-lntup")
	}
	if res.Err != nil {
		kind := execx.ClassifyError(res)
		status := model.CollectorUnavailable
		msg := "ss unavailable"
		switch kind {
		case "not_installed":
			status = model.CollectorNotInstalled
			msg = "ss not installed"
		case "permission_denied":
			status = model.CollectorPermissionDenied
			msg = "permission denied"
		case "timeout":
			status = model.CollectorTimeout
			msg = "timed out"
		}
		return nil, nil, model.CollectorResult{Name: collectorName, Status: status, Message: msg}
	}

	ports, procs := ParseSS(res.Stdout)
	return ports, procs, model.CollectorResult{
		Name:    collectorName,
		Status:  model.CollectorOK,
		Count:   len(ports),
		Message: strconv.Itoa(len(ports)) + " listening",
	}
}

// ParseSS parses `ss -lntup` output into ports and processes.
func ParseSS(output string) ([]model.Port, []model.Process) {
	var ports []model.Port
	seenProc := map[int]bool{}
	var procs []model.Process

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Netid") || strings.HasPrefix(line, "State") {
			continue
		}
		proto, local, processCol := splitSSLine(line)
		if proto == "" || local == "" {
			continue
		}
		addr, portNum := parseAddrPort(local)
		if portNum == 0 {
			continue
		}
		p := model.Port{
			Protocol: strings.ToLower(proto),
			Address:  addr,
			Port:     portNum,
		}
		name, pid := parseUsers(processCol)
		if name != "" {
			p.Process = name
			p.PID = pid
			p.Userspace = processCol
			if pid > 0 && !seenProc[pid] {
				seenProc[pid] = true
				procs = append(procs, model.Process{PID: pid, Name: name})
			}
		}
		ports = append(ports, p)
	}
	return ports, procs
}

func splitSSLine(line string) (proto, local, rest string) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return "", "", ""
	}
	proto = fields[0]
	// ss columns: Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port Process
	// With -H sometimes State is present.
	localIdx := 4
	if strings.EqualFold(fields[1], "LISTEN") || strings.EqualFold(fields[1], "UNCONN") ||
		strings.EqualFold(fields[1], "ESTAB") {
		localIdx = 4
	} else if isAddr(fields[3]) {
		// some variants without state when using -H differently
		localIdx = 3
	}
	if localIdx >= len(fields) {
		return "", "", ""
	}
	local = fields[localIdx]
	if localIdx+2 < len(fields) {
		rest = strings.Join(fields[localIdx+2:], " ")
	} else if localIdx+1 < len(fields) {
		// peer might be *,* then process
		rest = ""
		if localIdx+2 <= len(fields) {
			candidate := strings.Join(fields[localIdx+1:], " ")
			if strings.Contains(candidate, "users:(") {
				rest = candidate
			}
		}
	}
	// Find users:( anywhere in remaining
	if idx := strings.Index(line, "users:("); idx >= 0 {
		rest = line[idx:]
	}
	return proto, local, rest
}

func isAddr(s string) bool {
	return strings.Contains(s, ":") || strings.HasPrefix(s, "*") || strings.HasPrefix(s, "[")
}

func parseAddrPort(local string) (string, int) {
	local = strings.TrimSpace(local)
	m := addrPortRe.FindStringSubmatch(local)
	if m == nil {
		// *:80 or 0.0.0.0:80
		if i := strings.LastIndex(local, ":"); i >= 0 {
			addr := local[:i]
			port, err := strconv.Atoi(local[i+1:])
			if err != nil {
				return addr, 0
			}
			if addr == "*" {
				addr = "0.0.0.0"
			}
			return addr, port
		}
		return local, 0
	}
	addr := m[1]
	if addr == "*" {
		addr = "0.0.0.0"
	}
	port, _ := strconv.Atoi(m[2])
	return addr, port
}

func parseUsers(s string) (name string, pid int) {
	if s == "" {
		return "", 0
	}
	m := usersRe.FindStringSubmatch(s)
	if m == nil {
		return "", 0
	}
	pid, _ = strconv.Atoi(m[2])
	return m[1], pid
}
