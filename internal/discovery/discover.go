package discovery

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wtfisrunning/wtfisrunning/internal/discovery/docker"
	"github.com/wtfisrunning/wtfisrunning/internal/discovery/nginx"
	"github.com/wtfisrunning/wtfisrunning/internal/discovery/ports"
	"github.com/wtfisrunning/wtfisrunning/internal/discovery/system"
	"github.com/wtfisrunning/wtfisrunning/internal/discovery/systemd"
	execx "github.com/wtfisrunning/wtfisrunning/internal/exec"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

// Discover runs all collectors concurrently and assembles a Runtime snapshot.
func Discover(ctx context.Context, runner execx.Runner) *model.Runtime {
	rt := model.EmptyRuntime()
	rt.DiscoveredAt = time.Now().UTC()
	rt.Host = runner.Host()

	var (
		mu          sync.Mutex
		containers  []model.Container
		networks    []model.Network
		portList    []model.Port
		procs       []model.Process
		sysInfo     model.SystemInfo
		nginxResult nginx.Result
		systemdSvcs []model.Service
	)

	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}

	run(func() {
		info, res := system.Collect(ctx, runner)
		mu.Lock()
		sysInfo = info
		rt.Collectors["system"] = res
		mu.Unlock()
	})

	run(func() {
		c, n, res := docker.Collect(ctx, runner)
		mu.Lock()
		containers = c
		networks = n
		rt.Collectors["docker"] = res
		mu.Unlock()
	})

	run(func() {
		p, pr, res := ports.Collect(ctx, runner)
		mu.Lock()
		portList = p
		procs = pr
		rt.Collectors["ports"] = res
		mu.Unlock()
	})

	run(func() {
		nr, res := nginx.Collect(ctx, runner)
		mu.Lock()
		nginxResult = nr
		rt.Collectors["nginx"] = res
		mu.Unlock()
	})

	wg.Wait()

	// systemd after we know container/process names for hints
	hints := make([]string, 0, len(containers)+4)
	for _, c := range containers {
		hints = append(hints, c.Name)
	}
	if nginxResult.Installed {
		hints = append(hints, "nginx")
	}
	svcs, res := systemd.Collect(ctx, runner, hints)
	systemdSvcs = svcs
	rt.Collectors["systemd"] = res

	rt.System = sysInfo
	rt.Containers = containers
	rt.Networks = networks
	rt.Ports = portList
	rt.Processes = procs

	rt.Services = buildServices(containers, systemdSvcs, nginxResult, portList)
	rt.Relations = buildRelations(rt, nginxResult, networks)

	// Normalize nil slices for stable JSON
	if rt.Services == nil {
		rt.Services = []model.Service{}
	}
	if rt.Containers == nil {
		rt.Containers = []model.Container{}
	}
	if rt.Processes == nil {
		rt.Processes = []model.Process{}
	}
	if rt.Ports == nil {
		rt.Ports = []model.Port{}
	}
	if rt.Networks == nil {
		rt.Networks = []model.Network{}
	}
	if rt.Relations == nil {
		rt.Relations = []model.Relation{}
	}

	sort.Slice(rt.Services, func(i, j int) bool {
		return rankService(rt.Services[i]) < rankService(rt.Services[j]) ||
			(rankService(rt.Services[i]) == rankService(rt.Services[j]) && rt.Services[i].Name < rt.Services[j].Name)
	})

	return rt
}

