package execx

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
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

// Session is an optional lifecycle for runners that need setup/teardown.
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
type RemoteRunner struct {
	target string

	mu sync.Mutex

	// Unix: ControlMaster multiplexing (password asked once by ssh).
	useMux      bool
	controlPath string

	// Windows / mux-fallback: cached password via SSH_ASKPASS.
	useAskPass  bool
	askPassPath string
	passFile    string
}

func NewRemote(target string) *RemoteRunner {
	target = strings.TrimSpace(target)
	safe := sanitizeHost(target)
	path := filepath.Join(os.TempDir(), "wtfis-"+safe+"-"+strconv.Itoa(os.Getpid()))
	r := &RemoteRunner{
		target:      target,
		controlPath: path,
		useMux:      runtime.GOOS != "windows",
	}
	return r
}

func (r *RemoteRunner) Host() string { return r.target }

// EnsureSSH returns a clear error when the OpenSSH client is missing.
func EnsureSSH() error {
	if _, err := exec.LookPath("ssh"); err != nil {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("ssh not found on PATH — install OpenSSH Client (Settings → Apps → Optional features), or run: Add-WindowsCapability -Online -Name OpenSSH.Client~~~~0.0.1.0")
		}
		return fmt.Errorf("ssh not found on PATH — install OpenSSH (e.g. apt install openssh-client)")
	}
	return nil
}

func (r *RemoteRunner) sshBaseOpts() []string {
	opts := []string{
		"-o", "ConnectTimeout=20",
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "PreferredAuthentications=publickey,keyboard-interactive,password",
		"-o", "NumberOfPasswordPrompts=3",
	}
	if r.useMux {
		opts = append(opts,
			"-o", "ControlMaster=auto",
			"-o", "ControlPath="+r.controlPath,
			"-o", "ControlPersist=120",
		)
	}
	return opts
}

// Connect authenticates once. On Windows (or if mux fails), caches a password
// for SSH_ASKPASS so later commands don't re-prompt.
func (r *RemoteRunner) Connect(ctx context.Context) error {
	if err := EnsureSSH(); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "connecting to %s…\n", r.target)

	if r.useMux {
		if err := r.connectMux(ctx); err == nil {
			return nil
		} else {
			fmt.Fprintf(os.Stderr, "note: SSH multiplexing failed (%v); falling back to password prompt\n", err)
			r.useMux = false
		}
	}

	return r.connectAskPass(ctx)
}

func (r *RemoteRunner) connectMux(ctx context.Context) error {
	args := append(r.sshBaseOpts(), r.target, "true")
	cmd := exec.CommandContext(ctx, "ssh", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *RemoteRunner) connectAskPass(ctx context.Context) error {
	// Try key-based auth first (no password needed).
	if err := r.probeBatch(ctx); err == nil {
		return nil
	}

	fmt.Fprintf(os.Stderr, "%s's password: ", r.target)
	pass, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	if len(pass) == 0 {
		return fmt.Errorf("empty password")
	}

	if err := r.setupAskPass(string(pass)); err != nil {
		zero(pass)
		return err
	}
	zero(pass)

	// Verify credentials.
	res := r.runSSH(ctx, "true")
	if res.Err != nil {
		r.cleanupAskPass()
		return fmt.Errorf("ssh to %s failed: %v\n%s", r.target, res.Err, strings.TrimSpace(res.Stderr))
	}
	r.useAskPass = true
	return nil
}

func (r *RemoteRunner) probeBatch(ctx context.Context) error {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=accept-new",
		r.target, "true",
	}
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	return cmd.Run()
}

func (r *RemoteRunner) setupAskPass(password string) error {
	dir, err := os.MkdirTemp("", "wtfis-askpass-*")
	if err != nil {
		return err
	}
	passFile := filepath.Join(dir, "pass")
	if err := os.WriteFile(passFile, []byte(password), 0600); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}

	var script string
	var scriptPath string
	if runtime.GOOS == "windows" {
		scriptPath = filepath.Join(dir, "askpass.cmd")
		// cmd.exe reads the password file and prints it for ssh.
		script = "@echo off\r\ntype \"%~dp0pass\"\r\n"
	} else {
		scriptPath = filepath.Join(dir, "askpass.sh")
		script = "#!/bin/sh\ncat \"$0.pass\"\n"
		// store pass next to script with fixed name used below
		passFile = scriptPath + ".pass"
		_ = os.Remove(filepath.Join(dir, "pass"))
		if err := os.WriteFile(passFile, []byte(password), 0600); err != nil {
			_ = os.RemoveAll(dir)
			return err
		}
	}

	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		_ = os.RemoveAll(dir)
		return err
	}

	r.askPassPath = scriptPath
	r.passFile = passFile
	r.useAskPass = true
	return nil
}

func (r *RemoteRunner) cleanupAskPass() {
	if r.passFile != "" {
		_ = os.WriteFile(r.passFile, nil, 0600)
		_ = os.Remove(r.passFile)
	}
	if r.askPassPath != "" {
		dir := filepath.Dir(r.askPassPath)
		_ = os.RemoveAll(dir)
	}
	r.askPassPath = ""
	r.passFile = ""
	r.useAskPass = false
}

// Close tears down the SSH session helpers.
func (r *RemoteRunner) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.useMux && r.controlPath != "" {
		cmd := exec.Command("ssh",
			"-o", "ControlPath="+r.controlPath,
			"-O", "exit",
			r.target,
		)
		_ = cmd.Run()
		_ = os.Remove(r.controlPath)
	}
	r.cleanupAskPass()
}

func (r *RemoteRunner) Run(ctx context.Context, name string, args ...string) Result {
	inner := shellQuote(name, args...)
	remoteCmd := `export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:$PATH"; ` + inner
	return r.runSSH(ctx, remoteCmd)
}

func (r *RemoteRunner) runSSH(ctx context.Context, remoteCmd string) Result {
	args := append(r.sshBaseOpts(), r.target, remoteCmd)
	cmd := exec.CommandContext(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if r.useAskPass && r.askPassPath != "" {
		env := os.Environ()
		env = append(env,
			"SSH_ASKPASS="+r.askPassPath,
			"SSH_ASKPASS_REQUIRE=force",
			"DISPLAY=wtfisrunning:0",
			"SSH_ASKPASS_PROMPT=none",
		)
		cmd.Env = env
		// Detach from TTY so ssh uses ASKPASS instead of prompting.
		cmd.Stdin = nil
	}

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

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
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
