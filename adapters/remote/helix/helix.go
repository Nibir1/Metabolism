// Package helix replays past episodes through real Helix: it runs
// `helix replay` (Helix: cmd/helix/replay_cmd.go) and asks it to plan each
// request again, with or without a lesson. Helix plans and executes nothing.
//
// It lives under adapters/remote/ because each replay is a planner call to
// the user's configured model provider: past requests are sent again. The CLI
// therefore requires -allow-remote to use it (D-016).
//
// One `helix replay` process serves a whole run, speaking NDJSON (replay wire
// v1, pinned by testdata/replay_v1.ndjson, a byte copy of Helix's golden).
package helix

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// WireVersion is the newest replay protocol version this adapter speaks. A
// first-plan replay (Rounds <= 1) is sent as version 1, which every Helix
// with `helix replay` reads; more rounds need version 2 (D-021).
const WireVersion = 2

// HostName is the Host field of episodes Helix records.
const HostName = "helix"

// DefaultTimeout bounds one replay. A planner call with retries can take a
// while on a slow provider.
const DefaultTimeout = 3 * time.Minute

// Lesson is one lesson sent with a replay request.
type Lesson struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// Request is one line written to `helix replay`.
type Request struct {
	V       int      `json:"v"`
	ID      string   `json:"id"`
	Request string   `json:"request"`
	Lessons []Lesson `json:"lessons,omitempty"`
	Rounds  int      `json:"rounds,omitempty"`
}

