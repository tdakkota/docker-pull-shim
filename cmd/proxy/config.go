package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the proxy configuration.
type Config struct {
	Mirror    string `yaml:"mirror"`
	TLSVerify bool   `yaml:"tls_verify"`
	Listen    string `yaml:"listen"`   // unix socket path the proxy listens on
	Upstream  string `yaml:"upstream"` // real dockerd socket path
}

const (
	defaultListen   = "unix:///run/docker-pull-shim.sock"
	defaultUpstream = "unix:///var/run/docker.sock"
)

func loadConfig() (Config, error) {
	cfg := Config{
		Listen:   defaultListen,
		Upstream: defaultUpstream,
	}

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
	if raw.Listen != "" {
		cfg.Listen = raw.Listen
	}
	if raw.Upstream != "" {
		cfg.Upstream = raw.Upstream
	}
	return cfg, nil
}

// socketPath strips the "unix://" prefix from a socket URL.
func socketPath(u string) string {
	return strings.TrimPrefix(u, "unix://")
}
