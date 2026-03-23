package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// imagesFromDockerfileArgs extracts base images from the Dockerfile referenced
// by args (the arguments after "build").
func imagesFromDockerfileArgs(args []string) ([]string, error) {
	path := dockerfilePath(args)
	if path == "" {
		return nil, fmt.Errorf("Dockerfile not found")
	}
	return imagesFromDockerfile(path)
}

// buildFlagsWithValue is the set of docker build flags that consume the next argument.
// See: https://docs.docker.com/reference/cli/docker/buildx/build/
var buildFlagsWithValue = map[string]bool{
	"-f": true, "--file": true,
	"--build-arg": true, "--cache-from": true, "--cache-to": true,
	"--label": true, "-l": true,
	"--network": true,
	"--output": true, "-o": true,
	"--platform": true, "--progress": true,
	"--secret": true, "--ssh": true,
	"--tag": true, "-t": true,
	"--target": true,
	"--shm-size": true, "--ulimit": true, "--iidfile": true,
	"--add-host": true, "--attest": true, "--sbom": true, "--provenance": true,
}

// dockerfilePath returns the Dockerfile path from args or the default.
func dockerfilePath(args []string) string {
	// First pass: look for an explicit -f/--file flag.
	for i := range args {
		if (args[i] == "-f" || args[i] == "--file") && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(args[i], "--file="); ok {
			return v
		}
		if v, ok := strings.CutPrefix(args[i], "-f="); ok {
			return v
		}
	}

	// Second pass: find the build context dir.
	// docker build takes exactly one positional arg: the context path.
	// Skip all flags and their values to find it.
	contextDir := "."
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && buildFlagsWithValue[a] {
				skipNext = true
			}
			continue
		}
		contextDir = a
	}

	path := filepath.Join(contextDir, "Dockerfile")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func imagesFromDockerfile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Dockerfile %s: %w", path, err)
	}
	defer f.Close()

	var images []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "FROM ") {
			continue
		}
		// FROM <image> [AS <name>]
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		image := parts[1]
		if strings.EqualFold(image, "scratch") {
			continue
		}
		images = append(images, image)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read Dockerfile %s: %w", path, err)
	}
	return images, nil
}