// Step is one planned step in a response. Round, Outcome ("" planned and not
// run, "ok", "failed") and Err are version 2.
type Step struct {
	Tool    string `json:"tool"`
	Action  string `json:"action,omitempty"`
	Subject string `json:"subject,omitempty"`
	Round   int    `json:"round,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	Err     string `json:"err,omitempty"`
}

// Usage is what one replay cost.
type Usage struct {
	ModelCalls  int `json:"model_calls"`
	InputChars  int `json:"input_chars"`
	OutputChars int `json:"output_chars"`
}

// Response is one line read back.
type Response struct {
	V      int    `json:"v"`
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Steps  []Step `json:"steps,omitempty"`
	Reply  string `json:"reply,omitempty"`
	Rounds int    `json:"rounds,omitempty"`
	End    string `json:"end,omitempty"`
	Usage  Usage  `json:"usage"`
}

// Host implements ports.Host by driving `helix replay`.
type Host struct {
	// Command is the helix binary (default "helix" on PATH).
	Command string
	// Dir is the working directory the replays plan in, and the root the
	// read-only steps of a multi-round replay read under.
	Dir string
	// Rounds is how many planner rounds to replay (D-021). 0 or 1 replays
	// the first plan only and runs nothing.
	Rounds  int
	Timeout time.Duration

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  *bufio.Scanner
	stderr *capped
}

var _ ports.Host = (*Host)(nil)

// Describe implements ports.Host. The budget matches Helix's block bounds.
func (h *Host) Describe() types.HostProfile {
	return types.HostProfile{
		Name:   HostName,
		Kinds:  []types.LessonKind{types.KindFact, types.KindPattern, types.KindPreference},
		Budget: types.Budget{MaxLessons: 8, MaxChars: 1600},
	}
}

// Replay implements ports.Host.
func (h *Host) Replay(ctx context.Context, ep types.Episode, lessons []types.Lesson) (types.ReplayResult, error) {
	if ep.Host != HostName {
		return types.ReplayResult{}, fmt.Errorf("%w: episode %s was recorded by %q, not Helix", ports.ErrCannotReplay, ep.ID, ep.Host)
	}
	req := Request{V: 1, ID: string(ep.ID), Request: ep.Request.Text}
	if h.Rounds > 1 {
		req.V, req.Rounds = 2, h.Rounds
	}
	for _, l := range lessons {
		req.Lessons = append(req.Lessons, Lesson{ID: string(l.ID), Text: l.Text})
	}
	resp, err := h.roundTrip(ctx, req)
	if err != nil {
		return types.ReplayResult{}, err
	}
	if !resp.OK {
		return types.ReplayResult{}, fmt.Errorf("helix replay %s: %s", ep.ID, resp.Error)
	}
	res := types.ReplayResult{EpisodeID: ep.ID, Text: resp.Reply, Rounds: resp.Rounds, End: resp.End,
		Usage: types.Usage{ModelCalls: resp.Usage.ModelCalls, InputChars: resp.Usage.InputChars, OutputChars: resp.Usage.OutputChars}}
	for _, s := range resp.Steps {
		// A step that did not run is "OK" only in the sense of "not known to
		// fail". A read-only step a multi-round replay ran has a real outcome.
		res.Steps = append(res.Steps, types.Step{Tool: s.Tool, Action: s.Action, Subject: s.Subject,
			OK: s.Outcome != "failed", Err: s.Err, Round: s.Round, Ran: s.Outcome != ""})
	}
	return res, nil
}

func (h *Host) roundTrip(ctx context.Context, req Request) (Response, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.startLocked(); err != nil {
		return Response{}, err
	}
	line, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if _, err := h.stdin.Write(append(line, '\n')); err != nil {
		return Response{}, h.failLocked(fmt.Errorf("write to helix replay: %w", err))
	}

	timeout := h.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	type result struct {
		resp Response
		err  error
	}
	done := make(chan result, 1)
	// The reader works on this process's scanner and stderr, captured now: if
	// it times out, the next replay starts a new process while this goroutine
	// is still blocked on the old one.
	lines, stderr := h.lines, h.stderr
	go func() {
		if !lines.Scan() {
			err := lines.Err()
			if err == nil {
				err = io.EOF
			}
			done <- result{err: fmt.Errorf("helix replay ended: %w (stderr: %s)", err, tail(stderr.String()))}
			return
		}
		var resp Response
		dec := json.NewDecoder(strings.NewReader(lines.Text()))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&resp); err != nil {
			done <- result{err: fmt.Errorf("helix replay sent a line that is not a v%d response: %w", req.V, err)}
			return
		}
		done <- result{resp: resp}
	}()
	select {
	case r := <-done:
		if r.err != nil {
			return Response{}, h.failLocked(r.err)
		}
		if r.resp.V != req.V || r.resp.ID != req.ID {
			return Response{}, h.failLocked(fmt.Errorf("helix replay answered v%d id %q for v%d id %q",
				r.resp.V, r.resp.ID, req.V, req.ID))
		}
		return r.resp, nil
	case <-ctx.Done():
		return Response{}, h.failLocked(ctx.Err())
	case <-time.After(timeout):
		return Response{}, h.failLocked(fmt.Errorf("helix replay took longer than %s", timeout))
	}
}

func (h *Host) startLocked() error {
	if h.cmd != nil {
		return nil
	}
	bin, err := resolveBinary(h.Command)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "replay")
	cmd.Dir = h.Dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	h.stderr = &capped{max: 8 << 10}
	cmd.Stderr = h.stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s replay: %w", bin, err)
	}
	h.cmd, h.stdin = cmd, stdin
	h.lines = bufio.NewScanner(stdout)
	h.lines.Buffer(make([]byte, 64<<10), 4<<20)
	return nil
}

// failLocked kills the process after a broken exchange: its stream position
// is no longer trustworthy. The next replay starts a fresh one.
func (h *Host) failLocked(err error) error {
	if h.cmd != nil {
		_ = h.cmd.Process.Kill()
		_ = h.cmd.Wait()
		h.cmd = nil
	}
	return err
}

// Close ends the replay process.
func (h *Host) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cmd == nil {
		return nil
	}
	_ = h.stdin.Close()
	err := h.cmd.Wait()
	h.cmd = nil
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("helix replay exited %d: %s", exit.ExitCode(), tail(h.stderr.String()))
	}
	return err
}

// capped keeps the first max bytes of a process's stderr. os/exec writes to
// it from its own goroutine while the adapter may read it for an error
// message, so it is locked.
type capped struct {
	mu  sync.Mutex
	b   strings.Builder
	max int
}

func (c *capped) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if room := c.max - c.b.Len(); room > 0 {
		if len(p) > room {
			c.b.Write(p[:room])
		} else {
			c.b.Write(p)
		}
	}
	return len(p), nil
}

func (c *capped) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.b.String()
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = "…" + s[len(s)-300:]
	}
	if s == "" {
		return "(none)"
	}
	return s
}

// resolveBinary turns the configured command into an absolute path before the
// replay's working directory is set. A relative path such as bin/helix is
// otherwise looked up relative to that directory: with -replay-dir ~ it
// became ~/bin/helix, which does not exist.
func resolveBinary(command string) (string, error) {
	if command == "" {
		command = "helix"
	}
	if !strings.ContainsRune(command, filepath.Separator) {
		p, err := exec.LookPath(command)
		if err != nil {
			return "", fmt.Errorf("find %s on PATH: %w (build it with make helix-build and pass -helix-bin bin/helix)", command, err)
		}
		command = p
	}
	abs, err := filepath.Abs(command)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", fmt.Errorf("helix binary %s: %w", abs, err)
	}
	return abs, nil
}
