package systemd

import (
	"context"
	"strings"
	"time"

	execx "github.com/wtfisrunning/wtfisrunning/internal/exec"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

const collectorName = "systemd"

// interestingHints prioritizes services related to app runtimes.
var interestingHints = []string{
	"nginx", "docker", "containerd", "postgres", "postgresql", "mysql", "mariadb",
	"redis", "mongo", "rabbitmq", "caddy", "traefik", "haproxy", "apache", "httpd",
	"node", "php", "gunicorn", "uwsgi", "supervisor", "pm2", "fail2ban",
	"sshd", "ssh", "cron", "nftables", "ufw", "firewalld",
}

// Collect discovers relevant active systemd services.
func Collect(ctx context.Context, runner execx.Runner, hintNames []string) ([]model.Service, model.CollectorResult) {
	ctx, cancel := execx.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	check := runner.Run(ctx, "systemctl", "is-system-running")
	if check.Err != nil && execx.ClassifyError(check) == "not_installed" {
		return nil, model.CollectorResult{
			Name:    collectorName,
			Status:  model.CollectorNotInstalled,
			Message: "not available",
		}
	}
	// Even if degraded/offline, systemctl may work
	list := runner.Run(ctx, "systemctl", "list-units", "--type=service", "--state=running", "--no-pager", "--no-legend", "--plain")
	if list.Err != nil {
		kind := execx.ClassifyError(list)
		status := model.CollectorUnavailable
		msg := "unavailable"
		switch kind {
		case "not_installed":
			status = model.CollectorNotInstalled
			msg = "not available"
		case "permission_denied":
			status = model.CollectorPermissionDenied
			msg = "permission denied"
		case "timeout":
			status = model.CollectorTimeout
			msg = "timed out"
		}
		return nil, model.CollectorResult{Name: collectorName, Status: status, Message: msg}
	}

	services := ParseListUnits(list.Stdout, hintNames)
	return services, model.CollectorResult{
		Name:    collectorName,
		Status:  model.CollectorOK,
		Count:   len(services),
		Message: "available",
	}
}

// ParseListUnits parses systemctl list-units output and filters to relevant services.
func ParseListUnits(output string, hintNames []string) []model.Service {
	hints := map[string]bool{}
	for _, h := range interestingHints {
		hints[h] = true
	}
	for _, h := range hintNames {
		h = strings.ToLower(strings.TrimSpace(h))
		if h != "" {
			hints[h] = true
		}
	}

	var out []model.Service
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		unit := fields[0]
		if !strings.HasSuffix(unit, ".service") {
			continue
		}
		name := strings.TrimSuffix(unit, ".service")
		base := name
		if i := strings.LastIndex(name, "@"); i >= 0 {
			base = name[:i]
		}
		if !isInteresting(base, hints) {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, model.Service{
			ID:         "systemd:" + name,
			Name:       name,
			Kind:       "systemd",
			Status:     model.StatusRunning,
			StatusText: "running",
			Unit:       unit,
		})
	}
	return out
}

func isInteresting(name string, hints map[string]bool) bool {
	lower := strings.ToLower(name)
	if hints[lower] {
		return true
	}
	for h := range hints {
		if strings.Contains(lower, h) || strings.Contains(h, lower) {
			return true
		}
	}
	return false
}
