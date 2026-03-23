package main

import "strings"

// normalizeImage converts a bare image name to a fully-qualified one:
//   - "ubuntu"          → "docker.io/library/ubuntu"
//   - "alpine:3.21"     → "docker.io/library/alpine:3.21"
//   - "library/ubuntu"  → "docker.io/library/ubuntu"
//   - "gcr.io/foo/bar"  → "gcr.io/foo/bar"  (unchanged)
//   - "registry:5000/x" → "registry:5000/x" (unchanged)
//
// A registry prefix is detected by checking whether the component before the
// first "/" contains a "." or ":" (port/domain) or equals "localhost".
// This correctly distinguishes "alpine:3.21" (bare image + tag, no slash) from
// "registry:5000/repo" (registry with port).
func normalizeImage(image string) string {
	if prefix, _, ok := strings.Cut(image, "/"); ok {
		if strings.ContainsAny(prefix, ".:") || prefix == "localhost" {
			return image // already has a registry host
		}
		return "docker.io/" + image
	}
	return "docker.io/library/" + image
}
