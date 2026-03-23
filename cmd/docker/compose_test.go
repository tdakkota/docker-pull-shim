package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestComposeFilePath(t *testing.T) {
	t.Run("explicit -f flag", func(t *testing.T) {
		got := composeFilePath([]string{"-f", "/my/compose.yml", "up"})
		if got != "/my/compose.yml" {
			t.Errorf("got %q, want /my/compose.yml", got)
		}
	})

	t.Run("explicit --file flag", func(t *testing.T) {
		got := composeFilePath([]string{"--file", "/my/compose.yml", "up"})
		if got != "/my/compose.yml" {
			t.Errorf("got %q, want /my/compose.yml", got)
		}
	})

	t.Run("--file= equals form", func(t *testing.T) {
		got := composeFilePath([]string{"--file=/my/compose.yml", "up"})
		if got != "/my/compose.yml" {
			t.Errorf("got %q, want /my/compose.yml", got)
		}
	})

	t.Run("-f= equals form", func(t *testing.T) {
		got := composeFilePath([]string{"-f=/my/compose.yml", "up"})
		if got != "/my/compose.yml" {
			t.Errorf("got %q, want /my/compose.yml", got)
		}
	})

	t.Run("default compose.yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "compose.yaml")
		if err := os.WriteFile(path, []byte("services: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		// Change to the temp dir so the default search finds the file.
		orig, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(orig) })

		got := composeFilePath([]string{"up"})
		if got != "compose.yaml" {
			t.Errorf("got %q, want compose.yaml", got)
		}
	})

	t.Run("no file found", func(t *testing.T) {
		dir := t.TempDir()
		orig, _ := os.Getwd()
		if err := os.Chdir(dir); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chdir(orig) })

		got := composeFilePath([]string{"up"})
		if got != "" {
			t.Errorf("got %q, want empty string", got)
		}
	})
}

func TestImagesFromComposeFile(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name: "single service with image",
			content: `
services:
  web:
    image: nginx:latest
`,
			want: []string{"nginx:latest"},
		},
		{
			name: "multiple services",
			content: `
services:
  web:
    image: nginx:latest
  db:
    image: postgres:16
  cache:
    image: redis:7-alpine
`,
			want: []string{"nginx:latest", "postgres:16", "redis:7-alpine"},
		},
		{
			name: "service with only build (no image) is skipped",
			content: `
services:
  app:
    build: .
  db:
    image: postgres:16
`,
			want: []string{"postgres:16"},
		},
		{
			name: "service with both build and image",
			content: `
services:
  app:
    build: .
    image: myapp:latest
`,
			want: []string{"myapp:latest"},
		},
		{
			name:    "no services",
			content: "version: \"3\"\n",
			want:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "compose*.yml")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteString(tt.content); err != nil {
				t.Fatal(err)
			}
			f.Close()

			got, err := imagesFromComposeFile(f.Name())
			if err != nil {
				t.Fatalf("imagesFromComposeFile: %v", err)
			}

			// Sort both slices for deterministic comparison (map iteration order).
			sort.Strings(got)
			want := make([]string, len(tt.want))
			copy(want, tt.want)
			sort.Strings(want)

			if len(got) != len(want) {
				t.Fatalf("got %v, want %v", got, want)
			}
			for i := range got {
				if got[i] != want[i] {
					t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
				}
			}
		})
	}
}
