package version

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

const devVersion = "dev"

// Version is the application version, resolved in this order:
//
//  1. Release/Makefile builds: -ldflags "-X GoTodo/internal/version.Version=v1.2.3"
//  2. go install module@v1.2.3: module build info
//  3. Running from a git checkout: latest tag reachable from HEAD
//     (so git pull on a release commit reports that tag, not "dev")
//  4. Otherwise "dev"
var Version = devVersion

func init() {
	if Version != devVersion {
		return
	}
	// Stay on the placeholder under `go test` so CI/local suites are deterministic.
	if testing.Testing() {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := versionFromBuildInfo(info); v != "" {
			Version = v
			return
		}
	}
	if v := versionFromGit(); v != "" {
		Version = v
	}
}

func versionFromBuildInfo(info *debug.BuildInfo) string {
	if info == nil {
		return ""
	}
	return parseGitDescribeOutput([]byte(info.Main.Version))
}

func versionFromGit() string {
	dir := findGitRoot()
	if dir == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "describe", "--tags", "--abbrev=0")
	cmd.Dir = dir
	cmd.Stderr = io.Discard
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return parseGitDescribeOutput(out)
}

func parseGitDescribeOutput(out []byte) string {
	v := string(bytes.TrimSpace(out))
	if v == "" || strings.ContainsAny(v, " \t\n\r") {
		return ""
	}
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if len(v) < 2 || v[1] < '0' || v[1] > '9' {
		return ""
	}
	return v
}

func findGitRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for i := 0; i < 12; i++ {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
