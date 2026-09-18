package system

import (
	"context"
	"math"
	"strconv"
	"strings"
	"time"

	execx "github.com/wtfisrunning/wtfisrunning/internal/exec"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

const collectorName = "system"

// Collect gathers basic host system information.
func Collect(ctx context.Context, runner execx.Runner) (model.SystemInfo, model.CollectorResult) {
	info := model.SystemInfo{}
	ctx, cancel := execx.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	info.Hostname = firstLine(runner.Run(ctx, "hostname").Stdout)
	if info.Hostname == "" {
		info.Hostname = "unknown"
	}

	os := detectOS(ctx, runner)
	info.OS = os
	info.Kernel = firstLine(runner.Run(ctx, "uname", "-r").Stdout)

	uptimeOut := runner.Run(ctx, "cat", "/proc/uptime")
	if uptimeOut.Err == nil {
		fields := strings.Fields(uptimeOut.Stdout)
		if len(fields) > 0 {
			if sec, err := strconv.ParseFloat(fields[0], 64); err == nil {
				info.UptimeSec = int64(sec)
				info.Uptime = formatUptime(int64(sec))
			}
		}
	}
	if info.Uptime == "" {
		u := runner.Run(ctx, "uptime", "-p")
		if u.Err == nil {
			info.Uptime = strings.TrimPrefix(strings.TrimSpace(u.Stdout), "up ")
		}
	}

	info.MemUsedGB, info.MemTotalGB = memInfo(ctx, runner)
	info.Disks = diskMounts(ctx, runner)
	if len(info.Disks) > 0 {
		// Prefer root mount for summary fields
		root := info.Disks[0]
		for _, d := range info.Disks {
			if d.Mount == "/" {
				root = d
				break
			}
		}
		info.DiskPercent = root.Percent
		info.DiskUsedGB = root.UsedGB
		info.DiskTotalGB = root.TotalGB
	} else {
		info.DiskPercent, info.DiskUsedGB, info.DiskTotalGB = diskInfo(ctx, runner)
	}
	info.CPUPercent = cpuPercent(ctx, runner)

	return info, model.CollectorResult{
		Name:    collectorName,
		Status:  model.CollectorOK,
		Message: info.Hostname,
	}
}

func detectOS(ctx context.Context, runner execx.Runner) string {
	res := runner.Run(ctx, "cat", "/etc/os-release")
	if res.Err == nil {
		var pretty, name, version string
		for _, line := range strings.Split(res.Stdout, "\n") {
			line = strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "PRETTY_NAME="):
				pretty = unquote(strings.TrimPrefix(line, "PRETTY_NAME="))
			case strings.HasPrefix(line, "NAME="):
				name = unquote(strings.TrimPrefix(line, "NAME="))
			case strings.HasPrefix(line, "VERSION_ID="):
				version = unquote(strings.TrimPrefix(line, "VERSION_ID="))
			}
		}
		if pretty != "" {
			return pretty
		}
		if name != "" {
			if version != "" {
				return name + " " + version
			}
			return name
		}
	}
	u := firstLine(runner.Run(ctx, "uname", "-s").Stdout)
	if u != "" {
		return u
	}
	return "unknown"
}

func memInfo(ctx context.Context, runner execx.Runner) (used, total float64) {
	res := runner.Run(ctx, "cat", "/proc/meminfo")
	if res.Err != nil {
		return 0, 0
	}
	var memTotal, memAvailable, memFree, buffers, cached float64
	hasAvailable := false
	for _, line := range strings.Split(res.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		val, _ := strconv.ParseFloat(fields[1], 64) // kB
		switch fields[0] {
		case "MemTotal:":
			memTotal = val
		case "MemAvailable:":
			memAvailable = val
			hasAvailable = true
		case "MemFree:":
			memFree = val
		case "Buffers:":
			buffers = val
		case "Cached:":
			cached = val
		}
	}
	if memTotal <= 0 {
		return 0, 0
	}
	if !hasAvailable {
		memAvailable = memFree + buffers + cached
	}
	total = round1(memTotal / 1024 / 1024)
	used = round1((memTotal - memAvailable) / 1024 / 1024)
	if used < 0 {
		used = 0
	}
	return used, total
}

func diskInfo(ctx context.Context, runner execx.Runner) (percent, usedGB, totalGB float64) {
	mounts := diskMounts(ctx, runner)
	for _, d := range mounts {
		if d.Mount == "/" {
			return d.Percent, d.UsedGB, d.TotalGB
		}
	}
	if len(mounts) > 0 {
		return mounts[0].Percent, mounts[0].UsedGB, mounts[0].TotalGB
	}
	return 0, 0, 0
}

// diskMounts returns interesting filesystem mounts (/, /var, /home, docker, etc.).
func diskMounts(ctx context.Context, runner execx.Runner) []model.DiskMount {
	res := runner.Run(ctx, "df", "-P", "-k")
	if res.Err != nil {
		return nil
	}
	return ParseDF(res.Stdout)
}

