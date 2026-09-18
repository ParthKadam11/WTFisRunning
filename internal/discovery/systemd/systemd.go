package systemd

import (
	"context"
	"strings"
	"time"

	execx "github.com/wtfisrunning/wtfisrunning/internal/exec"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

const collectorName = "systemd"

// boringPrefixes / exact names are filtered out so custom app units still show.
var boringExact = map[string]bool{
	"dbus": true, "dbus-broker": true, "cron": true, "crond": true,
	"rsyslog": true, "syslog": true, "systemd-journald": true,
	"systemd-logind": true, "systemd-networkd": true, "systemd-resolved": true,
	"systemd-timesyncd": true, "systemd-udevd": true, "systemd-userdbd": true,
	"polkit": true, "accounts-daemon": true, "ModemManager": true,
	"multipathd": true, "udisks2": true, "upower": true, "snapd": true,
	"unattended-upgrades": true, "packagekit": true, "chronyd": true,
	"chrony": true, "irqbalance": true, "NetworkManager": true,
	"blk-availability": true, "lvm2-monitor": true,
}

var boringPrefixes = []string{
	"systemd-", "user@", "getty@", "session-", "user-runtime-dir@",
	"dbus-", "snap.", "plymouth",
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

// ParseListUnits parses systemctl list-units and keeps non-boring / hinted services.
func ParseListUnits(output string, hintNames []string) []model.Service {
	hints := map[string]bool{}
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
		if isBoring(name, base) && !hints[strings.ToLower(base)] && !hints[strings.ToLower(name)] {
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

func isBoring(full, base string) bool {
	lowerFull := strings.ToLower(full)
	lowerBase := strings.ToLower(base)
	if boringExact[base] || boringExact[lowerBase] || boringExact[full] || boringExact[lowerFull] {
		return true
	}
	for _, p := range boringPrefixes {
		pl := strings.ToLower(p)
		if strings.HasPrefix(lowerFull, pl) || strings.HasPrefix(lowerBase, pl) {
			return true
		}
	}
	// user@UID.service
	if lowerBase == "user" && strings.Contains(lowerFull, "@") {
		return true
	}
	return false
}
