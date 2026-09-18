package model

import "time"

// Status represents the health/availability of a runtime entity.
type Status string

const (
	StatusRunning Status = "running"
	StatusStopped Status = "stopped"
	StatusWarning Status = "warning"
	StatusFailed  Status = "failed"
	StatusUnknown Status = "unknown"
	StatusPartial Status = "partial"
)

// CollectorStatus describes whether a discovery collector succeeded.
type CollectorStatus string

const (
	CollectorOK               CollectorStatus = "ok"
	CollectorUnavailable      CollectorStatus = "unavailable"
	CollectorNotInstalled     CollectorStatus = "not_installed"
	CollectorPermissionDenied CollectorStatus = "permission_denied"
	CollectorTimeout          CollectorStatus = "timeout"
	CollectorParseError       CollectorStatus = "parse_error"
	CollectorError            CollectorStatus = "error"
)

// RelationType classifies a discovered relationship.
type RelationType string

const (
	RelProxiesTo   RelationType = "proxies_to"
	RelListensOn   RelationType = "listens_on"
	RelDependsOn   RelationType = "depends_on"
	RelRuns        RelationType = "runs"
	RelExposes     RelationType = "exposes"
	RelSameNetwork RelationType = "same_network"
)

// Runtime is the complete discovered snapshot of a machine.
type Runtime struct {
	DiscoveredAt time.Time                  `json:"discovered_at"`
	Host         string                     `json:"host,omitempty"`
	System       SystemInfo                 `json:"system"`
	Services     []Service                  `json:"services"`
	Containers   []Container                `json:"containers"`
	Processes    []Process                  `json:"processes"`
	Ports        []Port                     `json:"ports"`
	Networks     []Network                  `json:"networks"`
	Relations    []Relation                 `json:"relations"`
	Collectors   map[string]CollectorResult `json:"collectors"`
}

// SystemInfo holds basic host metrics.
type SystemInfo struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	Kernel      string  `json:"kernel,omitempty"`
	Uptime      string  `json:"uptime"`
	UptimeSec   int64   `json:"uptime_sec,omitempty"`
	CPUPercent  float64 `json:"cpu_percent"`
	MemUsedGB   float64 `json:"mem_used_gb"`
	MemTotalGB  float64 `json:"mem_total_gb"`
	DiskPercent float64 `json:"disk_percent"`
	DiskUsedGB  float64 `json:"disk_used_gb,omitempty"`
	DiskTotalGB float64 `json:"disk_total_gb,omitempty"`
}

// Service is a logical runtime unit (container, systemd unit, or named process).
type Service struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Kind        string   `json:"kind"` // container | systemd | process | nginx
	Status      Status   `json:"status"`
	StatusText  string   `json:"status_text,omitempty"`
	Ports       []int    `json:"ports,omitempty"`
	ContainerID string   `json:"container_id,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	PID         int      `json:"pid,omitempty"`
	Image       string   `json:"image,omitempty"`
	Networks    []string `json:"networks,omitempty"`
	Detail      string   `json:"detail,omitempty"`
}

// Container is a Docker container snapshot.
type Container struct {
	ID       string            `json:"id"`
	Name     string            `json:"name"`
	Image    string            `json:"image"`
	Status   Status            `json:"status"`
	State    string            `json:"state"`
	Ports    []PortMapping     `json:"ports,omitempty"`
	Networks []string          `json:"networks,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Created  string            `json:"created,omitempty"`
	Command  string            `json:"command,omitempty"`
}

// PortMapping is a published Docker port.
type PortMapping struct {
	HostIP        string `json:"host_ip,omitempty"`
	HostPort      int    `json:"host_port,omitempty"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
}

// Process is a process owning a listening port or related to a service.
type Process struct {
	PID     int    `json:"pid"`
	Name    string `json:"name"`
	User    string `json:"user,omitempty"`
	Command string `json:"command,omitempty"`
}

// Port is a listening socket.
type Port struct {
	Protocol  string `json:"protocol"`
	Address   string `json:"address"`
	Port      int    `json:"port"`
	PID       int    `json:"pid,omitempty"`
	Process   string `json:"process,omitempty"`
	Userspace string `json:"userspace,omitempty"`
}

// Network is a Docker network.
type Network struct {
	ID      string   `json:"id,omitempty"`
	Name    string   `json:"name"`
	Driver  string   `json:"driver,omitempty"`
	Members []string `json:"members,omitempty"`
}

// Relation links two runtime entities with evidence.
type Relation struct {
	Source      string       `json:"source"`
	Destination string       `json:"destination"`
	Type        RelationType `json:"type"`
	Label       string       `json:"label,omitempty"`
	Evidence    string       `json:"evidence,omitempty"`
}

// CollectorResult reports one collector's outcome.
type CollectorResult struct {
	Name    string          `json:"name"`
	Status  CollectorStatus `json:"status"`
	Message string          `json:"message,omitempty"`
	Count   int             `json:"count,omitempty"`
}

// EmptyRuntime returns a zero-value runtime with initialized maps.
func EmptyRuntime() *Runtime {
	return &Runtime{
		DiscoveredAt: time.Now().UTC(),
		Collectors:   map[string]CollectorResult{},
		Services:     []Service{},
		Containers:   []Container{},
		Processes:    []Process{},
		Ports:        []Port{},
		Networks:     []Network{},
		Relations:    []Relation{},
	}
}

// ServiceByID finds a service by ID.
func (r *Runtime) ServiceByID(id string) *Service {
	for i := range r.Services {
		if r.Services[i].ID == id {
			return &r.Services[i]
		}
	}
	return nil
}

// ServiceByName finds a service by name (case-insensitive exact).
func (r *Runtime) ServiceByName(name string) *Service {
	for i := range r.Services {
		if r.Services[i].Name == name {
			return &r.Services[i]
		}
	}
	return nil
}

// DependentsOf returns services that depend on the given service (incoming relations).
func (r *Runtime) DependentsOf(serviceID string) []Relation {
	var out []Relation
	svc := r.ServiceByID(serviceID)
	if svc == nil {
		return out
	}
	for _, rel := range r.Relations {
		if rel.Destination == serviceID || rel.Destination == svc.Name {
			out = append(out, rel)
		}
		// Also match port-based destinations like "redis:6379"
		if svc.Name != "" && (rel.Destination == svc.Name || hasPrefixName(rel.Destination, svc.Name)) {
			if !containsRel(out, rel) {
				out = append(out, rel)
			}
		}
	}
	return out
}

// DependenciesOf returns outgoing relations from a service.
func (r *Runtime) DependenciesOf(serviceID string) []Relation {
	var out []Relation
	svc := r.ServiceByID(serviceID)
	if svc == nil {
		return out
	}
	for _, rel := range r.Relations {
		if rel.Source == serviceID || rel.Source == svc.Name {
			out = append(out, rel)
		}
	}
	return out
}

func hasPrefixName(dest, name string) bool {
	if len(dest) <= len(name) {
		return dest == name
	}
	return dest[:len(name)] == name && (dest[len(name)] == ':' || dest[len(name)] == '/')
}

func containsRel(rels []Relation, r Relation) bool {
	for _, x := range rels {
		if x.Source == r.Source && x.Destination == r.Destination && x.Type == r.Type {
			return true
		}
	}
	return false
}
