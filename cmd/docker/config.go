package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds the shim configuration.
type Config struct {
	Mirror    string `yaml:"mirror"`
	TLSVerify bool   `yaml:"tls_verify"`
}

func loadConfig() (Config, error) {
	var cfg Config

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

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}
