package main

import "testing"

func TestFindComposeSubcmd(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"subcommand only", []string{"up"}, "up"},
		{"pull subcommand", []string{"pull"}, "pull"},
		{"-f before subcommand", []string{"-f", "my.yml", "up"}, "up"},
		{"--file before subcommand", []string{"--file", "my.yml", "up"}, "up"},
		{"--file= before subcommand", []string{"--file=my.yml", "up"}, "up"},
		{"--project-name before subcommand", []string{"--project-name", "myproj", "up"}, "up"},
		{"multiple flags before subcommand", []string{"-p", "myproj", "-f", "my.yml", "up"}, "up"},
		{"no subcommand", []string{"-f", "my.yml"}, ""},
		{"empty", []string{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findComposeSubcmd(tt.args)
			if got != tt.want {
				t.Errorf("findComposeSubcmd(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestExtractPullImage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"simple", []string{"alpine"}, "alpine"},
		{"with tag", []string{"alpine:3.21"}, "alpine:3.21"},
		{"with platform flag", []string{"--platform", "linux/arm64", "alpine"}, "alpine"},
		{"with platform equals form", []string{"--platform=linux/amd64", "alpine"}, "alpine"},
		{"with quiet flag", []string{"-q", "alpine"}, "alpine"},
		{"empty", []string{}, ""},
		{"only flags", []string{"--platform", "linux/amd64"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPullImage(tt.args)
			if got != tt.want {
				t.Errorf("extractPullImage(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestExtractRunImage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"simple image", []string{"alpine"}, "alpine"},
		{"image with tag", []string{"alpine:3.21", "sh"}, "alpine:3.21"},
		{"with name flag", []string{"--name", "mycontainer", "alpine"}, "alpine"},
		{"with env flag", []string{"-e", "FOO=bar", "alpine", "sh"}, "alpine"},
		{"with env equals form", []string{"-e=FOO=bar", "alpine"}, "alpine"},
		{"with network flag", []string{"--network", "host", "alpine"}, "alpine"},
		{"with detach flag", []string{"-d", "alpine"}, "alpine"},
		{"with rm flag", []string{"--rm", "alpine", "echo", "hi"}, "alpine"},
		{"with volume flag", []string{"-v", "/tmp:/tmp", "alpine"}, "alpine"},
		{"multiple flags", []string{"--rm", "-d", "--name", "c1", "-e", "X=1", "nginx:latest"}, "nginx:latest"},
		{"platform flag", []string{"--platform", "linux/arm64", "alpine"}, "alpine"},
		{"empty", []string{}, ""},
		{"fully qualified image", []string{"gcr.io/myproject/myapp:v1"}, "gcr.io/myproject/myapp:v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractRunImage(tt.args)
			if got != tt.want {
				t.Errorf("extractRunImage(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
