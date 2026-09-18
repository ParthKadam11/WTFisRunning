package logs

import (
	"context"
	"strings"
	"time"

	execx "github.com/ParthKadam11/WTFisRunning/internal/exec"
	"github.com/ParthKadam11/WTFisRunning/internal/model"
)

// Fetch returns recent log lines for a service (docker or systemd).
func Fetch(ctx context.Context, runner execx.Runner, svc model.Service) []string {
	ctx, cancel := execx.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	switch svc.Kind {
	case "container":
		name := svc.Name
		if name == "" {
			name = svc.ContainerID
		}
		res := runner.Run(ctx, "docker", "logs", "--tail", "20", name)
		if res.Err != nil && execx.DockerPermDenied(res) {
			res = runner.Run(ctx, "sudo", "-n", "docker", "logs", "--tail", "20", name)
		}
		if res.Err != nil {
			return []string{"(logs unavailable: " + trimErr(res) + ")"}
		}
		return trimLines(res.Stdout+res.Stderr, 20)

	case "systemd", "nginx":
		unit := svc.Unit
		if unit == "" {
			unit = svc.Name + ".service"
		}
		res := runner.Run(ctx, "journalctl", "-u", unit, "-n", "20", "--no-pager", "-o", "cat")
		if res.Err != nil {
			return []string{"(logs unavailable: " + trimErr(res) + ")"}
		}
		lines := trimLines(res.Stdout, 20)
		if len(lines) == 0 {
			return []string{"(no recent journal lines)"}
		}
		return lines

	default:
		if svc.Unit != "" {
			res := runner.Run(ctx, "journalctl", "-u", svc.Unit, "-n", "20", "--no-pager", "-o", "cat")
			if res.Err == nil {
				return trimLines(res.Stdout, 20)
			}
		}
		return nil
	}
}

func trimLines(s string, max int) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line) > 200 {
			line = line[:197] + "…"
		}
		out = append(out, line)
		if len(out) >= max {
			break
		}
	}
	return out
}

func trimErr(res execx.Result) string {
	msg := strings.TrimSpace(res.Stderr)
	if msg == "" && res.Err != nil {
		msg = res.Err.Error()
	}
	if len(msg) > 80 {
		msg = msg[:77] + "…"
	}
	if msg == "" {
		return "error"
	}
	return msg
}
