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

`wtfisrunning` is a read-only CLI/TUI that discovers runtime state and shows it as a clean topology.

This is an **MVP**, not a monitoring platform. No agents, no database, no cloud, no Kubernetes.

## Install

```bash
go install github.com/wtfisrunning/wtfisrunning/cmd/wtfisrunning@latest
```

Or from this repo:

```bash
go build -o wtfisrunning ./cmd/wtfisrunning
```

Requires Go 1.22+.

## Usage

```bash
# Interactive TUI (local machine)
wtfisrunning

# Remote VPS via SSH (password or keys — password is asked once)
wtfisrunning user@server
wtfisrunning user@server --once
wtfisrunning user@server --json
```

If Docker shows **permission denied**, on the VPS:

```bash
sudo usermod -aG docker $USER
# log out and back in, then retry
```

Or allow passwordless docker via sudo (the tool will try `sudo -n docker` automatically).

### TUI keys

| Key | Action |
|-----|--------|
| `↑` `↓` / `j` `k` | Navigate |
| `enter` | Inspect service |
| `i` | Impact view (when relationships exist) |
| `/` | Filter services |
| `r` | Refresh |
| `esc` | Back |
| `q` | Quit |

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
- Remote mode shells out to `ssh`; password auth is not supported
- Not a live metrics daemon — each refresh is a fresh snapshot

## Development

```bash
go test ./...
go vet ./...
gofmt -w .
go run ./cmd/wtfisrunning --once
```

## License

MIT
