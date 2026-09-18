package compose_test

import (
	"testing"

	"github.com/wtfisrunning/wtfisrunning/internal/discovery/compose"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

func TestBuildProjects(t *testing.T) {
	cs := []model.Container{
		{
			Name:   "app-api-1",
			Status: model.StatusRunning,
			Labels: map[string]string{
				"com.docker.compose.project": "app",
				"com.docker.compose.service": "api",
			},
		},
		{
			Name:   "app-db-1",
			Status: model.StatusRunning,
			Labels: map[string]string{
				"com.docker.compose.project": "app",
				"com.docker.compose.service": "db",
			},
		},
		{
			Name:   "lonely",
			Status: model.StatusRunning,
		},
	}
	projects := compose.BuildProjects(cs)
	if len(projects) != 1 {
		t.Fatalf("got %d projects", len(projects))
	}
	if projects[0].Name != "app" || projects[0].Running != 2 || projects[0].Total != 2 {
		t.Fatalf("%+v", projects[0])
	}
	if len(projects[0].Services) != 2 {
		t.Fatalf("services=%v", projects[0].Services)
	}
}

func TestDependsOnList(t *testing.T) {
	got := compose.DependsOnList("db:service_started:false,redis:service_started:false")
	if len(got) != 2 || got[0] != "db" || got[1] != "redis" {
		t.Fatalf("%v", got)
	}
}
