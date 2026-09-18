package ports_test

import (
	"testing"

	"github.com/wtfisrunning/wtfisrunning/internal/discovery/ports"
)

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
	if plist[0].Port != 22 || plist[0].Process != "sshd" {
		t.Errorf("port0 = %+v", plist[0])
	}
	if plist[1].Port != 80 || plist[1].Process != "nginx" {
		t.Errorf("port1 = %+v", plist[1])
	}
	if plist[2].Port != 443 {
		t.Errorf("port2 = %+v", plist[2])
	}
	if plist[3].Port != 5432 || plist[3].Process != "postgres" {
		t.Errorf("port3 = %+v", plist[3])
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
