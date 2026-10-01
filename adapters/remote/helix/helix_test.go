package helix

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// TestMain lets this test binary act as `helix replay` when asked to, so the
// adapter is tested against a real process speaking the real protocol.
func TestMain(m *testing.M) {
	if mode := os.Getenv("FAKE_HELIX"); mode != "" && len(os.Args) > 1 && os.Args[len(os.Args)-1] == "replay" {
		fakeHelix(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func fakeHelix(mode string) {
	fmt.Fprintln(os.Stderr, "fake helix starting") // stderr must never reach the protocol
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		switch mode {
		case "hang":
			time.Sleep(time.Hour)
		case "garbage":
			fmt.Println("this is not json")
			continue
		case "wrong-id":
			_ = json.NewEncoder(os.Stdout).Encode(Response{V: 1, ID: "someone-else", OK: true})
			continue
		}
		resp := Response{V: 1, ID: req.ID, OK: true, Usage: Usage{ModelCalls: 1, InputChars: 100, OutputChars: 20}}
		if len(req.Lessons) > 0 {
			resp.Steps = []Step{{Tool: "file", Action: "read", Subject: "read go.mod"}}
			resp.Reply = "lessons: " + req.Lessons[0].Text
		} else {
			resp.Steps = []Step{{Tool: "file", Action: "read", Subject: "read package.json"}}
		}
		if req.Request == "fail" {
			resp = Response{V: 1, ID: req.ID, Error: "planner unavailable"}
		}
		_ = json.NewEncoder(os.Stdout).Encode(resp)
	}
}

func fakeHost(t *testing.T, mode string) *Host {
	t.Helper()
	t.Setenv("FAKE_HELIX", mode)
	h := &Host{Command: os.Args[0], Timeout: 2 * time.Second}
	t.Cleanup(func() { _ = h.Close() })
	return h
}

func ep(text string) types.Episode {
	return types.Episode{ID: types.NewID(t0), Host: HostName, Request: types.Request{Text: text}}
}

func TestReplayWithAndWithoutLessonsOverOneProcess(t *testing.T) {
	h := fakeHost(t, "ok")
	ctx := context.Background()
	without, err := h.Replay(ctx, ep("build the project"), nil)
	if err != nil {
		t.Fatal(err)
	}
	with, err := h.Replay(ctx, ep("build the project"), []types.Lesson{{ID: types.NewID(t0), Text: "This is a Go module."}})
	if err != nil {
		t.Fatal(err)
	}
	if without.Steps[0].Subject != "read package.json" || with.Steps[0].Subject != "read go.mod" || !with.Steps[0].OK {
		t.Fatalf("without %+v / with %+v", without.Steps, with.Steps)
	}
	if with.Text != "lessons: This is a Go module." || with.Usage.ModelCalls != 1 {
		t.Fatalf("with %+v", with)
	}
	first := h.cmd
	if _, err := h.Replay(ctx, ep("again"), nil); err != nil || h.cmd != first {
		t.Fatal("each replay should reuse the same helix replay process")
	}
}

func TestOnlyHelixEpisodesAreReplayed(t *testing.T) {
	h := fakeHost(t, "ok")
	e := ep("x")
	e.Host = "helixsim"
	if _, err := h.Replay(context.Background(), e, nil); !errors.Is(err, ports.ErrCannotReplay) {
		t.Fatalf("err = %v", err)
	}
}

func TestHelixErrorsAndBrokenStreams(t *testing.T) {
	ctx := context.Background()
	if _, err := fakeHost(t, "ok").Replay(ctx, ep("fail"), nil); err == nil || !strings.Contains(err.Error(), "planner unavailable") {
		t.Errorf("planner error: %v", err)
	}
	if _, err := fakeHost(t, "garbage").Replay(ctx, ep("x"), nil); err == nil || !strings.Contains(err.Error(), "not a v1 response") {
		t.Errorf("garbage: %v", err)
	}
	if _, err := fakeHost(t, "wrong-id").Replay(ctx, ep("x"), nil); err == nil || !strings.Contains(err.Error(), "someone-else") {
		t.Errorf("wrong id: %v", err)
	}
	h := fakeHost(t, "hang")
	h.Timeout = 300 * time.Millisecond
	start := time.Now()
	if _, err := h.Replay(ctx, ep("x"), nil); err == nil || !strings.Contains(err.Error(), "longer than") {
		t.Errorf("hang: %v", err)
	}
	if time.Since(start) > 5*time.Second || h.cmd != nil {
		t.Error("a hung replay was not killed")
	}
	missing := &Host{Command: filepath.Join(t.TempDir(), "no-helix-here")}
	if _, err := missing.Replay(ctx, ep("x"), nil); err == nil {
		t.Error("a missing binary was not reported")
	}
}

// The request this adapter writes and the responses it reads are the ones
// pinned by Helix's golden file (copied here byte for byte).
func TestReplayWireGolden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "replay_v1.ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("golden has %d lines", len(lines))
	}
	req := Request{V: 1, ID: "0199a1b2c3d4e5f6a7b8c9d0e1f2", Request: "build the project",
		Lessons: []Lesson{{ID: "01a0f7afc76b7637f51421cbd985", Text: "This is a Go module: read go.mod."}}}
	got, _ := json.Marshal(req)
	if !bytes.Equal(got, []byte(lines[0])) {
		t.Fatalf("request encoding drifted:\n got %s\nwant %s", got, lines[0])
	}
	for _, l := range lines[1:] {
		var r Response
		dec := json.NewDecoder(strings.NewReader(l))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("response %s: %v", l, err)
		}
	}
}
