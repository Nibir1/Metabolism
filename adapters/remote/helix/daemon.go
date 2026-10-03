package helix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Daemon runs `helix daemon` for a driven session (D-023): the synthetic
// user's requests go to it through `helix remote submit`, exactly as a
// person's would, and are recorded by Helix's own recorder.
//
// It is here, under adapters/remote, because every submitted request is a
// planner call to Helix's configured model provider.
type Daemon struct {
	// Command is the helix binary (default "helix" on PATH).
	Command string
	// Log receives the daemon's output (default: discarded).
	Log io.Writer

	bin  string
	cmd  *exec.Cmd
	done chan error
}

// Start runs the daemon and waits until it answers, at most ready.
func (d *Daemon) Start(ctx context.Context, ready time.Duration) error {
	if d.cmd != nil {
		return errors.New("helix daemon: already started")
	}
	bin, err := resolveBinary(d.Command)
	if err != nil {
		return err
	}
	d.bin = bin
	if err := Status(ctx, bin); err == nil {
		return errors.New("a Helix daemon is already running; stop it first (helix remote stop)")
	}
	cmd := exec.Command(bin, "daemon") //nolint:gosec // the user's own helix binary
	if d.Log != nil {
		cmd.Stdout, cmd.Stderr = d.Log, d.Log
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start helix daemon: %w", err)
	}
	d.cmd, d.done = cmd, make(chan error, 1)
	go func() { d.done <- cmd.Wait() }()

	deadline := time.Now().Add(ready)
	for time.Now().Before(deadline) {
		select {
		case err := <-d.done:
			d.cmd = nil
			return fmt.Errorf("helix daemon exited during startup: %v", err)
		case <-ctx.Done():
			_ = d.Stop(context.Background())
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
		if Status(ctx, bin) == nil {
			return nil
		}
	}
	_ = d.Stop(context.Background())
	return fmt.Errorf("helix daemon did not answer within %s", ready)
}

// Stop asks the daemon to stop, and kills it if it has not within 15
// seconds. Stopping a daemon that is not running is not an error.
func (d *Daemon) Stop(ctx context.Context) error {
	if d.cmd == nil {
		return nil
	}
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = exec.CommandContext(sctx, d.bin, "remote", "stop").Run() //nolint:gosec // the user's own helix binary
	select {
	case <-d.done:
	case <-time.After(15 * time.Second):
		_ = d.cmd.Process.Kill()
		<-d.done
	}
	d.cmd = nil
	return nil
}

// Status reports whether a Helix daemon answers.
func Status(ctx context.Context, bin string) error {
	sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return exec.CommandContext(sctx, bin, "remote", "status").Run() //nolint:gosec // the user's own helix binary
}

// Submit sends one request to the running daemon and returns Helix's reply.
// A request that takes longer than timeout is abandoned (the daemon finishes
// the turn on its own; its record still lands).
func (d *Daemon) Submit(ctx context.Context, text string, timeout time.Duration) (string, error) {
	if d.cmd == nil {
		return "", errors.New("helix daemon: not started")
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(sctx, d.bin, "remote", "submit", text) //nolint:gosec // the user's own helix binary
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if sctx.Err() != nil {
		return "", fmt.Errorf("no reply within %s", timeout)
	}
	reply := strings.TrimSpace(out.String())
	var doc struct {
		Reply string `json:"reply"`
	}
	if json.Unmarshal([]byte(reply), &doc) == nil {
		reply = doc.Reply
	}
	if err != nil {
		return reply, fmt.Errorf("helix remote submit: %w", err)
	}
	return reply, nil
}
