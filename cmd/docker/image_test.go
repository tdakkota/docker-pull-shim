package main

import "testing"

func TestNormalizeImage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Bare name → docker.io/library/
		{"ubuntu", "docker.io/library/ubuntu"},
		{"alpine", "docker.io/library/alpine"},
		{"alpine:3.21", "docker.io/library/alpine:3.21"},

		// org/repo → docker.io/
		{"library/ubuntu", "docker.io/library/ubuntu"},
		{"myorg/myapp", "docker.io/myorg/myapp"},
		{"myorg/myapp:latest", "docker.io/myorg/myapp:latest"},

		// Already has registry (contains dot before first slash) → unchanged
		{"gcr.io/myproject/myapp", "gcr.io/myproject/myapp"},
		{"ghcr.io/foo/bar:v1", "ghcr.io/foo/bar:v1"},
		{"quay.io/prometheus/prometheus", "quay.io/prometheus/prometheus"},

		// Already has registry (contains colon/port before first slash) → unchanged
		{"registry:5000/myapp", "registry:5000/myapp"},
		{"10.42.0.44:5000/docker.io/library/alpine", "10.42.0.44:5000/docker.io/library/alpine"},

		// docker.io explicit → unchanged (has dot)
		{"docker.io/library/golang:1.26-alpine", "docker.io/library/golang:1.26-alpine"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeImage(tt.input)
			if got != tt.want {
				t.Errorf("normalizeImage(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
