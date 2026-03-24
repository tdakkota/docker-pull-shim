package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// prePull copies the image from the configured mirror into the upstream daemon
// using skopeo (to pull from mirror) + POST /images/load (to inject into daemon).
// Errors are logged but never fatal — the original client request is always forwarded.
func prePull(cfg Config, upstreamSocket string, image string) {
	if cfg.Mirror == "" {
		slog.Info("no mirror configured, skipping pre-pull", "image", image)
		return
	}

	// Mirror must be a plain host or host:port with no path components or whitespace.
	if strings.ContainsAny(cfg.Mirror, " \t\n/") {
		slog.Warn("invalid mirror address", "mirror", cfg.Mirror, "reason", "must be host or host:port")
		return
	}
	if strings.ContainsRune(cfg.Mirror, ':') {
		if _, _, err := net.SplitHostPort(cfg.Mirror); err != nil {
			slog.Warn("invalid mirror address", "mirror", cfg.Mirror, "err", err)
			return
		}
	}

	normalized := normalizeImage(image)
	src := fmt.Sprintf("docker://%s/%s", cfg.Mirror, normalized)

	lg := slog.With("image", image)
	lg.Debug("normalized image name", "src", src)

	tmp, err := os.CreateTemp("", "img-*.tar")
	if err != nil {
		lg.Error("create temp file", "err", err)
		return
	}
	tmp.Close()
	defer func() {
		if err := os.Remove(tmp.Name()); err != nil && !errors.Is(err, os.ErrNotExist) {
			lg.Warn("remove temp file", "path", tmp.Name(), "err", err)
		}
	}()

	skopeoArgs := []string{"copy"}
	if !cfg.TLSVerify {
		skopeoArgs = append(skopeoArgs, "--src-tls-verify=false")
	}
	skopeoArgs = append(skopeoArgs, src, "docker-archive:"+tmp.Name())

	lg.Info("pulling image", "mirror", cfg.Mirror)
	startPull := time.Now()
	skopeo := exec.Command("skopeo", skopeoArgs...)
	skopeo.Stdout = os.Stderr
	skopeo.Stderr = os.Stderr
	if err := skopeo.Run(); err != nil {
		lg.Error("skopeo failed", "err", err)
		return
	}
	lg.Debug("pulled image", "took", time.Since(startPull))

	lg.Info("loading image")
	startLoad := time.Now()
	if err := loadImageAPI(upstreamSocket, tmp.Name()); err != nil {
		lg.Error("load image failed", "err", err)
	}
	lg.Info("loaded image", "took", time.Since(startLoad))
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
