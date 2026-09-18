package nginx

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	execx "github.com/ParthKadam11/WTFisRunning/internal/exec"
	"github.com/ParthKadam11/WTFisRunning/internal/model"
)

const collectorName = "nginx"

// Result holds nginx discovery output.
type Result struct {
	Installed   bool
	Running     bool
	ConfigText  string
	ServerNames []string
	Upstreams   []Upstream
	Locations   []ProxyLocation
	Relations   []model.Relation
}

// Upstream is an nginx upstream block.
type Upstream struct {
	Name    string
	Servers []string
}

// ProxyLocation is a server_name + proxy_pass pair.
type ProxyLocation struct {
	ServerName string
	Listen     []string
	ProxyPass  string
	Path       string
}

var (
	reServerName = regexp.MustCompile(`(?i)^\s*server_name\s+(.+?);`)
	reListen     = regexp.MustCompile(`(?i)^\s*listen\s+([^;]+);`)
	reProxyPass  = regexp.MustCompile(`(?i)^\s*proxy_pass\s+([^;]+);`)
	reUpstream   = regexp.MustCompile(`(?i)^\s*upstream\s+([^\s{]+)\s*\{`)
	reUpServer   = regexp.MustCompile(`(?i)^\s*server\s+([^;]+);`)
	reLocation   = regexp.MustCompile(`(?i)^\s*location\s+([^{]+)\{`)
	reComment    = regexp.MustCompile(`(?m)#.*$`)
)

// Collect discovers nginx and extracts simple topology from config.
func Collect(ctx context.Context, runner execx.Runner) (Result, model.CollectorResult) {
	ctx, cancel := execx.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	out := Result{}

	which := runner.Run(ctx, "sh", "-c", "command -v nginx")
	if which.Err != nil || strings.TrimSpace(which.Stdout) == "" {
		return out, model.CollectorResult{
			Name:    collectorName,
			Status:  model.CollectorNotInstalled,
			Message: "not installed",
		}
	}
	out.Installed = true

	// Check if running
	pidof := runner.Run(ctx, "pidof", "nginx")
	if pidof.Err == nil && strings.TrimSpace(pidof.Stdout) != "" {
		out.Running = true
	} else {
		st := runner.Run(ctx, "systemctl", "is-active", "nginx")
		if strings.TrimSpace(st.Stdout) == "active" {
			out.Running = true
		}
	}

	cfg := runner.Run(ctx, "nginx", "-T")
	if cfg.Err != nil {
		// Still report installed/running
		msg := "installed"
		if out.Running {
			msg = "running (config inaccessible)"
		}
		status := model.CollectorOK
		if execx.ClassifyError(cfg) == "permission_denied" {
			status = model.CollectorPermissionDenied
			msg = "permission denied reading config"
		}
		return out, model.CollectorResult{Name: collectorName, Status: status, Message: msg}
	}

	out.ConfigText = cfg.Stdout
	parsed := ParseConfig(cfg.Stdout)
	out.ServerNames = parsed.ServerNames
	out.Upstreams = parsed.Upstreams
	out.Locations = parsed.Locations
	out.Relations = parsed.Relations

	msg := "installed"
	if out.Running {
		msg = "running"
	}
	if len(out.Locations) > 0 {
		msg += ", " + strconv.Itoa(len(out.Locations)) + " proxy routes"
	}

	return out, model.CollectorResult{
		Name:    collectorName,
		Status:  model.CollectorOK,
		Count:   len(out.Locations),
		Message: msg,
	}
}

// ParseConfig extracts upstreams and proxy_pass relationships from nginx -T output.
func ParseConfig(raw string) Result {
	cleaned := stripComments(raw)
	upstreams := parseUpstreams(cleaned)
	upMap := map[string]Upstream{}
	for _, u := range upstreams {
		upMap[u.Name] = u
	}

	var locations []ProxyLocation
	var serverNames []string
	seenName := map[string]bool{}

	blocks := splitServerBlocks(cleaned)
	for _, block := range blocks {
		names := findServerNames(block)
		listens := findListens(block)
		for _, n := range names {
			if !seenName[n] && n != "_" && n != "localhost" {
				seenName[n] = true
				serverNames = append(serverNames, n)
			}
		}
		locs := findProxyLocations(block, names, listens)
		locations = append(locations, locs...)
	}

	var relations []model.Relation
	for _, loc := range locations {
		dest := normalizeProxyPass(loc.ProxyPass, upMap)
		if dest == "" {
			continue
		}
		label := loc.ServerName
		if label == "" {
			label = loc.Path
		}
		relations = append(relations, model.Relation{
			Source:      "nginx",
			Destination: dest,
			Type:        model.RelProxiesTo,
			Label:       label,
			Evidence:    "proxy_pass",
		})
	}

	return Result{
		ServerNames: serverNames,
		Upstreams:   upstreams,
		Locations:   locations,
		Relations:   relations,
	}
}

func stripComments(s string) string {
	return reComment.ReplaceAllString(s, "")
}

