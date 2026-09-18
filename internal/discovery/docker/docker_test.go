package docker_test

import (
	"testing"

	"github.com/wtfisrunning/wtfisrunning/internal/discovery/docker"
	"github.com/wtfisrunning/wtfisrunning/internal/model"
)

func TestParsePSJSON(t *testing.T) {
	input := `{"ID":"abc1234567890","Names":"api","Image":"my-api:latest","Status":"Up 2 hours","State":"running","Ports":"0.0.0.0:4000->4000/tcp","Networks":"app-network"}
{"ID":"def1234567890","Names":"redis","Image":"redis:7","Status":"Up 2 hours","State":"running","Ports":"6379/tcp","Networks":"app-network"}
{"ID":"ghi1234567890","Names":"old","Image":"old:1","Status":"Exited (0)","State":"exited","Ports":"","Networks":"bridge"}
`
	cs, err := docker.ParsePSJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 3 {
		t.Fatalf("got %d", len(cs))
	}
	if cs[0].Name != "api" || cs[0].Status != model.StatusRunning {
		t.Errorf("%+v", cs[0])
	}
	if len(cs[0].Ports) != 1 || cs[0].Ports[0].HostPort != 4000 {
		t.Errorf("ports %+v", cs[0].Ports)
	}
	if cs[2].Status != model.StatusStopped {
		t.Errorf("expected stopped, got %s", cs[2].Status)
	}
}

func TestParsePorts(t *testing.T) {
	cases := []struct {
		in   string
		want int
		host int
		ctr  int
	}{
		{"0.0.0.0:8080->80/tcp", 1, 8080, 80},
		{"80/tcp", 1, 0, 80},
		{"0.0.0.0:443->443/tcp,:::443->443/tcp", 2, 443, 443},
		{"", 0, 0, 0},
	}
	for _, tc := range cases {
		got := docker.ParsePorts(tc.in)
		if len(got) != tc.want {
			t.Errorf("%q: len=%d want %d", tc.in, len(got), tc.want)
			continue
		}
		if tc.want > 0 {
			if got[0].HostPort != tc.host || got[0].ContainerPort != tc.ctr {
				t.Errorf("%q: %+v", tc.in, got[0])
			}
		}
	}
}