func buildServices(containers []model.Container, systemdSvcs []model.Service, ngx nginx.Result, portList []model.Port) []model.Service {
	var services []model.Service
	seen := map[string]bool{}
	portsClaimed := map[int]bool{}

	// Prefer containers as primary services
	for _, c := range containers {
		if c.Status == model.StatusStopped {
			continue
		}
		ports := make([]int, 0, len(c.Ports))
		for _, p := range c.Ports {
			if p.HostPort > 0 {
				ports = append(ports, p.HostPort)
			} else if p.ContainerPort > 0 {
				ports = append(ports, p.ContainerPort)
			}
		}
		ports = uniqueInts(ports)
		for _, p := range ports {
			portsClaimed[p] = true
		}
		svc := model.Service{
			ID:          "container:" + c.Name,
			Name:        c.Name,
			Kind:        "container",
			Status:      c.Status,
			StatusText:  string(c.Status),
			Ports:       ports,
			ContainerID: c.ID,
			Image:       c.Image,
			Networks:    c.Networks,
		}
		services = append(services, svc)
		seen[strings.ToLower(c.Name)] = true
	}

	if ngx.Installed {
		nginxPorts := portsForProcess(portList, "nginx")
		st := model.StatusStopped
		stText := "stopped"
		if ngx.Running {
			st = model.StatusRunning
			stText = "running"
		}
		if !seen["nginx"] {
			for _, p := range nginxPorts {
				portsClaimed[p] = true
			}
			services = append(services, model.Service{
				ID:         "nginx",
				Name:       "nginx",
				Kind:       "nginx",
				Status:     st,
				StatusText: stText,
				Ports:      nginxPorts,
				Unit:       "nginx.service",
			})
			seen["nginx"] = true
		}
	}

	for _, s := range systemdSvcs {
		key := strings.ToLower(s.Name)
		if seen[key] {
			continue
		}
		// Skip docker/containerd noise if we already have containers
		if (key == "docker" || key == "containerd") && len(containers) > 0 {
			continue
		}
		s.Ports = portsForProcess(portList, s.Name)
		for _, p := range s.Ports {
			portsClaimed[p] = true
		}
		services = append(services, s)
		seen[key] = true
	}

	// Named listening processes
	for _, p := range portList {
		if p.Process == "" || p.Protocol != "tcp" {
			continue
		}
		key := strings.ToLower(p.Process)
		if seen[key] {
			for i := range services {
				if strings.EqualFold(services[i].Name, p.Process) {
					services[i].Ports = uniqueInts(append(services[i].Ports, p.Port))
					portsClaimed[p.Port] = true
				}
			}
			continue
		}
		if !isAppProcess(p.Process) {
			continue
		}
		services = append(services, model.Service{
			ID:         "process:" + p.Process,
			Name:       p.Process,
			Kind:       "process",
			Status:     model.StatusRunning,
			StatusText: "running",
			Ports:      []int{p.Port},
			PID:        p.PID,
		})
		seen[key] = true
		portsClaimed[p.Port] = true
	}

	// Unnamed TCP listeners still matter (common without root / ss -p).
	// Surface them so the UI is never empty when something is clearly listening.
	portOwners := map[int]string{}
	for _, p := range portList {
		if p.Protocol != "tcp" {
			continue
		}
		if portsClaimed[p.Port] || isNoisePort(p.Port) {
			continue
		}
		name := fmt.Sprintf(":%d", p.Port)
		if existing, ok := portOwners[p.Port]; ok {
			_ = existing
			continue
		}
		display := name
		if p.Process != "" {
			display = p.Process
		}
		portOwners[p.Port] = display
		id := "port:" + strconv.Itoa(p.Port)
		if seen[strings.ToLower(display)] {
			continue
		}
		services = append(services, model.Service{
			ID:         id,
			Name:       display,
			Kind:       "port",
			Status:     model.StatusRunning,
			StatusText: "listening",
			Ports:      []int{p.Port},
			PID:        p.PID,
			Detail:     p.Address,
		})
		seen[strings.ToLower(display)] = true
		portsClaimed[p.Port] = true
	}

	return services
}

func isNoisePort(port int) bool {
	switch port {
	case 53, 323, 111, 631, 5353, 5355:
		return true
	default:
		return false
	}
}

