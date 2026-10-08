package coder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Sandbox runs `go test` on model-written code under macOS's sandbox
// (sandbox-exec): no network at all, and writes only inside the exercise
// copy and the Go build cache. Reads are not restricted (the toolchain needs
// them), but with no network nothing read can leave the machine. The
// environment is emptied except for what the toolchain needs, so no key in
// the caller's environment reaches the code.
type Sandbox struct {
	// CacheDir is the Go build cache shared across runs (default: the
	// user's cache directory, metabolism/coder-gocache).
	CacheDir string
}

// profile is the sandbox: everything the default allows, minus the network
// and minus writes outside the allowed directories.
const profile = `(version 1)
(allow default)
(deny network*)
(deny file-write*)
(allow file-write*
  (subpath (param "WORK"))
  (subpath (param "CACHE"))
  (literal "/dev/null")
  (literal "/dev/dtracehelper")
  (regex #"^/dev/tty"))
`

// ErrNoSandbox: the code cannot be run safely on this machine.
var ErrNoSandbox = errors.New("coder: running model-written code needs macOS sandbox-exec")

// Test implements Runner.
func (s Sandbox) Test(ctx context.Context, dir string, timeout time.Duration) (bool, string, error) {
	if runtime.GOOS != "darwin" {
		return false, "", ErrNoSandbox
	}
	sbx, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return false, "", ErrNoSandbox
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return false, "", fmt.Errorf("coder: no go toolchain: %w", err)
	}
	cache := s.CacheDir
	if cache == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return false, "", err
		}
		cache = filepath.Join(base, "metabolism", "coder-gocache")
	}
	if err := os.MkdirAll(cache, 0o700); err != nil {
		return false, "", err
	}
	// The sandbox matches real paths: /var is /private/var on macOS.
	work, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false, "", err
	}
	if cache, err = filepath.EvalSymlinks(cache); err != nil {
		return false, "", err
	}
	tmp := filepath.Join(work, ".tmp")
	if err := os.MkdirAll(tmp, 0o700); err != nil {
		return false, "", err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, sbx, "-p", profile, "-D", "WORK="+work, "-D", "CACHE="+cache,
		goBin, "test", "-count=1", "./...")
	cmd.Dir = work
	cmd.Env = []string{
		"PATH=" + filepath.Dir(goBin) + ":/usr/bin:/bin",
		"HOME=" + work,
		"GOCACHE=" + filepath.Join(cache, "build"),
		"GOMODCACHE=" + filepath.Join(cache, "mod"),
		"GOPATH=" + filepath.Join(cache, "path"),
		"GOTMPDIR=" + tmp,
		"TMPDIR=" + tmp,
		"GOFLAGS=-mod=mod",
		"GOPROXY=off",
		"GOTOOLCHAIN=local",
		"GOTELEMETRY=off",
		"CGO_ENABLED=0",
	}
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err = cmd.Run()
	text := out.String()
	if ctx.Err() == context.DeadlineExceeded {
		return false, "the tests timed out after " + timeout.String() + "\n" + text, nil
	}
	var exit *exec.ExitError
	switch {
	case err == nil:
		return true, text, nil
	case errors.As(err, &exit):
		if strings.Contains(text, "sandbox-exec:") {
			return false, "", fmt.Errorf("coder: the sandbox refused to start: %s", strings.TrimSpace(text))
		}
		return false, text, nil
	}
	return false, text, err
}