func splitServerBlocks(s string) []string {
	var blocks []string
	lower := strings.ToLower(s)
	idx := 0
	for {
		i := strings.Index(lower[idx:], "server")
		if i < 0 {
			break
		}
		i += idx
		// ensure it's a server { block, not server_name
		rest := strings.TrimSpace(s[i+6:])
		if !strings.HasPrefix(rest, "{") && !strings.HasPrefix(rest, "\n") && !strings.HasPrefix(rest, " ") {
			// might be server_name — skip keyword length
			idx = i + 6
			continue
		}
		// find opening brace
		brace := strings.Index(s[i:], "{")
		if brace < 0 {
			break
		}
		start := i + brace
		end := findMatchingBrace(s, start)
		if end < 0 {
			break
		}
		blocks = append(blocks, s[start+1:end])
		idx = end + 1
	}
	return blocks
}

func findMatchingBrace(s string, openIdx int) int {
	depth := 0
	for i := openIdx; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseUpstreams(s string) []Upstream {
	var out []Upstream
	lower := strings.ToLower(s)
	idx := 0
	for {
		i := strings.Index(lower[idx:], "upstream ")
		if i < 0 {
			break
		}
		i += idx
		m := reUpstream.FindStringSubmatch(s[i:])
		if m == nil {
			idx = i + 8
			continue
		}
		name := m[1]
		braceRel := strings.Index(s[i:], "{")
		if braceRel < 0 {
			break
		}
		start := i + braceRel
		end := findMatchingBrace(s, start)
		if end < 0 {
			break
		}
		body := s[start+1 : end]
		var servers []string
		for _, line := range strings.Split(body, "\n") {
			sm := reUpServer.FindStringSubmatch(line)
			if sm != nil {
				servers = append(servers, strings.Fields(sm[1])[0])
			}
		}
		out = append(out, Upstream{Name: name, Servers: servers})
		idx = end + 1
	}
	return out
}

func findServerNames(block string) []string {
	var names []string
	for _, line := range strings.Split(block, "\n") {
		m := reServerName.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		for _, n := range strings.Fields(m[1]) {
			n = strings.TrimSuffix(n, ";")
			if n != "" {
				names = append(names, n)
			}
		}
	}
	return names
}

func findListens(block string) []string {
	var out []string
	for _, line := range strings.Split(block, "\n") {
		m := reListen.FindStringSubmatch(line)
		if m != nil {
			out = append(out, strings.TrimSpace(m[1]))
		}
	}
	return out
}

func findProxyLocations(block string, serverNames, listens []string) []ProxyLocation {
	var out []ProxyLocation
	primaryName := ""
	if len(serverNames) > 0 {
		primaryName = serverNames[0]
	}

	idx := 0
	for {
		i := strings.Index(strings.ToLower(block[idx:]), "location")
		if i < 0 {
			break
		}
		i += idx
		m := reLocation.FindStringSubmatch(block[i:])
		if m == nil {
			idx = i + 8
			continue
		}
		path := strings.TrimSpace(m[1])
		braceRel := strings.Index(block[i:], "{")
		if braceRel < 0 {
			break
		}
		start := i + braceRel
		end := findMatchingBrace(block, start)
		if end < 0 {
			break
		}
		body := block[start+1 : end]
		for _, line := range strings.Split(body, "\n") {
			pm := reProxyPass.FindStringSubmatch(line)
			if pm == nil {
				continue
			}
			out = append(out, ProxyLocation{
				ServerName: primaryName,
				Listen:     listens,
				ProxyPass:  strings.TrimSpace(pm[1]),
				Path:       path,
			})
		}
		idx = end + 1
	}

	// Server-level proxy_pass (no location block)
	if len(out) == 0 {
		for _, line := range strings.Split(block, "\n") {
			pm := reProxyPass.FindStringSubmatch(line)
			if pm != nil {
				out = append(out, ProxyLocation{
					ServerName: primaryName,
					Listen:     listens,
					ProxyPass:  strings.TrimSpace(pm[1]),
					Path:       "/",
				})
			}
		}
	}

	return dedupeLocations(out)
}

func normalizeProxyPass(pp string, upMap map[string]Upstream) string {
	pp = strings.TrimSpace(pp)
	pp = strings.TrimSuffix(pp, "/")
	// http://127.0.0.1:4000 or http://api:4000 or http://upstream_name
	pp = strings.TrimPrefix(pp, "https://")
	pp = strings.TrimPrefix(pp, "http://")
	pp = strings.TrimPrefix(pp, "uwsgi://")
	pp = strings.TrimPrefix(pp, "fastcgi://")
	if i := strings.Index(pp, "/"); i >= 0 {
		pp = pp[:i]
	}
	if u, ok := upMap[pp]; ok {
		if len(u.Servers) > 0 {
			return cleanUpstreamServer(u.Servers[0])
		}
		return pp
	}
	return cleanUpstreamServer(pp)
}

func cleanUpstreamServer(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Fields(s)[0]
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	// 127.0.0.1:4000 → keep; unix: → skip
	if strings.HasPrefix(s, "unix:") {
		return ""
	}
	return s
}

func dedupeLocations(in []ProxyLocation) []ProxyLocation {
	seen := map[string]bool{}
	var out []ProxyLocation
	for _, l := range in {
		key := l.ServerName + "|" + l.Path + "|" + l.ProxyPass
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, l)
	}
	return out
}
