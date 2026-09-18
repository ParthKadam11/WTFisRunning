# wtfisrunning

> See what the fuck is actually running on your Linux server.

```
┌─ WTF IS RUNNING? ────────────────────────────────────────────┐
│ runtime topology · production-vps                            │
│                                                              │
│  SERVICES                         SYSTEM                     │
│  ─────────────────────            ──────────────────────     │
│  › ● nginx             :80 :443   uptime     4d 12h          │
│    ● api               :4000      cpu        18%             │
│    ● web               :3000      memory     3.2 / 8 GB      │
│    ● redis             :6379      disk       41%             │
│                                                              │
│  RUNTIME                                                     │
│  ──────────────────────────────────────────────────────────  │
│  nginx                                                       │
│  ├── api.example.com ──► api:4000                            │
│  └── app.example.com ──► web:3000                            │
│                                                              │
│  5 services · 7 ports · 3 relationships       refreshed 2s   │
│  ↑↓ navigate   enter inspect   i impact   r refresh   q quit │
└──────────────────────────────────────────────────────────────┘
```

## Why

SSH into a box and you get a pile of tools: `docker ps`, `ss`, `systemctl`, `nginx -T`. None of them answer the simple question: **what is actually running, and how does it connect?**

`wtf` (`wtfisrunning`) is a read-only CLI/TUI that discovers runtime state and shows it as a clean topology.

This is an **MVP**, not a monitoring platform. No agents, no database, no cloud, no Kubernetes.

## Install

No clone. Install the binary on your laptop (same idea as installing `jq`), then run it against any host you can SSH to.

**macOS / Linux**

```bash
curl -fsSL https://github.com/ParthKadam11/WTFisRunning/releases/latest/download/i | sh
```

**Go**

```bash
go install github.com/ParthKadam11/WTFisRunning/cmd/wtf@latest
```

**Windows**

Download from [Releases](https://github.com/ParthKadam11/WTFisRunning/releases/latest) and put `wtf.exe` on PATH.

Requires OpenSSH (`ssh`) on the client. Nothing is installed on the remote server.

## How to use

Same idea as SSH: install `wtf` on **your** machine, point it at a host.

### Quick start

```bash
# 1. Install (macOS / Linux)
curl -fsSL https://github.com/ParthKadam11/WTFisRunning/releases/latest/download/i | sh

# 2. Confirm SSH works first
ssh user@your-server

# 3. Run wtf the same way
wtf user@your-server
```

You’ll get an interactive TUI of services, ports, and topology. Keys/password auth both work; password is asked once if needed.

### Common commands

| Command | What it does |
|---------|----------------|
| `wtf` | Scan **this** machine (local TUI) |
| `wtf user@host` | Scan a remote host over SSH (TUI) |
| `wtf user@host --once` | Print a human snapshot and exit |
| `wtf user@host --json` | Print JSON and exit (scripts/CI) |
| `wtf --host user@host` | Same as positional `user@host` |
| `wtf --help` | Show help |

Examples:

```bash
wtf                          # local
wtf deploy@1.2.3.4           # remote TUI
wtf deploy@1.2.3.4 --once    # one-shot text
wtf deploy@1.2.3.4 --json    # machine-readable
```

### TUI keys

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Navigate services |
| `enter` | Inspect selected service (details + recent logs) |
| `i` | Impact view (dependents / relationships) |
| `/` | Filter services |
| `r` | Refresh discovery |
| `esc` | Back / clear filter |
| `q` | Quit |

### Remote tips

- Needs OpenSSH on the client (`ssh` in PATH). Nothing is installed on the server.
- If `ssh user@host` fails, `wtf user@host` will fail too — fix SSH first.
- Docker **permission denied** on the VPS:

```bash
sudo usermod -aG docker $USER
# log out and back in, then retry
```

Or passwordless sudo for docker (the tool also tries `sudo -n docker` automatically):

```bash
echo 'deployer ALL=(ALL) NOPASSWD: /usr/bin/docker' | sudo tee /etc/sudoers.d/deployer-docker
```

## What it discovers

| Collector | What you get | If missing |
|-----------|--------------|------------|
| **Docker** | Containers, images, ports, networks, compose projects | `unavailable` / `not installed` |
| **Ports** | Listening TCP/UDP, owner user/cmdline, public vs local | degrades gracefully |
| **nginx** | Running state + `proxy_pass` / upstream topology | skipped |
| **systemd** | Relevant active services + failed units | skipped |
| **TLS** | Cert CN/expiry on :443/:8443 when reachable | skipped |
| **System** | Hostname, OS, uptime, CPU, memory, multi-disk mounts | partial |

Inspect view also loads recent **docker logs** / **journalctl** lines for the selected service.

All collectors are independent. One failure never kills the scan.

## Architecture

```
CLI
 └─ Discovery coordinator
     ├─ docker / ports / nginx / systemd / system
     └─ Runtime model
          ├─ TUI (Bubble Tea)
          ├─ --once
          └─ --json
```

Discovery never talks to the TUI directly. Collectors speak through a small `Runner` interface so local and SSH execution share the same code path.

## Limitations (honest)

- Linux-focused (reads `/proc`, uses `ss`, `systemctl`, `docker`, `nginx`)
- No root required, but some process/port ownership may be blank without privileges
- nginx parsing is intentionally incomplete — enough for useful topology, not a full config validator
- Docker network membership shows co-location, not application-level dependencies
- Impact view only uses discovered relationships (proxy_pass, networks) — it will not invent deps
- Remote mode shells out to `ssh` (keys or password)
- Not a live metrics daemon — each refresh is a fresh snapshot

## Development

```bash
go test ./...
go vet ./...
gofmt -w .
go run ./cmd/wtf --once
```

## License

MIT
