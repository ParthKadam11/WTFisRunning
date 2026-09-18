package discovery

import (
	"testing"

	"github.com/wtfisrunning/wtfisrunning/internal/discovery/nginx"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

func TestParseComposeDepends(t *testing.T) {
	got := parseComposeDepends("db:service_started:false,redis:service_started:false")
	if len(got) != 2 || got[0] != "db" || got[1] != "redis" {
		t.Fatalf("%v", got)
	}
	got = parseComposeDepends("postgres,redis")
	if len(got) != 2 {
		t.Fatalf("%v", got)
	}
}

func TestBuildRelationsCompose(t *testing.T) {
	rt := model.EmptyRuntime()
	rt.Containers = []model.Container{
		{
			Name:   "api",
			Status: model.StatusRunning,
			Labels: map[string]string{
				"com.docker.compose.project":    "app",
				"com.docker.compose.service":    "api",
				"com.docker.compose.depends_on": "db:service_started:false",
			},
			Networks: []string{"app_net"},
		},
		{
			Name:   "app-db-1",
			Status: model.StatusRunning,
			Labels: map[string]string{
				"com.docker.compose.project": "app",
				"com.docker.compose.service": "db",
			},
			Networks: []string{"app_net"},
		},
	}
	rt.Services = []model.Service{
		{ID: "container:api", Name: "api", Kind: "container", Status: model.StatusRunning},
		{ID: "container:app-db-1", Name: "app-db-1", Kind: "container", Status: model.StatusRunning},
	}
	nets := []model.Network{{Name: "app_net", Members: []string{"api", "app-db-1"}}}
	rels := buildRelations(rt, nginx.Result{}, nets)
	foundDep := false
	foundNet := false
	for _, r := range rels {
		if r.Type == model.RelDependsOn && r.Source == "api" && r.Destination == "app-db-1" {
			foundDep = true
		}
		if r.Type == model.RelSameNetwork {
			foundNet = true
		}
	}
	if !foundDep {
		t.Fatalf("missing depends_on in %+v", rels)
	}
	if !foundNet {
		t.Fatalf("missing network rel in %+v", rels)
	}
}
