# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build the daemon proxy binary
go build -o docker-pull-shim-proxy ./cmd/proxy

# Run all tests
go test ./...

# Run tests for a specific package
go test ./cmd/proxy/...

# Run a single test
go test ./cmd/proxy/ -run TestHandleConn_Passthrough
```

## Architecture

### `cmd/proxy/` — Daemon proxy

A Unix socket reverse proxy that sits between all Docker clients and dockerd.
Users set `DOCKER_HOST=unix:///run/docker-pull-shim.sock`; the proxy forwards all traffic to the real daemon socket, intercepting `POST /images/create` (the Docker pull endpoint used by every client regardless of language).

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
listen:   "unix:///run/docker-pull-shim.sock"
upstream: "unix:///var/run/docker.sock"
```

Missing config file is silently ignored (no mirror = pass-through only).

### Mirror pull sequence

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
