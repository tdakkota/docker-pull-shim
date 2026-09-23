package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/go-faster/sdk/cliversion"
)

const modulePath = "github.com/tdakkota/docker-pull-shim"

func buildInfo() cliversion.Info {
	info, _ := cliversion.GetInfo(modulePath)
	if info.Version == "" {
		info.Version = "(devel)"
	}
	return info
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

	info := buildInfo()
	shimEntry := map[string]any{
		"Name":    "docker-pull-shim",
		"Version": info.Version,
	}
	if info.Commit != "" {
		shimEntry["Details"] = map[string]any{"GitCommit": info.Commit}
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
