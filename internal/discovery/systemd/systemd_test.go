package systemd_test

import (
	"testing"

	"github.com/wtfisrunning/wtfisrunning/internal/discovery/systemd"
)

func TestParseListUnits(t *testing.T) {
	input := `  nginx.service          loaded active running A high performance web server
  docker.service         loaded active running Docker Application Container Engine
  ssh.service            loaded active running OpenBSD Secure Shell server
  unrelated-foo.service  loaded active running Something obscure
  cron.service           loaded active running Regular background program processing daemon
  redis.service          loaded active running Advanced key-value store
`
	svcs := systemd.ParseListUnits(input, nil)
	names := map[string]bool{}
	for _, s := range svcs {
		names[s.Name] = true
	}
	for _, want := range []string{"nginx", "docker", "ssh", "cron", "redis"} {
		if !names[want] {
			t.Errorf("missing %s in %#v", want, names)
		}
	}
	if names["unrelated-foo"] {
		t.Errorf("should filter unrelated-foo")
	}
}

func TestParseListUnitsHints(t *testing.T) {
	input := `  myapp.service  loaded active running My App
  other.service  loaded active running Other
`
	svcs := systemd.ParseListUnits(input, []string{"myapp"})
	if len(svcs) != 1 || svcs[0].Name != "myapp" {
		t.Fatalf("%+v", svcs)
	}
}
