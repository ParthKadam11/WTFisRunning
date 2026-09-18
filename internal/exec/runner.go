package execx

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Result is the outcome of a command execution.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// Runner executes read-only shell commands locally or remotely.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) Result
	Host() string
}

// LocalRunner executes commands on the local machine.
type LocalRunner struct{}

func NewLocal() *LocalRunner {
	return &LocalRunner{}
}

func (r *LocalRunner) Host() string { return "local" }

func (r *LocalRunner) Run(ctx context.Context, name string, args ...string) Result {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
		}
	}
	return res
}

// RemoteRunner executes commands over SSH using the system ssh client.
type RemoteRunner struct {
	target string // user@host
}

func NewRemote(target string) *RemoteRunner {
	return &RemoteRunner{target: strings.TrimSpace(target)}
}

func (r *RemoteRunner) Host() string { return r.target }

func (r *RemoteRunner) Run(ctx context.Context, name string, args ...string) Result {
	// Non-interactive SSH often has a minimal PATH; force a sane one and
	// run through bash so binaries like docker/ss/systemctl resolve.
	inner := shellQuote(name, args...)
	remoteCmd := `export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$PATH"; ` + inner
	cmd := exec.CommandContext(ctx, "ssh",
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=15",
		"-o", "StrictHostKeyChecking=accept-new",
		r.target,
		remoteCmd,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := Result{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Err:    err,
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			res.ExitCode = ee.ExitCode()
		} else {
			res.ExitCode = -1
		}
	}
	return res
}

// WithTimeout wraps a parent context with a timeout.
func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}

// ClassifyError maps common exec failures to short messages.
func ClassifyError(res Result) string {
	if res.Err == nil {
		return ""
	}
	msg := strings.ToLower(res.Stderr + " " + res.Err.Error())
	switch {
	case strings.Contains(msg, "executable file not found") ||
		strings.Contains(msg, "not found") ||
		strings.Contains(msg, "no such file"):
		return "not_installed"
	case strings.Contains(msg, "permission denied") ||
		strings.Contains(msg, "operation not permitted"):
		return "permission_denied"
	case strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "context deadline"):
		return "timeout"
	case strings.Contains(msg, "cannot connect") ||
		strings.Contains(msg, "is the docker daemon running") ||
		strings.Contains(msg, "connection refused"):
		return "unavailable"
	default:
		if res.ExitCode == 127 {
			return "not_installed"
		}
		return "error"
	}
}

func shellQuote(name string, args ...string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, quote(name))
	for _, a := range args {
		parts = append(parts, quote(a))
	}
	return strings.Join(parts, " ")
}

func quote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"\\$`!#&|;<>(){}[]*?~") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ErrResult is a helper for wrapping a classified failure.
func ErrResult(kind, detail string) error {
	if detail == "" {
		return fmt.Errorf("%s", kind)
	}
	return fmt.Errorf("%s: %s", kind, detail)
}
