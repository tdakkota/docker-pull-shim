package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

func main() {
	logLevelFlag := flag.String("log-level", "info", "log level: debug, info, warn, error")
	flag.Parse()

	var level slog.Level
	if err := level.UnmarshalText([]byte(*logLevelFlag)); err != nil {
		fmt.Fprintf(os.Stderr, "proxy: invalid -log-level %q: %v\n", *logLevelFlag, err)
		os.Exit(1)
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler).With("component", "proxy"))

	cfg, err := loadConfig()
	if err != nil {
		slog.Warn("config error", "err", err)
	}

	upstreamPath, err := chooseUpstream(cfg)
	if err != nil {
		slog.Error("cannot determine upstream socket", "err", err)
		os.Exit(1)
	}

	listenPath, err := chooseListen(cfg, upstreamPath)
	if err != nil {
		slog.Error("cannot determine listen socket", "err", err)
		os.Exit(1)
	}

	// Remove stale socket file from a previous run.
	if err := os.Remove(listenPath); err != nil && !os.IsNotExist(err) {
		slog.Error("remove existing socket", "path", listenPath, "err", err)
		os.Exit(1)
	}

	ln, err := net.Listen("unix", listenPath)
	if err != nil {
		slog.Error("listen failed", "path", listenPath, "err", err)
		os.Exit(1)
	}
	defer func() { _ = ln.Close() }()

	// Clean up socket on exit.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		_ = ln.Close()
		_ = os.Remove(listenPath)
		os.Exit(0)
	}()

	slog.Info("listening", "listen", listenPath, "upstream", upstreamPath)

	dialUpstream := func() (net.Conn, error) {
		return net.Dial("unix", upstreamPath)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			// Listener was closed (e.g. on signal).
			return
		}
		go handleConn(conn, cfg, upstreamPath, dialUpstream)
	}
}

func handleConn(clientConn net.Conn, cfg Config, upstreamSocket string, dialUpstream func() (net.Conn, error)) {
	defer func() { _ = clientConn.Close() }()

	upConn, err := dialUpstream()
	if err != nil {
		slog.Error("dial upstream", "err", err)
		return
	}
	defer func() { _ = upConn.Close() }()

	clientBuf := bufio.NewReader(clientConn)
	upBuf := bufio.NewReader(upConn)

	for {
		req, err := http.ReadRequest(clientBuf)
		if err != nil {
			return
		}
		slog.Debug("request", "method", req.Method, "path", req.URL.Path)

		// Intercept POST /images/create — the Docker pull endpoint.
		// Handle both bare (/images/create) and versioned (/v1.xx/images/create) paths.
		if req.Method == http.MethodPost && strings.HasSuffix(req.URL.Path, "/images/create") {
			fromImage := req.URL.Query().Get("fromImage")
			fromSrc := req.URL.Query().Get("fromSrc")
			// fromSrc non-empty means import-from-url/stdin, not a registry pull.
			if fromImage != "" && fromSrc == "" {
				image := imageWithTag(fromImage, req.URL.Query().Get("tag"))
				slog.Info("intercepted pull", "image", image)
				prePull(cfg, upstreamSocket, image)
			}
		}

		// Forward request to upstream daemon.
		if err := req.Write(upConn); err != nil {
			slog.Info("write to upstream", "err", err)
			return
		}

		resp, err := http.ReadResponse(upBuf, req)
		if err != nil {
			slog.Info("read from upstream", "err", err)
			return
		}

		// 101 Switching Protocols = HTTP hijack (exec, attach, resize).
		// Flush the initial response and then tunnel raw bytes bidirectionally.
		if resp.StatusCode == http.StatusSwitchingProtocols {
			if err := resp.Write(clientConn); err != nil {
				_ = resp.Body.Close()
				return
			}
			_ = resp.Body.Close()
			done := make(chan struct{}, 2)
			go func() {
				_, _ = io.Copy(upConn, clientBuf)
				halfClose(upConn) // client closed its write side; tell the daemon
				done <- struct{}{}
			}()
			go func() {
				_, _ = io.Copy(clientConn, upBuf)
				halfClose(clientConn) // daemon closed its write side; tell the client
				done <- struct{}{}
			}()
			<-done
			<-done // wait for both directions
			return
		}

		writeErr := resp.Write(clientConn)
		_ = resp.Body.Close()
		if writeErr != nil {
			return
		}

		if !isKeepAlive(req, resp) {
			return
		}
	}
}

// closeWriter is implemented by *net.TCPConn and *net.UnixConn.
type closeWriter interface {
	CloseWrite() error
}

// halfClose signals end-of-stream on the write side of c without fully closing
// it. The peer sees EOF on its read side while in-flight data in the other
// direction can still drain. Falls back to a no-op for connection types that
// do not support half-close (e.g. net.Pipe used in tests).
func halfClose(c net.Conn) {
	if cw, ok := c.(closeWriter); ok {
		_ = cw.CloseWrite()
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
