package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/wtfisrunning/wtfisrunning/internal/discovery"
	execx "github.com/wtfisrunning/wtfisrunning/internal/exec"
	"github.com/wtfisrunning/wtfisrunning/internal/output"
	"github.com/wtfisrunning/wtfisrunning/internal/tui"
)

func main() {
	fs := flag.NewFlagSet("wtfisrunning", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	once := fs.Bool("once", false, "print a human-readable snapshot and exit")
	jsonOut := fs.Bool("json", false, "print the runtime model as JSON and exit")
	hostFlag := fs.String("host", "", "remote host (user@hostname) via SSH")
	help := fs.Bool("help", false, "show help")

	fs.Usage = printHelp

	args := rearrangeArgs(os.Args[1:])
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if *help {
		printHelp()
		os.Exit(0)
	}

	target, err := resolveHost(*hostFlag, fs.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n", err)
		printHelp()
		os.Exit(2)
	}

	var runner execx.Runner = execx.NewLocal()
	if target != "" {
		remote := execx.NewRemote(target)
		runner = remote
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		err := remote.Connect(ctx)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			fmt.Fprintf(os.Stderr, "hint: check SSH access, or try: ssh %s\n", target)
			os.Exit(1)
		}
		defer remote.Close()
	}

	if *jsonOut || *once {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		rt := discovery.Discover(ctx, runner)
		if *jsonOut {
			if err := output.WriteJSON(os.Stdout, rt); err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", err)
				os.Exit(1)
			}
			return
		}
		output.WriteOnce(os.Stdout, rt)
		return
	}

	if err := tui.Run(runner); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func rearrangeArgs(args []string) []string {
	var flags, positionals []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			name := strings.TrimLeft(a, "-")
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				continue
			}
			if name == "host" && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		positionals = append(positionals, a)
	}
	return append(flags, positionals...)
}

func resolveHost(hostFlag string, args []string) (string, error) {
	hostFlag = strings.TrimSpace(hostFlag)
	var positional string
	switch len(args) {
	case 0:
	case 1:
		positional = strings.TrimSpace(args[0])
	default:
		return "", fmt.Errorf("unexpected arguments: %s", strings.Join(args, " "))
	}

	if hostFlag != "" && positional != "" && hostFlag != positional {
		return "", fmt.Errorf("specify the host once (got %q and %q)", positional, hostFlag)
	}
	if positional != "" {
		return positional, nil
	}
	return hostFlag, nil
}

func printHelp() {
	fmt.Fprint(os.Stderr, `wtfisrunning — see what the fuck is actually running

USAGE
  wtfisrunning                      interactive TUI (local)
  wtfisrunning user@server          interactive TUI via SSH
  wtfisrunning user@server --once   human-readable snapshot
  wtfisrunning user@server --json   JSON runtime model

OPTIONS
  --once          Print a snapshot and exit
  --json          Print JSON and exit
  --host TARGET   Same as positional user@host
  --help          Show this help

REMOTE
  Asks for your SSH password once (if keys aren't set up), then reuses
  that session. Prefer SSH keys when you can.

  Docker "permission denied"? On the VPS:
    sudo usermod -aG docker $USER
    # then log out and back in
  Or give passwordless sudo docker:
    sudo visudo  # add:  youruser ALL=(ALL) NOPASSWD: /usr/bin/docker

KEYS (TUI)
  ↑↓ / j k    navigate
  enter       inspect service
  i           impact view
  /           filter
  r           refresh
  esc         back
  q           quit
`)
}
