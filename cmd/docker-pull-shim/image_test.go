package main

import "testing"

func TestNormalizeImage(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ubuntu", "docker.io/library/ubuntu"},
		{"alpine:3.21", "docker.io/library/alpine:3.21"},
		{"library/ubuntu", "docker.io/library/ubuntu"},
		{"myorg/myapp:latest", "docker.io/myorg/myapp:latest"},
		{"gcr.io/myproject/myapp", "gcr.io/myproject/myapp"},
		{"registry:5000/myapp", "registry:5000/myapp"},
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