// ParseDF parses `df -P -k` output into interesting mounts.
func ParseDF(output string) []model.DiskMount {
	interesting := map[string]bool{
		"/": true, "/var": true, "/var/lib/docker": true, "/home": true,
		"/tmp": true, "/boot": true, "/opt": true, "/usr": true, "/data": true,
	}
	var out []model.DiskMount
	seen := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Filesystem") {
			continue
		}
		fields := strings.Fields(line)
		pctIdx := -1
		for i, f := range fields {
			if strings.HasSuffix(f, "%") {
				pctIdx = i
				break
			}
		}
		if pctIdx < 3 || pctIdx+1 >= len(fields) {
			continue
		}
		mount := fields[pctIdx+1]
		// join remaining mount path parts if any
		if pctIdx+2 < len(fields) {
			mount = strings.Join(fields[pctIdx+1:], " ")
		}
		device := fields[0]
		if pctIdx > 3 {
			device = strings.Join(fields[:pctIdx-3], " ")
		}
		// skip pseudo filesystems
		if strings.HasPrefix(device, "tmpfs") || strings.HasPrefix(device, "devtmpfs") ||
			strings.HasPrefix(device, "udev") || strings.HasPrefix(device, "overlay") ||
			device == "efivarfs" {
			continue
		}
		keep := interesting[mount]
		if !keep {
			for hint := range interesting {
				if hint != "/" && (strings.HasPrefix(mount, hint+"/") || mount == hint) {
					keep = true
					break
				}
			}
		}
		// also keep high-usage mounts (>= 80%) on real devices
		totalKB, err1 := strconv.ParseFloat(fields[pctIdx-3], 64)
		usedKB, err2 := strconv.ParseFloat(fields[pctIdx-2], 64)
		pct, err3 := strconv.ParseFloat(strings.TrimSuffix(fields[pctIdx], "%"), 64)
		if err1 != nil || err2 != nil || totalKB <= 0 {
			continue
		}
		if err3 != nil || pct < 0 || pct > 100 {
			pct = round1((usedKB / totalKB) * 100)
		}
		if pct > 100 {
			pct = 100
		}
		if !keep && pct < 80 {
			continue
		}
		if seen[mount] {
			continue
		}
		seen[mount] = true
		out = append(out, model.DiskMount{
			Mount:   mount,
			Device:  device,
			Percent: pct,
			UsedGB:  round1(usedKB / 1024 / 1024),
			TotalGB: round1(totalKB / 1024 / 1024),
		})
	}
	// Ensure / is first when present
	sortDisks(out)
	return out
}

func sortDisks(disks []model.DiskMount) {
	for i := range disks {
		if disks[i].Mount == "/" && i != 0 {
			disks[0], disks[i] = disks[i], disks[0]
			return
		}
	}
}

func cpuPercent(ctx context.Context, runner execx.Runner) float64 {
	a := readCPU(ctx, runner)
	time.Sleep(200 * time.Millisecond)
	b := readCPU(ctx, runner)
	if a.total == 0 || b.total == 0 {
		return 0
	}
	idleDelta := b.idle - a.idle
	totalDelta := b.total - a.total
	if totalDelta <= 0 {
		return 0
	}
	usage := (1.0 - float64(idleDelta)/float64(totalDelta)) * 100
	if usage < 0 {
		return 0
	}
	return round1(usage)
}

type cpuSample struct {
	idle  uint64
	total uint64
}

func readCPU(ctx context.Context, runner execx.Runner) cpuSample {
	res := runner.Run(ctx, "cat", "/proc/stat")
	if res.Err != nil {
		return cpuSample{}
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			return cpuSample{}
		}
		var total, idle uint64
		for i := 1; i < len(fields); i++ {
			v, _ := strconv.ParseUint(fields[i], 10, 64)
			total += v
			if i == 4 || i == 5 { // idle + iowait
				idle += v
			}
		}
		return cpuSample{idle: idle, total: total}
	}
	return cpuSample{}
}

func formatUptime(sec int64) string {
	if sec < 60 {
		return strconv.FormatInt(sec, 10) + "s"
	}
	days := sec / 86400
	sec %= 86400
	hours := sec / 3600
	sec %= 3600
	mins := sec / 60
	var parts []string
	if days > 0 {
		parts = append(parts, strconv.FormatInt(days, 10)+"d")
	}
	if hours > 0 {
		parts = append(parts, strconv.FormatInt(hours, 10)+"h")
	}
	if mins > 0 && days == 0 {
		parts = append(parts, strconv.FormatInt(mins, 10)+"m")
	}
	if len(parts) == 0 {
		return "0m"
	}
	return strings.Join(parts, " ")
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && ((s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'')) {
		return s[1 : len(s)-1]
	}
	return s
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}
