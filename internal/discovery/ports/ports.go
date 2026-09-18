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

// Collect discovers listening TCP/UDP ports via ss and enriches ownership.
func Collect(ctx context.Context, runner execx.Runner) ([]model.Port, []model.Process, model.CollectorResult) {
	ctx, cancel := execx.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	res := runner.Run(ctx, "ss", "-lntupH")
	if res.Err != nil {
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
	for i := range ports {
		ports[i].Exposure = ClassifyExposure(ports[i].Address)
	}
	EnrichOwnership(ctx, runner, ports, procs)

	public := 0
	for _, p := range ports {
		if p.Exposure == model.ExposurePublic && p.Protocol == "tcp" {
			public++
		}
	}
	msg := strconv.Itoa(len(ports)) + " listening"
	if public > 0 {
		msg += ", " + strconv.Itoa(public) + " public"
	}

	return ports, procs, model.CollectorResult{
		Name:    collectorName,
		Status:  model.CollectorOK,
		Count:   len(ports),
		Message: msg,
	}
}

// ClassifyExposure maps a bind address to public/local/private.
func ClassifyExposure(addr string) model.Exposure {
	a := strings.ToLower(strings.TrimSpace(addr))
	a = strings.TrimPrefix(a, "[")
	a = strings.TrimSuffix(a, "]")
	// strip zone id e.g. fe80::1%eth0 or 127.0.0.53%lo
	if i := strings.IndexByte(a, '%'); i >= 0 {
		a = a[:i]
	}
	switch a {
	case "", "*", "0.0.0.0", "::", "::0", "https://example.com/p/dynamo":
		return model.ExposurePublic
	case "127.0.0.1", "::1", "localhost":
		return model.ExposureLocal
	default:
		if strings.HasPrefix(a, "127.") {
			return model.ExposureLocal
		}
		return model.ExposurePrivate
	}
}

// EnrichOwnership fills user + cmdline from /proc when PID is known.
func EnrichOwnership(ctx context.Context, runner execx.Runner, ports []model.Port, procs []model.Process) {
	cache := map[int]struct{ user, cmd string }{}
	lookup := func(pid int) (string, string) {
		if pid <= 0 {
			return "", ""
		}
		if v, ok := cache[pid]; ok {
			return v.user, v.cmd
		}
		user, cmd := readProcMeta(ctx, runner, pid)
		cache[pid] = struct{ user, cmd string }{user, cmd}
		return user, cmd
	}

	for i := range ports {
		if ports[i].PID <= 0 {
			continue
		}
		u, c := lookup(ports[i].PID)
		ports[i].User = u
		ports[i].Cmdline = c
	}
	for i := range procs {
		u, c := lookup(procs[i].PID)
		procs[i].User = u
		procs[i].Command = c
	}
}

func readProcMeta(ctx context.Context, runner execx.Runner, pid int) (user, cmdline string) {
	pidStr := strconv.Itoa(pid)
	// cmdline: null-separated
	cmdRes := runner.Run(ctx, "cat", "/proc/"+pidStr+"/cmdline")
	if cmdRes.Err == nil {
		cmdline = strings.ReplaceAll(cmdRes.Stdout, "\x00", " ")
		cmdline = strings.TrimSpace(cmdline)
		if len(cmdline) > 160 {
			cmdline = cmdline[:157] + "…"
		}
	}
	// owner via stat -c %U (GNU) or id
	st := runner.Run(ctx, "stat", "-c", "%U", "/proc/"+pidStr)
	if st.Err == nil {
		user = strings.TrimSpace(st.Stdout)
	}
	if user == "" {
		// fallback: ps -o user= -p PID
		ps := runner.Run(ctx, "ps", "-o", "user=", "-p", pidStr)
		if ps.Err == nil {
			user = strings.TrimSpace(ps.Stdout)
		}
	}
	return user, cmdline
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
			Exposure: ClassifyExposure(addr),
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
	localIdx := 4
	if strings.EqualFold(fields[1], "LISTEN") || strings.EqualFold(fields[1], "UNCONN") ||
		strings.EqualFold(fields[1], "ESTAB") {
		localIdx = 4
	} else if isAddr(fields[3]) {
		localIdx = 3
	}
	if localIdx >= len(fields) {
		return "", "", ""
	}
	local = fields[localIdx]
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
