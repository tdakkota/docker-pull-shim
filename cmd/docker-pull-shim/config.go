package main

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the proxy configuration.
type Config struct {
	Mirror    string  `yaml:"mirror"`
	TLSVerify bool    `yaml:"tls_verify"`
	Listen    *string `yaml:"listen"`   // nil = auto-detect; set = use exactly
	Upstream  *string `yaml:"upstream"` // nil = auto-detect; set = use exactly
}

const sockName = "docker-pull-shim.sock"

func loadConfig() (Config, error) {
	// Both Listen and Upstream are nil: auto-detect at startup.
	cfg := Config{}

	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return cfg, fmt.Errorf("locate config dir: %w", err)
	}
	path := filepath.Join(cfgDir, "docker-pull-shim", "config.yaml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("read config %s: %w", path, err)
	}

	// Unmarshal into a temporary struct so we can apply defaults for zero-value fields.
	var raw Config
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	if raw.Mirror != "" {
		cfg.Mirror = raw.Mirror
	}
	cfg.TLSVerify = raw.TLSVerify
	if raw.Listen != nil {
		cfg.Listen = raw.Listen
	}
	if raw.Upstream != nil {
		cfg.Upstream = raw.Upstream
	}
	return cfg, nil
}

// chooseUpstream returns the bare Unix socket path of the upstream Docker daemon.
// If cfg.Upstream was explicitly set in config, it is validated and used as-is.
// Otherwise autoUpstreamPath is called, which prefers a rootless daemon.
func chooseUpstream(cfg Config) (string, error) {
	if cfg.Upstream != nil {
		path := socketPath(*cfg.Upstream)
		if !isSocket(path) {
			return "", fmt.Errorf("upstream socket %s does not exist", path)
		}
		return path, nil
	}
	return autoUpstreamPath()
}

// autoUpstreamPath detects the upstream daemon socket in priority order:
//  1. Rootless Docker: $XDG_RUNTIME_DIR/docker.sock (if it exists)
//  2. System-wide: /var/run/docker.sock (if it exists)
func autoUpstreamPath() (string, error) {
	if xdg := xdgRuntimeDir(); xdg != "" {
		path := filepath.Join(xdg, "docker.sock")
		if isSocket(path) {
			slog.Info("auto-detected rootless upstream", "path", path)
			return path, nil
		}
	}
	const rootSock = "/var/run/docker.sock"
	if isSocket(rootSock) {
		slog.Info("auto-detected system-wide upstream", "path", rootSock)
		return rootSock, nil
	}
	return "", fmt.Errorf("no Docker daemon socket found; set upstream: in config")
}

// chooseListen returns the bare Unix socket path the proxy should listen on.
// If cfg.Listen was explicitly set in config, it is used as-is (after stripping
// the unix:// prefix). Otherwise the path is auto-detected by autoListenPath.
func chooseListen(cfg Config, upstreamSocket string) (string, error) {
	if cfg.Listen != nil {
		return socketPath(*cfg.Listen), nil
	}
	return autoListenPath(upstreamSocket)
}

// autoListenPath selects the listen socket path in priority order:
//  1. Rootless Docker: upstream socket is under $XDG_RUNTIME_DIR
//     → $XDG_RUNTIME_DIR/docker-pull-shim.sock
//  2. Write access to /run → /run/docker-pull-shim.sock
//  3. $XDG_RUNTIME_DIR is set → $XDG_RUNTIME_DIR/docker-pull-shim.sock
func autoListenPath(upstreamSocket string) (string, error) {
	xdg := xdgRuntimeDir()

	// Rootless Docker: upstream socket lives under XDG_RUNTIME_DIR, or the
	// standard rootless Docker socket exists there.
	if xdg != "" {
		if strings.HasPrefix(upstreamSocket, xdg+"/") || isSocket(filepath.Join(xdg, "docker.sock")) {
			path := filepath.Join(xdg, sockName)
			slog.Info("using user-level listen socket", "path", path, "reason", "rootless upstream")
			return path, nil
		}
	}

	// Try system-wide /run.
	if canWriteDir("/run") {
		path := filepath.Join("/run", sockName)
		slog.Info("using system-wide listen socket", "path", path)
		return path, nil
	}
	// Fall back to the user runtime directory.
	if xdg != "" {
		path := filepath.Join(xdg, sockName)
		slog.Info("using user-level listen socket", "path", path, "reason", "no write access to /run")
		return path, nil
	}
	return "", fmt.Errorf("no write access to /run and XDG_RUNTIME_DIR is not set; set listen: in config")
}

// xdgRuntimeDir returns the XDG runtime directory.
// It uses $XDG_RUNTIME_DIR when set, otherwise falls back to the
// conventional default /run/user/<uid> if that directory exists.
func xdgRuntimeDir() string {
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		return xdg
	}
	dir := fmt.Sprintf("/run/user/%d", os.Getuid())
	if _, err := os.Stat(dir); err == nil {
		return dir
	}
	return ""
}

// isSocket reports whether path exists and is a Unix socket.
func isSocket(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode()&os.ModeSocket != 0
}

// canWriteDir reports whether the process can create files in dir.
func canWriteDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".docker-pull-shim-probe-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// socketPath strips the "unix://" prefix from a socket URL.
func socketPath(u string) string {
	return strings.TrimPrefix(u, "unix://")
}
