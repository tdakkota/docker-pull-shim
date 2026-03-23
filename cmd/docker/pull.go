package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

// prePull copies image from the configured mirror registry using skopeo,
// loads it into Docker, then removes the temporary file.
// Errors are logged but not fatal — the original docker command will still run.
func prePull(cfg Config, realDocker string, image string) {
	if cfg.Mirror == "" {
		return
	}

	// Mirror must be a plain host or host:port with no path components or whitespace.
	if strings.ContainsAny(cfg.Mirror, " \t\n/") {
		log.Printf("shim: invalid mirror address %q: must be host or host:port", cfg.Mirror)
		return
	}

	normalized := normalizeImage(image)
	src := fmt.Sprintf("docker://%s/%s", cfg.Mirror, normalized)

	tmp, err := os.CreateTemp("", "img-*.tar")
	if err != nil {
		log.Printf("shim: failed to create temp file: %v", err)
		return
	}
	tmp.Close()
	defer func() {
		if err := os.Remove(tmp.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("shim: failed to remove temp file %s: %v", tmp.Name(), err)
		}
	}()

	dst := "docker-archive:" + tmp.Name()
	skopeoArgs := []string{"copy"}
	if !cfg.TLSVerify {
		skopeoArgs = append(skopeoArgs, "--src-tls-verify=false")
	}
	skopeoArgs = append(skopeoArgs, src, dst)

	log.Printf("shim: pulling %s from %s/%s", image, cfg.Mirror, normalized)
	skopeo := exec.Command("skopeo", skopeoArgs...)
	// Route subprocess output to stderr so it doesn't corrupt stdout of callers
	// that capture docker's stdout (e.g. docker inspect piped to jq).
	skopeo.Stdout = os.Stderr
	skopeo.Stderr = os.Stderr
	if err := skopeo.Run(); err != nil {
		log.Printf("shim: skopeo failed for %s: %v", image, err)
		return
	}

	log.Printf("shim: loading %s", image)
	load := exec.Command(realDocker, "load", "-i", tmp.Name())
	load.Stdout = os.Stderr // same as skopeo: keep stdout clean
	load.Stderr = os.Stderr
	if err := load.Run(); err != nil {
		log.Printf("shim: docker load failed for %s: %v", image, err)
	}
}

// prePullAll calls prePull for each image in the slice.
func prePullAll(cfg Config, realDocker string, images []string) {
	seen := make(map[string]struct{}, len(images))
	for _, img := range images {
		if img == "" {
			continue
		}
		if _, ok := seen[img]; ok {
			continue
		}
		seen[img] = struct{}{}
		prePull(cfg, realDocker, img)
	}
}
