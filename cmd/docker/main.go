package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Printf("shim: config error: %v", err)
	}

	realDocker, err := findRealDocker()
	if err != nil {
		log.Fatalf("shim: cannot find real docker: %v", err)
	}

	args := os.Args[1:]
	if len(args) > 0 {
		switch args[0] {
		case "pull":
			if img := extractPullImage(args[1:]); img != "" {
				prePullAll(cfg, realDocker, []string{img})
			}

		case "run":
			if img := extractRunImage(args[1:]); img != "" {
				prePullAll(cfg, realDocker, []string{img})
			}

		case "build":
			images, err := imagesFromDockerfileArgs(args[1:])
			if err != nil {
				log.Printf("shim: build image extraction: %v", err)
			}
			prePullAll(cfg, realDocker, images)

		case "compose":
			subArgs := args[1:]
			if sub := findComposeSubcmd(subArgs); sub == "up" || sub == "pull" {
				images, err := imagesFromComposeArgs(subArgs)
				if err != nil {
					log.Printf("shim: compose image extraction: %v", err)
				}
				prePullAll(cfg, realDocker, images)
			}
		}
	}

	// Replace this process with the real docker, passing all original args.
	// Use "docker" as argv[0] (basename convention) rather than the full path.
	if err := syscall.Exec(realDocker, append([]string{"docker"}, args...), os.Environ()); err != nil {
		log.Fatalf("shim: exec %s: %v", realDocker, err)
	}
}

// findRealDocker locates the real docker binary by walking PATH and skipping
// the current executable.
func findRealDocker() (string, error) {
	self, _ := os.Executable()
	// Resolve symlinks so we can compare reliably.
	selfResolved, _ := filepath.EvalSymlinks(self)

	pathEnv := os.Getenv("PATH")
	for _, dir := range filepath.SplitList(pathEnv) {
		candidate := filepath.Join(dir, "docker")
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		// Skip if it's us.
		if candidate == self || candidate == selfResolved {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil && (resolved == self || resolved == selfResolved) {
			continue
		}
		return candidate, nil
	}
	return "", os.ErrNotExist
}

// composeFlagsWithValue is the set of docker compose global flags that consume the next argument.
var composeFlagsWithValue = map[string]bool{
	"--file": true, "-f": true,
	"--project-name": true, "-p": true,
	"--profile": true, "--env-file": true,
	"--project-directory": true,
	"--ansi": true, "--progress": true, "--parallel": true,
}

// findComposeSubcmd returns the first non-flag positional argument in the
// args following "compose" — that is, the compose subcommand (e.g. "up", "pull").
func findComposeSubcmd(args []string) string {
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && composeFlagsWithValue[a] {
				skipNext = true
			}
			continue
		}
		return a
	}
	return ""
}

// pullFlagsWithValue is the set of docker pull flags that consume the next argument.
var pullFlagsWithValue = map[string]bool{
	"--platform": true,
}

// extractPullImage returns the image from "docker pull [options] <image>" args.
// args is everything after "pull".
func extractPullImage(args []string) string {
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			if !strings.Contains(a, "=") && pullFlagsWithValue[a] {
				skipNext = true
			}
			continue
		}
		return a
	}
	return ""
}

// runFlagsWithValue is the set of docker run flags that consume the next argument.
var runFlagsWithValue = map[string]bool{
	"--add-host": true, "--annotation": true, "--attach": true, "-a": true,
	"--blkio-weight": true, "--blkio-weight-device": true,
	"--cap-add": true, "--cap-drop": true,
	"--cgroup-parent": true, "--cgroupns": true, "--cidfile": true,
	"--cpu-period": true, "--cpu-quota": true, "--cpu-rt-period": true,
	"--cpu-rt-runtime": true, "--cpu-shares": true, "-c": true,
	"--cpus": true, "--cpuset-cpus": true, "--cpuset-mems": true,
	"--device": true, "--device-cgroup-rule": true, "--device-read-bps": true,
	"--device-read-iops": true, "--device-write-bps": true, "--device-write-iops": true,
	"--dns": true, "--dns-option": true, "--dns-search": true,
	"--domainname": true, "--entrypoint": true,
	"--env": true, "-e": true, "--env-file": true,
	"--expose": true, "--gpus": true, "--group-add": true,
	"--health-cmd": true, "--health-interval": true, "--health-retries": true,
	"--health-start-period": true, "--health-timeout": true,
	"--hostname": true, "-h": true,
	"--ip": true, "--ip6": true, "--ipc": true,
	"--isolation": true, "--kernel-memory": true,
	"--label": true, "-l": true, "--label-file": true,
	"--link": true, "--link-local-ip": true,
	"--log-driver": true, "--log-opt": true,
	"--mac-address": true, "--memory": true, "-m": true,
	"--memory-reservation": true, "--memory-swap": true, "--memory-swappiness": true,
	"--mount": true, "--name": true, "--network": true, "--network-alias": true,
	"--pid": true, "--pids-limit": true, "--platform": true,
	"--publish": true, "-p": true, "--restart": true,
	"--runtime": true, "--security-opt": true, "--shm-size": true,
	"--stop-signal": true, "--stop-timeout": true, "--storage-opt": true,
	"--sysctl": true, "--tmpfs": true, "--ulimit": true,
	"--user": true, "-u": true, "--userns": true, "--uts": true,
	"--volume": true, "-v": true, "--volume-driver": true, "--volumes-from": true,
	"--workdir": true, "-w": true,
}

// extractRunImage returns the image from "docker run [options] <image> [cmd]" args.
// args is everything after "run".
func extractRunImage(args []string) string {
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if strings.HasPrefix(a, "-") {
			// Check for --flag=value form (no next arg to skip).
			if strings.Contains(a, "=") {
				continue
			}
			if runFlagsWithValue[a] {
				skipNext = true
			}
			continue
		}
		return a
	}
	return ""
}