func buildRelations(rt *model.Runtime, ngx nginx.Result, networks []model.Network) []model.Relation {
	var rels []model.Relation
	seen := map[string]bool{}
	add := func(r model.Relation) {
		key := string(r.Type) + "|" + r.Source + "|" + r.Destination + "|" + r.Label
		if seen[key] {
			return
		}
		seen[key] = true
		rels = append(rels, r)
	}

	// nginx proxy relations — map destinations to service names when possible
	for _, r := range ngx.Relations {
		dest := resolveDestination(rt, r.Destination)
		r.Destination = dest
		add(r)
	}

	// Compose depends_on from labels
	for _, c := range rt.Containers {
		if c.Status != model.StatusRunning {
			continue
		}
		raw := c.Labels["com.docker.compose.depends_on"]
		if raw == "" {
			continue
		}
		for _, dep := range parseComposeDepends(raw) {
			// Resolve compose service name to container name when possible
			dest := resolveComposeService(rt, c.Labels["com.docker.compose.project"], dep)
			add(model.Relation{
				Source:      c.Name,
				Destination: dest,
				Type:        model.RelDependsOn,
				Evidence:    "compose depends_on",
			})
		}
	}

	// listens_on from services
	for _, svc := range rt.Services {
		for _, p := range svc.Ports {
			add(model.Relation{
				Source:      svc.Name,
				Destination: ":" + strconv.Itoa(p),
				Type:        model.RelListensOn,
				Evidence:    "port scan",
			})
		}
	}

	// same_network relationships between containers (only non-default networks with 2+ members)
	for _, n := range networks {
		if n.Name == "bridge" || n.Name == "host" || n.Name == "none" {
			continue
		}
		if len(n.Members) < 2 {
			continue
		}
		members := append([]string{}, n.Members...)
		sort.Strings(members)
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				add(model.Relation{
					Source:      members[i],
					Destination: members[j],
					Type:        model.RelSameNetwork,
					Label:       n.Name,
					Evidence:    "docker network",
				})
			}
		}
	}

	return rels
}

// parseComposeDepends parses labels like "db:service_started:false,redis:service_started:false"
// or older "db,redis".
func parseComposeDepends(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name := part
		if i := strings.Index(part, ":"); i >= 0 {
			name = part[:i]
		}
		name = strings.TrimSpace(name)
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

func resolveComposeService(rt *model.Runtime, project, service string) string {
	for _, c := range rt.Containers {
		if c.Labels["com.docker.compose.service"] == service {
			if project == "" || c.Labels["com.docker.compose.project"] == project {
				return c.Name
			}
		}
	}
	return service
}

func resolveDestination(rt *model.Runtime, dest string) string {
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return dest
	}
	host := dest
	port := ""
	if i := strings.LastIndex(dest, ":"); i >= 0 {
		host = dest[:i]
		port = dest[i+1:]
	}
	// Map localhost/127.0.0.1 to a service listening on that port
	if host == "127.0.0.1" || host == "localhost" || host == "0.0.0.0" || host == "::1" {
		if p, err := strconv.Atoi(port); err == nil {
			for _, svc := range rt.Services {
				for _, sp := range svc.Ports {
					if sp == p {
						return fmt.Sprintf("%s:%d", svc.Name, p)
					}
				}
			}
			return ":" + port
		}
	}
	// Match container/service by name
	for _, svc := range rt.Services {
		if strings.EqualFold(svc.Name, host) {
			if port != "" {
				return svc.Name + ":" + port
			}
			return svc.Name
		}
	}
	return dest
}

func portsForProcess(ports []model.Port, name string) []int {
	var out []int
	lower := strings.ToLower(name)
	for _, p := range ports {
		if strings.EqualFold(p.Process, name) || strings.Contains(strings.ToLower(p.Process), lower) {
			out = append(out, p.Port)
		}
	}
	return uniqueInts(out)
}

func uniqueInts(in []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, v := range in {
		if v <= 0 || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Ints(out)
	return out
}

func isAppProcess(name string) bool {
	ignore := map[string]bool{
		"sshd": true, "systemd": true, "systemd-resolve": true, "systemd-network": true,
		"dbus-daemon": true, "cron": true, "master": true, // postfix
	}
	if ignore[strings.ToLower(name)] {
		return false
	}
	return true
}

func rankService(s model.Service) int {
	switch s.Kind {
	case "nginx":
		return 0
	case "container":
		return 1
	case "systemd":
		return 2
	default:
		return 3
	}
}
