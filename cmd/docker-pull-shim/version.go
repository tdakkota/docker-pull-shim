package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
)

// shimBuildInfo returns the module version and git commit hash embedded by
// `go build`. Version is "(devel)" when no module tag is present; GitCommit
// is empty when VCS info was not stamped (e.g. a vendor build or `go run`).
func shimBuildInfo() (version, gitCommit string) {
	version = "(devel)"
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}
	if info.Main.Version != "" {
		version = info.Main.Version
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			gitCommit = s.Value
			break
		}
	}
	return
}

// injectShimVersion reads a JSON GET /version response body, appends a
// "docker-pull-shim" entry to the Components array, and returns a new
// response with an updated body and ContentLength. The original response
// is returned unchanged on any error (non-200, non-JSON, parse failure).
func injectShimVersion(resp *http.Response) *http.Response {
	if resp.StatusCode != http.StatusOK {
		return resp
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		return resp
	}

	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}

	var v map[string]any
	if err := json.Unmarshal(body, &v); err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}

	version, gitCommit := shimBuildInfo()
	shimEntry := map[string]any{
		"Name":    "docker-pull-shim",
		"Version": version,
	}
	if gitCommit != "" {
		shimEntry["Details"] = map[string]any{"GitCommit": gitCommit}
	}
	switch comps := v["Components"].(type) {
	case []any:
		v["Components"] = append(comps, shimEntry)
	default:
		v["Components"] = []any{shimEntry}
	}

	modified, err := json.Marshal(v)
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}

	newResp := *resp
	newResp.Header = resp.Header.Clone()
	newResp.Body = io.NopCloser(bytes.NewReader(modified))
	newResp.ContentLength = int64(len(modified))
	return &newResp
}
