package systemd_test

import (
	"testing"

	"github.com/ParthKadam11/WTFisRunning/internal/discovery/systemd"
)

func TestParseListUnitsKeepsAppsDropsNoise(t *testing.T) {
	input := `  nginx.service          loaded active running A high performance web server
  docker.service         loaded active running Docker Application Container Engine
  ssh.service            loaded active running OpenBSD Secure Shell server
  my-api.service         loaded active running Custom API
  cex-worker.service     loaded active running Worker
  systemd-resolved.service loaded active running Network Name Resolution
  cron.service           loaded active running Regular background program processing daemon
  user@1000.service      loaded active running User Manager for UID 1000
`
	svcs := systemd.ParseListUnits(input, nil)
	names := map[string]bool{}
	for _, s := range svcs {
		names[s.Name] = true
	}
	for _, want := range []string{"nginx", "docker", "ssh", "my-api", "cex-worker"} {
		if !names[want] {
			t.Errorf("missing %s in %#v", want, names)
		}
	}
	for _, drop := range []string{"systemd-resolved", "cron", "user@1000"} {
		if names[drop] {
			t.Errorf("should filter %s", drop)
		}
	}
}

func TestParseListUnitsHintsOverrideBoring(t *testing.T) {
	input := `  cron.service  loaded active running Cron
`
	svcs := systemd.ParseListUnits(input, []string{"cron"})
	if len(svcs) != 1 || svcs[0].Name != "cron" {
		t.Fatalf("%+v", svcs)
	}
}
