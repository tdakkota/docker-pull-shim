package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// prePull copies the image from the configured mirror into the upstream daemon
// using skopeo (to pull from mirror) + POST /images/load (to inject into daemon).
// Errors are logged but never fatal — the original client request is always forwarded.
func prePull(cfg Config, upstreamSocket string, image string) {
	if cfg.Mirror == "" {
		return
	}

	// Mirror must be a plain host or host:port with no path components or whitespace.
	if strings.ContainsAny(cfg.Mirror, " \t\n/") {
		log.Printf("proxy: invalid mirror address %q: must be host or host:port", cfg.Mirror)
		return
	}
	if strings.ContainsRune(cfg.Mirror, ':') {
		if _, _, err := net.SplitHostPort(cfg.Mirror); err != nil {
			log.Printf("proxy: invalid mirror address %q: %v", cfg.Mirror, err)
			return
		}
	}

	normalized := normalizeImage(image)
	src := fmt.Sprintf("docker://%s/%s", cfg.Mirror, normalized)

	tmp, err := os.CreateTemp("", "img-*.tar")
	if err != nil {
		log.Printf("proxy: failed to create temp file: %v", err)
		return
	}
	tmp.Close()
	defer func() {
		if err := os.Remove(tmp.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("proxy: failed to remove temp file %s: %v", tmp.Name(), err)
		}
	}()

	skopeoArgs := []string{"copy"}
	if !cfg.TLSVerify {
		skopeoArgs = append(skopeoArgs, "--src-tls-verify=false")
	}
	skopeoArgs = append(skopeoArgs, src, "docker-archive:"+tmp.Name())

	log.Printf("proxy: pulling %s from %s/%s", image, cfg.Mirror, normalized)
	skopeo := exec.Command("skopeo", skopeoArgs...)
	skopeo.Stdout = os.Stderr
	skopeo.Stderr = os.Stderr
	if err := skopeo.Run(); err != nil {
		log.Printf("proxy: skopeo failed for %s: %v", image, err)
		return
	}

	log.Printf("proxy: loading %s into daemon", image)
	if err := loadImageAPI(upstreamSocket, tmp.Name()); err != nil {
		log.Printf("proxy: load failed for %s: %v", image, err)
	}
}

// loadImageAPI sends the tar archive at tarPath to the upstream daemon via
// POST /images/load, avoiding any dependency on the docker CLI binary.
func loadImageAPI(upstreamSocket, tarPath string) error {
	f, err := os.Open(tarPath)
	if err != nil {
		return fmt.Errorf("open tar: %w", err)
	}
	defer f.Close()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", upstreamSocket)
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport}

	resp, err := client.Post("http://docker/images/load?quiet=1", "application/x-tar", f)
	if err != nil {
		return fmt.Errorf("POST /images/load: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST /images/load: status %s", resp.Status)
	}
	return nil
}
