package main

import "strings"

// normalizeImage converts a bare image name to a fully-qualified one.
// See cmd/docker/image.go for the same logic used in the CLI shim.
func normalizeImage(image string) string {
	if prefix, _, ok := strings.Cut(image, "/"); ok {
		if strings.ContainsAny(prefix, ".:") || prefix == "localhost" {
			return image
		}
		return "docker.io/" + image
	}
	return "docker.io/library/" + image
}
