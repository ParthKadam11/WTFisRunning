package compose

import (
	"sort"
	"strings"

	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

// BuildProjects groups containers by docker compose project labels.
func BuildProjects(containers []model.Container) []model.ComposeProject {
	type agg struct {
		services map[string]bool
		names    []string
		running  int
		total    int
	}
	projects := map[string]*agg{}

	for _, c := range containers {
		proj := ""
		svc := ""
		if c.Labels != nil {
			proj = c.Labels["com.docker.compose.project"]
			svc = c.Labels["com.docker.compose.service"]
		}
		if proj == "" {
			continue
		}
		a := projects[proj]
		if a == nil {
			a = &agg{services: map[string]bool{}}
			projects[proj] = a
		}
		a.total++
		a.names = append(a.names, c.Name)
		if svc != "" {
			a.services[svc] = true
		}
		if c.Status == model.StatusRunning {
			a.running++
		}
	}

	var out []model.ComposeProject
	for name, a := range projects {
		svcs := make([]string, 0, len(a.services))
		for s := range a.services {
			svcs = append(svcs, s)
		}
		sort.Strings(svcs)
		sort.Strings(a.names)
		out = append(out, model.ComposeProject{
			Name:       name,
			Services:   svcs,
			Containers: a.names,
			Running:    a.running,
			Total:      a.total,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ServiceMeta returns compose project/service labels for a container name.
func ServiceMeta(containers []model.Container, name string) (project, service string) {
	for _, c := range containers {
		if c.Name != name {
			continue
		}
		if c.Labels == nil {
			return "", ""
		}
		return c.Labels["com.docker.compose.project"], c.Labels["com.docker.compose.service"]
	}
	return "", ""
}

// DependsOnList parses compose depends_on label values.
func DependsOnList(raw string) []string {
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
