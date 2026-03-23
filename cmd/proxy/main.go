package main

import (
	"bufio"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Printf("proxy: config error: %v", err)
	}

	listenPath := socketPath(cfg.Listen)
	upstreamPath := socketPath(cfg.Upstream)

	// Remove stale socket file from a previous run.
	if err := os.Remove(listenPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("proxy: remove existing socket %s: %v", listenPath, err)
	}

	ln, err := net.Listen("unix", listenPath)
	if err != nil {
		log.Fatalf("proxy: listen %s: %v", listenPath, err)
	}
	defer ln.Close()

	// Clean up socket on exit.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		ln.Close()
		os.Remove(listenPath)
		os.Exit(0)
	}()

	log.Printf("proxy: listening on %s → %s", cfg.Listen, cfg.Upstream)

	dialUpstream := func() (net.Conn, error) {
		return net.Dial("unix", upstreamPath)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener was closed (e.g. on signal).
			return
		}
		go handleConn(conn, cfg, dialUpstream)
	}
}

func handleConn(clientConn net.Conn, cfg Config, dialUpstream func() (net.Conn, error)) {
	defer clientConn.Close()

	upConn, err := dialUpstream()
	if err != nil {
		log.Printf("proxy: dial upstream: %v", err)
		return
	}
	defer upConn.Close()

	clientBuf := bufio.NewReader(clientConn)
	upBuf := bufio.NewReader(upConn)

	for {
		req, err := http.ReadRequest(clientBuf)
		if err != nil {
			return
		}

		// Intercept POST /images/create — the Docker pull endpoint.
		// Handle both bare (/images/create) and versioned (/v1.xx/images/create) paths.
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/images/create") {
			fromImage := req.URL.Query().Get("fromImage")
			fromSrc := req.URL.Query().Get("fromSrc")
			// fromSrc non-empty means import-from-url/stdin, not a registry pull.
			if fromImage != "" && fromSrc == "" {
				prePull(cfg, socketPath(cfg.Upstream), imageWithTag(fromImage, req.URL.Query().Get("tag")))
			}
		}

		// Forward request to upstream daemon.
		if err := req.Write(upConn); err != nil {
			log.Printf("proxy: write request to upstream: %v", err)
			return
		}

		resp, err := http.ReadResponse(upBuf, req)
		if err != nil {
			log.Printf("proxy: read response from upstream: %v", err)
			return
		}

		// 101 Switching Protocols = HTTP hijack (exec, attach, resize).
		// Flush the initial response and then tunnel raw bytes bidirectionally.
		if resp.StatusCode == http.StatusSwitchingProtocols {
			if err := resp.Write(clientConn); err != nil {
				resp.Body.Close()
				return
			}
			resp.Body.Close()
			done := make(chan struct{}, 2)
			go func() { io.Copy(upConn, clientConn); done <- struct{}{} }()
			go func() { io.Copy(clientConn, upConn); done <- struct{}{} }()
			<-done
			<-done // wait for both directions
			return
		}

		writeErr := resp.Write(clientConn)
		resp.Body.Close()
		if writeErr != nil {
			return
		}

		if !isKeepAlive(req, resp) {
			return
		}
	}
}

// imageWithTag returns fromImage combined with tag when the image has no tag separator.
func imageWithTag(fromImage, tag string) string {
	if tag != "" && !strings.ContainsRune(fromImage, ':') {
		return fromImage + ":" + tag
	}
	return fromImage
}

// isKeepAlive reports whether the connection should be reused after this exchange.
func isKeepAlive(req *http.Request, resp *http.Response) bool {
	if req.Close || resp.Close {
		return false
	}
	// HTTP/1.0 is not keep-alive by default unless explicitly requested.
	if req.ProtoMajor == 1 && req.ProtoMinor == 0 {
		return strings.EqualFold(req.Header.Get("Connection"), "keep-alive")
	}
	return true
}
