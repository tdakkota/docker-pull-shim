# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build the binary
go build -o docker-pull-shim ./cmd/docker-pull-shim

# Run all tests
go test ./...

# Run tests for a specific package
go test ./cmd/docker-pull-shim/...

# Run a single test
go test ./cmd/docker-pull-shim/ -run TestHandleConn_Passthrough

# Run with debug logging
./docker-pull-shim -log-level debug

# Lint (run after every change; 0 issues is the bar)
golangci-lint run ./...
```

## Lint conventions

`golangci-lint` is the enforced linter. Run it after every change and fix all issues before committing.

## Architecture

### `cmd/docker-pull-shim/` — Daemon proxy

A Unix socket reverse proxy that sits between all Docker clients and dockerd.
Users set `DOCKER_HOST` to point at the proxy's socket; the proxy forwards all traffic to the real daemon socket, intercepting `POST /images/create` (the Docker pull endpoint used by every client regardless of language).

```
DOCKER_HOST=unix:///run/docker-pull-shim.sock   # (auto-detected at startup)

  any client (CLI, testcontainers, SDK, ...)
          │  HTTP over Unix socket
          ▼
  /run/docker-pull-shim.sock   ← proxy listens here (auto-detected)
          │
    POST /images/create?fromImage=<img>&tag=<tag>
    → skopeo copy docker://<mirror>/<img> docker-archive:<tmp>
    → POST /images/load (tar → upstream API, no CLI dependency)
          │
          ▼
  /var/run/docker.sock         ← real dockerd (auto-detected)
```

### Key files (`cmd/docker-pull-shim/`)

| File | Responsibility |
|------|---------------|
| `main.go` | Unix socket listener, `handleConn` (HTTP/1.1 loop, 101 hijack passthrough), `isKeepAlive`, `-log-level` flag |
| `config.go` | `Config` with `Listen`/`Upstream` pointer fields, `loadConfig`, `chooseUpstream`, `chooseListen`, `xdgRuntimeDir`, `socketPath` |
| `image.go` | `normalizeImage` — ensures fully-qualified image reference |
| `pull.go` | `prePull` (skopeo + `POST /images/load` via custom transport), `loadImageAPI` |

### Config

`~/.config/docker-pull-shim/config.yaml` (see `contrib/config.example.yaml`):
```yaml
mirror: "10.42.0.44:5000"   # host or host:port only — no slashes
tls_verify: false            # maps to skopeo --src-tls-verify
# listen and upstream are optional — auto-detected when omitted
#listen:   "unix:///run/docker-pull-shim.sock"
#upstream: "unix:///var/run/docker.sock"
```

Missing config file is silently ignored (no mirror = pass-through only).

### Socket auto-detection

**Upstream** (`chooseUpstream`): explicit config → rootless `$XDG_RUNTIME_DIR/docker.sock` → `/var/run/docker.sock`. Fails if no socket is found.

**Listen** (`chooseListen`): explicit config → rootless upstream → write access to `/run` → `$XDG_RUNTIME_DIR`. Logged at `Info` level so the chosen paths are always visible.

`xdgRuntimeDir()` returns `$XDG_RUNTIME_DIR` when set, else `/run/user/<uid>` if that directory exists.

### Mirror pull sequence

1. `skopeo copy ... docker-archive:<tmp>`
2. `POST /images/load?quiet=1` with tar body directly to upstream socket (no CLI)

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

### Logging

Uses `log/slog` (text handler to stderr). Default level is `info`.

```
-log-level debug|info|warn|error
```

Key log points: upstream/listen socket chosen (Info), intercepted pull (Info), skopeo start/finish (Info), no mirror (Debug), per-request path (Debug).

### Deployment

Ready-to-use systemd units are in `contrib/systemd/`:

| File | Use case |
|------|----------|
| `docker-pull-shim.service` | System-wide Docker (`/var/run/docker.sock`) |
| `docker-pull-shim.user.service` | Rootless Docker (`$XDG_RUNTIME_DIR/docker.sock`) |
