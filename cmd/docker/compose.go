package main

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// composeFile is a minimal representation of a Docker Compose file,
// containing only the fields needed for image extraction.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

type composeService struct {
	Image string `yaml:"image"`
}

// defaultComposeFiles is the search order for compose files when -f is not given.
var defaultComposeFiles = []string{
	"compose.yaml",
	"compose.yml",
	"docker-compose.yml",
	"docker-compose.yaml",
}

// imagesFromComposeArgs extracts image names from a compose file.
// args are the arguments after "compose" (e.g. ["-f", "my-compose.yml", "up", ...]).
func imagesFromComposeArgs(args []string) ([]string, error) {
	path := composeFilePath(args)
	if path == "" {
		return nil, fmt.Errorf("no compose file found")
	}
	return imagesFromComposeFile(path)
}

// composeFilePath returns the compose file path from args or by searching defaults.
func composeFilePath(args []string) string {
	for i, a := range args {
		if (a == "-f" || a == "--file") && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, "--file="); ok {
			return v
		}
		if v, ok := strings.CutPrefix(a, "-f="); ok {
			return v
		}
	}
	for _, name := range defaultComposeFiles {
		if _, err := os.Stat(name); err == nil {
			return name
		}
	}
	return ""
}

func imagesFromComposeFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read compose file %s: %w", path, err)
	}

	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parse compose file %s: %w", path, err)
	}

	images := make([]string, 0, len(cf.Services))
	for _, svc := range cf.Services {
		if svc.Image != "" {
			images = append(images, svc.Image)
		}
	}
	return images, nil
}
