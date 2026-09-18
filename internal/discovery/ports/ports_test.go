package ports_test

import (
	"testing"

	"github.com/ParthKadam11/WTFisRunning/internal/discovery/ports"
	"github.com/ParthKadam11/WTFisRunning/internal/model"
)

func TestClassifyExposure(t *testing.T) {
	cases := map[string]model.Exposure{
		"0.0.0.0":     model.ExposurePublic,
		"*":           model.ExposurePublic,
		"::":          model.ExposurePublic,
		"127.0.0.1":   model.ExposureLocal,
		"::1":         model.ExposureLocal,
		"127.0.0.53":  model.ExposureLocal,
		"10.0.0.5":    model.ExposurePrivate,
		"192.168.1.1": model.ExposurePrivate,
	}
	for addr, want := range cases {
		if got := ports.ClassifyExposure(addr); got != want {
			t.Errorf("%s: got %s want %s", addr, got, want)
		}
	}
}

func TestParseSS(t *testing.T) {
	input := `Netid State  Recv-Q Send-Q Local Address:Port Peer Address:Port Process
tcp   LISTEN 0      128          0.0.0.0:22         0.0.0.0:*    users:(("sshd",pid=1001,fd=3))
tcp   LISTEN 0      511          0.0.0.0:80         0.0.0.0:*    users:(("nginx",pid=200,fd=8))
tcp   LISTEN 0      511             [::]:443           [::]:*    users:(("nginx",pid=200,fd=9))
tcp   LISTEN 0      128        127.0.0.1:5432       0.0.0.0:*    users:(("postgres",pid=300,fd=5))
udp   UNCONN 0      0          127.0.0.1:323        0.0.0.0:*    
`
	plist, procs := ports.ParseSS(input)
	if len(plist) != 5 {
		t.Fatalf("expected 5 ports, got %d", len(plist))
	}
	if plist[0].Exposure != model.ExposurePublic {
		t.Errorf("ssh exposure=%s", plist[0].Exposure)
	}
	if plist[3].Exposure != model.ExposureLocal {
		t.Errorf("postgres exposure=%s", plist[3].Exposure)
	}
	if len(procs) < 3 {
		t.Fatalf("expected processes, got %d", len(procs))
	}
}

func TestParseSSEmpty(t *testing.T) {
	plist, procs := ports.ParseSS("")
	if len(plist) != 0 || len(procs) != 0 {
		t.Fatalf("expected empty")
	}
}

func TestParseSSNoProcess(t *testing.T) {
	input := `tcp   LISTEN 0 128 0.0.0.0:8080 0.0.0.0:*`
	plist, _ := ports.ParseSS(input)
	if len(plist) != 1 {
		t.Fatalf("got %d", len(plist))
	}
	if plist[0].Port != 8080 || plist[0].Process != "" {
		t.Errorf("%+v", plist[0])
	}
}
