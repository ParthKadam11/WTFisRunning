package execx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// Session is an optional lifecycle for runners that need setup/teardown
// (e.g. SSH password auth + connection reuse).
type Session interface {
	Connect(ctx context.Context) error
	Close()
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
	return finish(stdout.String(), stderr.String(), err)
}

// RemoteRunner executes commands over SSH using the system ssh client.
// Connect() may prompt for a password once; later Run() calls reuse that session.
type RemoteRunner struct {
	target      string
	controlPath string
}

func NewRemote(target string) *RemoteRunner {
	target = strings.TrimSpace(target)
	safe := sanitizeHost(target)
	path := filepath.Join(os.TempDir(), "wtfis-"+safe+"-"+strconv.Itoa(os.Getpid()))
	return &RemoteRunner{target: target, controlPath: path}
}

func (r *RemoteRunner) Host() string { return r.target }

func (r *RemoteRunner) sshOpts() []string {
	return []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + r.controlPath,
		"-o", "ControlPersist=120",
		"-o", "ConnectTimeout=20",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "PreferredAuthentications=publickey,keyboard-interactive,password",
		"-o", "NumberOfPasswordPrompts=3",
	}
}

// Connect opens the SSH master connection. May prompt for a password on the TTY.
func (r *RemoteRunner) Connect(ctx context.Context) error {
	fmt.Fprintf(os.Stderr, "connecting to %s (enter password if asked)…\n", r.target)
	args := append(r.sshOpts(), r.target, "true")
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh to %s failed: %w", r.target, err)
	}
	return nil
}

// Close tears down the SSH master connection.
func (r *RemoteRunner) Close() {
	cmd := exec.Command("ssh",
		"-o", "ControlPath="+r.controlPath,
		"-O", "exit",
		r.target,
	)
	_ = cmd.Run()
	_ = os.Remove(r.controlPath)
}

func (r *RemoteRunner) Run(ctx context.Context, name string, args ...string) Result {
	inner := shellQuote(name, args...)
	remoteCmd := `export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$PATH"; ` + inner
	cmdArgs := append(r.sshOpts(), r.target, remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", cmdArgs...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return finish(stdout.String(), stderr.String(), err)
}

func finish(stdout, stderr string, err error) Result {
	res := Result{Stdout: stdout, Stderr: stderr, Err: err}
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
		strings.Contains(msg, "operation not permitted") ||
		strings.Contains(msg, "docker.sock"):
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

// DockerPermDenied reports whether a result looks like a docker.sock ACL failure.
func DockerPermDenied(res Result) bool {
	if res.Err == nil {
		return false
	}
	msg := strings.ToLower(res.Stderr + " " + res.Err.Error())
	return strings.Contains(msg, "permission denied") || strings.Contains(msg, "docker.sock")
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

func sanitizeHost(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '@', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := b.String()
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		return "host"
	}
	return out
}

// ErrResult is a helper for wrapping a classified failure.
func ErrResult(kind, detail string) error {
	if detail == "" {
		return fmt.Errorf("%s", kind)
	}
	return fmt.Errorf("%s: %s", kind, detail)
}
