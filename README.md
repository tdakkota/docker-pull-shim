# docker-pull-shim

A Unix socket proxy that sits between Docker clients and the daemon, transparently redirecting image pulls through a registry mirror.

## How it works

The proxy listens on a Unix socket and forwards all Docker API traffic to the real daemon socket unchanged — except `POST /images/create` (the pull endpoint used by every client regardless of language). When a pull is intercepted and a mirror is configured, the proxy:

1. Runs `skopeo copy docker://<mirror>/<image> docker-archive:<tmp>`
2. Loads the resulting archive into the daemon via `POST /images/load`

The original pull request is then forwarded normally; the daemon finds the image already present and returns immediately. If either step fails the error is logged and the original request proceeds untouched — the proxy never breaks normal Docker operation.

```
DOCKER_HOST=unix:///run/docker-pull-shim.sock

  any client (docker CLI, SDK, testcontainers, …)
        │ HTTP over Unix socket
        ▼
  /run/docker-pull-shim.sock        ← proxy (auto-detected)
        │  intercepts POST /images/create
        │  → skopeo copy docker://<mirror>/<image> docker-archive:/tmp/img.tar
        │  → POST /images/load
        ▼
  /var/run/docker.sock              ← dockerd (auto-detected)
```

## Requirements

- [`skopeo`](https://github.com/containers/skopeo) — must be on `$PATH`
- Go 1.21+ (build only)

## Installation

```bash
make install          # builds and installs to ~/.local/bin/docker-pull-shim
```

Or build manually:

```bash
go build -o docker-pull-shim ./cmd/docker-pull-shim
```

### systemd

Ready-to-use units are in `contrib/systemd/`:

| Unit file | Use case |
|-----------|----------|
| `docker-pull-shim.service` | System-wide Docker (`/var/run/docker.sock`) |
| `docker-pull-shim.user.service` | Rootless Docker (`$XDG_RUNTIME_DIR/docker.sock`) |

**System-wide:**
```bash
sudo cp contrib/systemd/docker-pull-shim.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now docker-pull-shim
```

**Rootless:**
```bash
cp contrib/systemd/docker-pull-shim.user.service ~/.config/systemd/user/docker-pull-shim.service
systemctl --user daemon-reload
systemctl --user enable --now docker-pull-shim
```

## Configuration

Copy `contrib/config.example.yaml` to `~/.config/docker-pull-shim/config.yaml` and set your mirror:

```yaml
mirror: "registry.example.com"   # host or host:port — no scheme, no path
tls_verify: true
```

The listen and upstream sockets are auto-detected; see `contrib/config.example.yaml` for override options.

## Usage

Point Docker clients at the proxy socket:

```bash
export DOCKER_HOST=unix:///run/docker-pull-shim.sock   # system-wide
# or
export DOCKER_HOST=unix://$XDG_RUNTIME_DIR/docker-pull-shim.sock   # rootless
```

The proxy logs the chosen sockets at startup — check them if you're unsure which path to use:

```
$ docker-pull-shim -log-level debug
time=… level=INFO msg="auto-detected system-wide upstream" path=/var/run/docker.sock
time=… level=INFO msg="using system-wide listen socket" path=/run/docker-pull-shim.sock
time=… level=INFO msg=listening listen=/run/docker-pull-shim.sock upstream=/var/run/docker.sock
```
