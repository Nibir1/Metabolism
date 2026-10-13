// Package mlx trains and serves small models with Apple's MLX (mlx-lm), on
// this machine (Phase 7, D-032). It is the trainer port's first adapter and
// the only place non-Go code runs, as a separate Python process from the
// virtual environment make trainer-setup creates. It lives under
// adapters/remote because the first use of a base model downloads it from
// Hugging Face; training data never leaves the machine.
package mlx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/ports"
)

// DefaultBase is an instruction-tuned coding model, 4-bit, small enough to
// run and train on this machine. The 3B model passed 1 of the 39 exercises
// and left lessons no room (D-037); the 7B is the next size up.
const DefaultBase = "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit"

// DefaultPython is the trainer environment's interpreter.
const DefaultPython = ".runs/trainer/venv/bin/python"

// ErrNotSetUp: the trainer environment is missing.
var ErrNotSetUp = errors.New("mlx: the trainer is not set up (run make trainer-setup once)")

// Trainer runs mlx-lm's LoRA fine-tuning.
type Trainer struct {
	Python string
	Log    io.Writer // the trainer's output (nil: discarded)
}

var _ ports.Trainer = Trainer{}

// Name implements ports.Trainer.
func (t Trainer) Name() string { return "mlx-lora" }

func python(p string) (string, error) {
	if p == "" {
		p = DefaultPython
	}
	if _, err := os.Stat(p); err != nil {
		return "", ErrNotSetUp
	}
	return p, nil
}

// Train implements ports.Trainer.
func (t Trainer) Train(ctx context.Context, req ports.TrainRequest) error {
	py, err := python(t.Python)
	if err != nil {
		return err
	}
	iters := req.Iters
	if iters <= 0 {
		iters = 200
	}
	cmd := exec.CommandContext(ctx, py, "-m", "mlx_lm", "lora", "--model", req.Base, "--train",
		"--data", req.DataDir, "--adapter-path", req.OutDir, "--iters", strconv.Itoa(iters), "--batch-size", "1")
	cmd.Stdout, cmd.Stderr = orDiscard(t.Log), orDiscard(t.Log)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mlx: training failed: %w", err)
	}
	if _, err := os.Stat(req.OutDir); err != nil {
		return fmt.Errorf("mlx: training wrote no adapter at %s", req.OutDir)
	}
	return nil
}

// Server is a local OpenAI-compatible server for a base model, with an
// adapter when Adapter is set. It listens on loopback only.
type Server struct {
	Python  string
	Base    string
	Adapter string
	Log     io.Writer
}

// Start runs the server and waits until it answers. stop ends it.
func (s Server) Start(ctx context.Context) (baseURL string, stop func(), err error) {
	py, err := python(s.Python)
	if err != nil {
		return "", nil, err
	}
	port, err := freePort()
	if err != nil {
		return "", nil, err
	}
	args := []string{"-m", "mlx_lm", "server", "--model", s.Base, "--host", "127.0.0.1", "--port", strconv.Itoa(port)}
	if s.Adapter != "" {
		args = append(args, "--adapter-path", s.Adapter)
	}
	cmd := exec.Command(py, args...)
	cmd.Stdout, cmd.Stderr = orDiscard(s.Log), orDiscard(s.Log)
	if err := cmd.Start(); err != nil {
		return "", nil, err
	}
	stop = func() {
		_ = cmd.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			_ = cmd.Process.Kill()
		}
	}
	baseURL = fmt.Sprintf("http://127.0.0.1:%d/v1", port)
	// The first start downloads the base model: allow it time.
	deadline := time.Now().Add(20 * time.Minute)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			stop()
			return "", nil, ctx.Err()
		}
		if cmd.ProcessState != nil {
			return "", nil, errors.New("mlx: the server exited while starting")
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return baseURL, stop, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	stop()
	return "", nil, errors.New("mlx: the server did not start in 20 minutes")
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func orDiscard(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

// IsLocal reports whether a base URL is this machine's.
func IsLocal(u string) bool { return strings.HasPrefix(u, "http://127.0.0.1:") }
