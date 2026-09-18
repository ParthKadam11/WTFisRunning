package system_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/wtfisrunning/wtfisrunning/internal/discovery/system"
	execx "github.com/wtfisrunning/wtfisrunning/internal/exec"
)

type fakeRunner struct {
	responses map[string]execx.Result
}

func (f *fakeRunner) Host() string { return "test" }

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) execx.Result {
	key := name + " " + strings.Join(args, " ")
	if r, ok := f.responses[key]; ok {
		return r
	}
	// prefix match for flexibility
	for k, r := range f.responses {
		if strings.HasPrefix(key, k) {
			return r
		}
	}
	return execx.Result{Err: context.Canceled, ExitCode: -1}
}

func TestCollectParsesSystem(t *testing.T) {
	r := &fakeRunner{responses: map[string]execx.Result{
		"hostname":            {Stdout: "prod-vps\n"},
		"cat /etc/os-release": {Stdout: "PRETTY_NAME=\"Ubuntu 24.04 LTS\"\nNAME=\"Ubuntu\"\n"},
		"uname -r":            {Stdout: "6.8.0\n"},
		"cat /proc/uptime":    {Stdout: "90061.22 12345.00\n"},
		"cat /proc/meminfo": {Stdout: "" +
			"MemTotal:        8165432 kB\n" +
			"MemAvailable:    4092716 kB\n"},
		"df -P -k /": {Stdout: "" +
			"Filesystem     1024-blocks    Used Available Capacity Mounted on\n" +
			"/dev/sda1        10240000 4198400   6041600      41% /\n"},
		"cat /proc/stat": {Stdout: "cpu  100 0 100 800 0 0 0 0 0 0\n"},
	}}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	info, res := system.Collect(ctx, r)
	if res.Status != "ok" {
		t.Fatalf("status=%s", res.Status)
	}
	if info.Hostname != "prod-vps" {
		t.Errorf("hostname=%s", info.Hostname)
	}
	if info.OS != "Ubuntu 24.04 LTS" {
		t.Errorf("os=%s", info.OS)
	}
	if info.Uptime != "1d 1h" {
		t.Errorf("uptime=%s", info.Uptime)
	}
	if info.DiskPercent != 41 {
		t.Errorf("disk=%v", info.DiskPercent)
	}
	if info.MemTotalGB < 7 || info.MemTotalGB > 9 {
		t.Errorf("mem total=%v", info.MemTotalGB)
	}
}

func TestDiskPathWithSpaces(t *testing.T) {
	r := &fakeRunner{responses: map[string]execx.Result{
		"hostname":            {Stdout: "host\n"},
		"cat /etc/os-release": {Stdout: "PRETTY_NAME=\"Test\"\n"},
		"uname -r":            {Stdout: "1\n"},
		"cat /proc/uptime":    {Stdout: "10.0 1.0\n"},
		"cat /proc/meminfo":   {Stdout: "MemTotal: 1000 kB\nMemAvailable: 500 kB\n"},
		"df -P -k /": {Stdout: "" +
			"Filesystem           1024-blocks      Used Available Capacity Mounted on\n" +
			"C:/Program Files/Git   392918012 381977036  10940976      98% /\n"},
		"cat /proc/stat": {Stdout: "cpu  1 0 1 8 0 0 0\n"},
	}}
	info, _ := system.Collect(context.Background(), r)
	if info.DiskPercent != 98 {
		t.Fatalf("disk percent=%v want 98", info.DiskPercent)
	}
}
