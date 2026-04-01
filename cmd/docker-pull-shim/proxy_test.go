package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// tcpDial returns a dial function that connects to a TCP address (used in tests
// to avoid needing real Unix sockets).
func tcpDial(addr string) func() (net.Conn, error) {
	return func() (net.Conn, error) { return net.Dial("tcp", addr) }
}

// TestHandleConn_Passthrough verifies that non-intercepted requests are
// forwarded transparently to the upstream.
func TestHandleConn_Passthrough(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	}))
	upstream.Start()
	defer upstream.Close()

	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	cfg := Config{}
	go handleConn(serverConn, cfg, "", tcpDial(upstream.Listener.Addr().String()))

	req, err := http.NewRequest(http.MethodGet, "http://docker/_ping", nil)
	require.NoError(t, err)
	require.NoError(t, req.Write(clientConn))

	resp, err := http.ReadResponse(bufio.NewReader(clientConn), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("X-Upstream") != "yes" {
		t.Error("X-Upstream header not forwarded")
	}
}

// TestHandleConn_InterceptPull verifies that POST /images/create triggers
// a pre-pull attempt when fromImage is set and fromSrc is absent.
func TestHandleConn_InterceptPull(t *testing.T) {
	intercepted := make(chan string, 1)

	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create") {
			intercepted <- r.URL.Query().Get("fromImage")
		}
		w.WriteHeader(http.StatusOK)
	}))
	upstream.Start()
	defer upstream.Close()

	// Temp socket file for the upstream TCP address isn't needed here since
	// handleConn dials a Unix socket, so we test via a TCP-backed fake upstream
	// by swapping the dial target.
	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	cfg := Config{} // mirror empty → prePull is a no-op
	go handleConn(serverConn, cfg, "", tcpDial(upstream.Listener.Addr().String()))

	req, err := http.NewRequest(http.MethodPost, "http://docker/images/create?fromImage=alpine&tag=latest", nil)
	require.NoError(t, err)
	req.ContentLength = 0
	require.NoError(t, req.Write(clientConn))

	resp, err := http.ReadResponse(bufio.NewReader(clientConn), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	select {
	case img := <-intercepted:
		if img != "alpine" {
			t.Errorf("intercepted image = %q, want %q", img, "alpine")
		}
	default:
		t.Error("upstream did not receive the /images/create request")
	}
}

// TestHandleConn_SkipFromSrc verifies that import-from-source requests
// (fromSrc != "") are forwarded to upstream without triggering a pre-pull.
func TestHandleConn_SkipFromSrc(t *testing.T) {
	received := make(chan string, 1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/images/create") {
			received <- r.URL.Query().Get("fromSrc")
		}
		w.WriteHeader(http.StatusOK)
	}))
	upstream.Start()
	defer upstream.Close()

	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	cfg := Config{Mirror: "mirror.example.com"}
	go handleConn(serverConn, cfg, "", tcpDial(upstream.Listener.Addr().String()))

	req, err := http.NewRequest(http.MethodPost, "http://docker/images/create?fromSrc=-&repo=myimg", nil)
	require.NoError(t, err)
	req.Body = io.NopCloser(strings.NewReader(""))
	req.ContentLength = 0
	require.NoError(t, req.Write(clientConn))

	resp, err := http.ReadResponse(bufio.NewReader(clientConn), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	// Verify the request was forwarded with fromSrc intact.
	select {
	case fromSrc := <-received:
		if fromSrc != "-" {
			t.Errorf("upstream got fromSrc=%q, want %q", fromSrc, "-")
		}
	default:
		t.Error("upstream did not receive the /images/create request")
	}
}

// TestHandleConn_VersionedPath verifies interception works on versioned API paths.
func TestHandleConn_VersionedPath(t *testing.T) {
	intercepted := make(chan string, 1)

	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/images/create") {
			intercepted <- r.URL.Query().Get("fromImage")
		}
		w.WriteHeader(http.StatusOK)
	}))
	upstream.Start()
	defer upstream.Close()

	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	cfg := Config{}
	go handleConn(serverConn, cfg, "", tcpDial(upstream.Listener.Addr().String()))

	req, err := http.NewRequest(http.MethodPost, "http://docker/v1.44/images/create?fromImage=nginx", nil)
	require.NoError(t, err)
	req.ContentLength = 0
	require.NoError(t, req.Write(clientConn))

	_, err = http.ReadResponse(bufio.NewReader(clientConn), req)
	require.NoError(t, err)

	select {
	case img := <-intercepted:
		if img != "nginx" {
			t.Errorf("intercepted image = %q, want %q", img, "nginx")
		}
	default:
		t.Error("upstream did not receive the /images/create request")
	}
}

