package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImagesFromDockerfile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "single FROM",
			content: "FROM alpine:3.21\nRUN echo hi\n",
			want:    []string{"alpine:3.21"},
		},
		{
			name:    "multi-stage build",
			content: "FROM golang:1.26-alpine AS builder\nRUN go build\nFROM alpine:3.21\nCOPY --from=builder /app /app\n",
			want:    []string{"golang:1.26-alpine", "alpine:3.21"},
		},
		{
			name:    "scratch is skipped",
			content: "FROM golang:1.26 AS builder\nFROM scratch\nCOPY --from=builder /app /app\n",
			want:    []string{"golang:1.26"},
		},
		{
			name:    "case-insensitive FROM",
			content: "from ubuntu:22.04\nRUN apt-get update\n",
			want:    []string{"ubuntu:22.04"},
		},
		{
			name:    "FROM with AS clause",
			content: "FROM node:20 AS frontend\n",
			want:    []string{"node:20"},
		},
		{
			name:    "no FROM lines",
			content: "RUN echo hello\n",
			want:    nil,
		},
		{
			name:    "comments and blank lines",
			content: "# syntax=docker/dockerfile:1\n\nFROM alpine\n",
			want:    []string{"alpine"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "Dockerfile")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tt.content); err != nil {
				t.Fatal(err)
			}
			f.Close()

			got, err := imagesFromDockerfile(f.Name())
			if err != nil {
				t.Fatalf("imagesFromDockerfile: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestDockerfilePath(t *testing.T) {
	t.Run("explicit -f flag", func(t *testing.T) {
		got := dockerfilePath([]string{"-f", "/my/Dockerfile", "."})
		if got != "/my/Dockerfile" {
			t.Errorf("got %q, want /my/Dockerfile", got)
		}
	})

	t.Run("explicit --file flag", func(t *testing.T) {
		got := dockerfilePath([]string{"--file", "/my/Dockerfile", "."})
		if got != "/my/Dockerfile" {
			t.Errorf("got %q, want /my/Dockerfile", got)
		}
	})

	t.Run("--file= equals form", func(t *testing.T) {
		got := dockerfilePath([]string{"--file=/my/Dockerfile", "."})
		if got != "/my/Dockerfile" {
			t.Errorf("got %q, want /my/Dockerfile", got)
		}
	})

	t.Run("default Dockerfile in context dir", func(t *testing.T) {
		dir := t.TempDir()
		dfPath := filepath.Join(dir, "Dockerfile")
		if err := os.WriteFile(dfPath, []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got := dockerfilePath([]string{dir})
		if got != dfPath {
			t.Errorf("got %q, want %q", got, dfPath)
		}
	})

	t.Run("context dir found despite --target flag consuming a value", func(t *testing.T) {
		dir := t.TempDir()
		dfPath := filepath.Join(dir, "Dockerfile")
		if err := os.WriteFile(dfPath, []byte("FROM alpine\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Without the fix, "--target" would consume "builder" and "." would be
		// picked as context, but "foo" might be incorrectly picked instead.
		got := dockerfilePath([]string{"-t", "myapp:latest", "--target", "builder", dir})
		if got != dfPath {
			t.Errorf("got %q, want %q", got, dfPath)
		}
	})
}
