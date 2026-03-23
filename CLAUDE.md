# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build the CLI shim binary
go build -o docker ./cmd/docker

# Build the daemon proxy binary
go build -o docker-pull-shim-proxy ./cmd/proxy

# Run all tests
go test ./...

# Run tests for a specific package
go test ./cmd/docker/...
go test ./cmd/proxy/...

# Run a single test
go test ./cmd/docker/ -run TestNormalizeImage
go test ./cmd/proxy/ -run TestHandleConn_Passthrough

# Install CLI shim (places binary as ~/go/bin/docker)
go install github.com/tdakkota/docker-pull-shim/cmd/docker@latest
```

## Architecture

There are two binaries, each in its own package:

### `cmd/docker/` — CLI shim

When placed earlier in `PATH` than the real `docker`, it intercepts specific subcommands, pre-pulls images through a configured mirror registry, then hands off to the real `docker` via `syscall.Exec` — replacing the process so TTY, signals, and exit codes pass through transparently.

**Limitation**: only intercepts `docker` CLI calls; misses testcontainers, Docker SDK, compose-as-library, etc.

### `cmd/proxy/` — Daemon proxy

A Unix socket reverse proxy that sits between all Docker clients and dockerd. Users set `DOCKER_HOST=unix:///run/docker-pull-shim.sock`; the proxy forwards all traffic to the real daemon socket, intercepting `POST /images/create` (the Docker pull endpoint used by every client regardless of language).

```
DOCKER_HOST=unix:///run/docker-pull-shim.sock

  any client (CLI, testcontainers, SDK, ...)
          │  HTTP over Unix socket
          ▼
  /run/docker-pull-shim.sock   ← proxy listens here
          │
    POST /images/create?fromImage=<img>&tag=<tag>
    → skopeo copy docker://<mirror>/<img> docker-archive:<tmp>
    → POST /images/load (tar → upstream API, no CLI dependency)
          │
          ▼
  /var/run/docker.sock         ← real dockerd
```

### CLI shim dispatch flow (`cmd/docker/main.go`)

```
os.Args[1:] → switch on args[0]:
  "pull"    → extractPullImage    → prePullAll
  "run"     → extractRunImage     → prePullAll
  "build"   → imagesFromDockerfileArgs → prePullAll
  "compose" → findComposeSubcmd (handles flags before subcommand like -f)
              if sub == "up"|"pull" → imagesFromComposeArgs → prePullAll
  *         → pass-through
→ syscall.Exec(realDocker, ["docker", ...args], environ)
```

`findRealDocker` walks `PATH` and skips the current executable (by both direct path and resolved symlinks) to find the real `docker`.

### CLI shim: image extraction pattern

Each `extract*` function uses a **skip-next** loop with a `*FlagsWithValue` map:
- Flags in `--flag=value` form: skip nothing (value is part of the same token)
- Flags in `--flag value` form: set `skipNext = true` to consume the next arg
- First non-flag arg is the image (or context dir for `build`)

The same pattern is used in `findComposeSubcmd` to locate the compose subcommand past any global flags like `-f my.yml`.

### CLI shim key files (`cmd/docker/`)

| File | Responsibility |
|------|---------------|
| `main.go` | Entry point, `findRealDocker`, command dispatch, flag-skip helpers |
| `config.go` | YAML config loading from `~/.config/docker-pull-shim/config.yaml` |
| `image.go` | `normalizeImage`: bare name → `docker.io/library/name`, org/repo → `docker.io/org/repo`, registry-prefixed → unchanged |
| `pull.go` | `prePull` (skopeo + docker load), `prePullAll` (dedup + loop) |
| `compose.go` | Parse compose YAML, extract `services.*.image` values |
| `dockerfile.go` | Parse Dockerfile `FROM` lines, skip `scratch` |

### Daemon proxy key files (`cmd/proxy/`)

| File | Responsibility |
|------|---------------|
| `main.go` | Unix socket listener, `handleConn` (HTTP/1.1 loop, 101 hijack passthrough), `isKeepAlive` |
| `config.go` | `Config` with `Listen`/`Upstream` socket fields, `loadConfig`, `socketPath` (strips `unix://` prefix) |
| `image.go` | Same `normalizeImage` logic as `cmd/docker/image.go` |
| `pull.go` | `prePull` (skopeo + `POST /images/load` via custom transport), `loadImageAPI` |

### Config

`~/.config/docker-pull-shim/config.yaml`:
```yaml
mirror: "10.42.0.44:5000"   # host or host:port only — no slashes
tls_verify: false            # maps to skopeo --src-tls-verify
# proxy-only fields:
listen:   "unix:///run/docker-pull-shim.sock"
upstream: "unix:///var/run/docker.sock"
```

Missing config file is silently ignored (no mirror = pass-through only).

### Mirror pull sequence

**CLI shim** (`cmd/docker/pull.go`):
1. `skopeo copy ... docker-archive:<tmp>`
2. `docker load -i <tmp>` (CLI)

**Daemon proxy** (`cmd/proxy/pull.go`):
1. `skopeo copy ... docker-archive:<tmp>`
2. `POST /images/load?quiet=1` with tar body directly to upstream socket (no CLI)

Both:
- Validate `cfg.Mirror` — reject if contains whitespace or `/`
- `normalizeImage(image)` → fully-qualified name
- `--src-tls-verify=false` when `cfg.TLSVerify` is false
- Errors logged, never fatal; original operation always proceeds

### Proxy connection handling

`handleConn` parses raw HTTP/1.1 in a loop (one upstream connection per client connection):
- Intercepts `POST **/images/create` — matches both bare and versioned paths (`/v1.xx/images/create`) via `strings.HasSuffix`
- Skips pre-pull when `fromSrc != ""` (import-from-url/stdin, not a registry pull)
- `101 Switching Protocols` → flushes response, then bidirectional `io.Copy` (handles exec/attach/resize hijacking)
- `dialUpstream func() (net.Conn, error)` injected for testability; tests use TCP-backed `httptest.Server`

All subprocess stdout is redirected to `os.Stderr` to keep stdout clean for callers that pipe `docker` output.