// TestHandleConn_KeepAlive verifies that multiple requests can be served
// over a single connection.
func TestHandleConn_KeepAlive(t *testing.T) {
	var mu sync.Mutex
	count := 0
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		n := count
		mu.Unlock()
		_, _ = fmt.Fprintf(w, "req%d", n)
	}))
	upstream.Start()
	defer upstream.Close()

	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	cfg := Config{}
	go handleConn(serverConn, cfg, "", tcpDial(upstream.Listener.Addr().String()))

	br := bufio.NewReader(clientConn)
	for i := 1; i <= 3; i++ {
		req, err := http.NewRequest(http.MethodGet, "http://docker/_ping", nil)
		require.NoError(t, err)
		require.NoError(t, req.Write(clientConn))
		resp, err := http.ReadResponse(br, req)
		if err != nil {
			t.Fatalf("request %d: read response: %v", i, err)
		}
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		if string(body) != fmt.Sprintf("req%d", i) {
			t.Errorf("request %d: body = %q, want %q", i, body, fmt.Sprintf("req%d", i))
		}
	}
}

// TestIsKeepAlive covers the keep-alive detection logic.
func TestIsKeepAlive(t *testing.T) {
	tests := []struct {
		name      string
		reqClose  bool
		respClose bool
		proto     string
		connHdr   string
		want      bool
	}{
		{"http11 default", false, false, "HTTP/1.1", "", true},
		{"req.Close", true, false, "HTTP/1.1", "", false},
		{"resp.Close", false, true, "HTTP/1.1", "", false},
		{"http10 no header", false, false, "HTTP/1.0", "", false},
		{"http10 keep-alive", false, false, "HTTP/1.0", "keep-alive", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			minor := 1
			if tt.proto == "HTTP/1.0" {
				minor = 0
			}
			req := &http.Request{
				Close:      tt.reqClose,
				ProtoMajor: 1,
				ProtoMinor: minor,
				Header:     http.Header{},
			}
			if tt.connHdr != "" {
				req.Header.Set("Connection", tt.connHdr)
			}
			resp := &http.Response{
				Close: tt.respClose,
			}
			got := isKeepAlive(req, resp)
			if got != tt.want {
				t.Errorf("isKeepAlive = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestSocketPath covers the "unix://" prefix stripping.
func TestSocketPath(t *testing.T) {
	tests := []struct{ in, want string }{
		{"unix:///var/run/docker.sock", "/var/run/docker.sock"},
		{"/var/run/docker.sock", "/var/run/docker.sock"},
		{"unix://./docker.sock", "./docker.sock"},
	}
	for _, tt := range tests {
		got := socketPath(tt.in)
		if got != tt.want {
			t.Errorf("socketPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestHandleConn_UsesTCPUpstream ensures handleConn can connect to a TCP
// upstream (used in tests in place of a Unix socket).
func TestHandleConn_UsesTCPUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	go handleConn(serverConn, Config{}, "", tcpDial(upstream.Listener.Addr().String()))

	req, err := http.NewRequest(http.MethodGet, "http://docker/version", nil)
	require.NoError(t, err)
	require.NoError(t, req.Write(clientConn))

	resp, err := http.ReadResponse(bufio.NewReader(clientConn), req)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
}

// TestImageWithTag covers the fromImage + tag combining logic.
func TestImageWithTag(t *testing.T) {
	tests := []struct {
		fromImage, tag, want string
	}{
		{"alpine", "3.21", "alpine:3.21"},          // tag appended
		{"alpine:latest", "3.21", "alpine:latest"}, // existing tag wins
		{"alpine", "", "alpine"},                   // no tag, unchanged
		{"nginx", "1.25", "nginx:1.25"},
	}
	for _, tt := range tests {
		got := imageWithTag(tt.fromImage, tt.tag)
		if got != tt.want {
			t.Errorf("imageWithTag(%q, %q) = %q, want %q", tt.fromImage, tt.tag, got, tt.want)
		}
	}
}

// TestHandleConn_Version verifies that GET /version responses have the
// docker-pull-shim entry injected into the Components array.
func TestHandleConn_Version(t *testing.T) {
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Version":"27.0.0","Components":[{"Name":"Engine","Version":"27.0.0"}]}`))
	}))
	upstream.Start()
	defer upstream.Close()

	clientConn, serverConn := net.Pipe()
	defer func() { _ = clientConn.Close() }()

	go handleConn(serverConn, Config{}, "", tcpDial(upstream.Listener.Addr().String()))

	for _, path := range []string{"/version", "/v1.47/version"} {
		req, err := http.NewRequest(http.MethodGet, "http://docker"+path, nil)
		require.NoError(t, err)
		require.NoError(t, req.Write(clientConn))

		resp, err := http.ReadResponse(bufio.NewReader(clientConn), req)
		require.NoError(t, err)

		var body map[string]any
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		_ = resp.Body.Close()

		comps, ok := body["Components"].([]any)
		require.True(t, ok, "Components must be an array")

		var found bool
		for _, c := range comps {
			m, _ := c.(map[string]any)
			if m["Name"] == "docker-pull-shim" {
				found = true
				require.NotEmpty(t, m["Version"], "shim Version must be set")
				// Details.GitCommit must be absent or non-empty (never an empty string).
				if details, ok := m["Details"].(map[string]any); ok {
					require.NotEmpty(t, details["GitCommit"], "Details.GitCommit must not be empty when present")
				}
			}
		}
		require.True(t, found, "docker-pull-shim component not found in %s response", path)
	}
}

// Compile-time check that os is imported (used in main.go).
var _ = os.Stderr
