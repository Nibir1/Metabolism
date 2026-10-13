package coder

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// scripted answers each call with the next reply, and records the prompts.
type scripted struct {
	replies []string
	prompts []string
}

func (s *scripted) Name() string { return "scripted" }
func (s *scripted) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	s.prompts = append(s.prompts, req.User)
	if len(s.replies) == 0 {
		return ports.ModelResponse{}, errors.New("no more replies")
	}
	r := s.replies[0]
	s.replies = s.replies[1:]
	return ports.ModelResponse{Text: r}, nil
}

func files(name, content string) string {
	return fmt.Sprintf(`{"files":{%q:%q}}`, name, content)
}

func exercises(t *testing.T) map[string]Exercise {
	t.Helper()
	exs, err := Load(filepath.Join("testdata", "exercises"))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]Exercise{}
	for _, ex := range exs {
		m[ex.Name] = ex
	}
	return m
}

func needSandbox(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the sandbox is macOS only")
	}
}

const wrong = "package greeting\n\nfunc Hello(name string) string { return \"Hi \" + name }\n"
const right = "package greeting\n\nfunc Hello(name string) string { return \"Hello, \" + name + \"!\" }\n"

// The reference solution is never shown; a wrong first answer gets the test
// output and a second attempt; the run records both.
func TestSolveRetriesWithTheTestOutput(t *testing.T) {
	needSandbox(t)
	ex := exercises(t)["greeting"]
	model := &scripted{replies: []string{files("greeting.go", wrong), files("greeting.go", right)}}
	lesson := types.Lesson{Text: "Read the expected string in the instructions character by character."}
	res, err := Agent{Model: model, Runner: Sandbox{CacheDir: t.TempDir()}}.Solve(context.Background(), ex, []types.Lesson{lesson})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed || res.Attempts != 2 || len(res.Steps) != 4 || res.Usage.ModelCalls != 2 {
		t.Fatalf("result %+v", res)
	}
	if res.Steps[1].OK || !res.Steps[3].OK || res.Steps[1].Err == "" {
		t.Fatalf("steps %+v", res.Steps)
	}
	if strings.Contains(model.prompts[0], "Hello, \" + name") {
		t.Fatal("the reference solution reached the prompt")
	}
	if !strings.Contains(model.prompts[0], lesson.Text) || !strings.Contains(model.prompts[1], "The tests failed") {
		t.Fatalf("prompts: %q", model.prompts)
	}
	ep, out := Episode(ex, res, nil, nil, time.Now(), time.Now())
	if err := ep.Validate(); err != nil || ep.End != types.RunDone || out.Kind != types.OutcomeSuccess || ep.Attrs["exercise"] != "greeting" {
		t.Fatalf("episode %+v %v, outcome %+v", ep, err, out)
	}
}

// Code a model writes cannot write outside its copy or reach the network.
func TestSandboxRefusesEscape(t *testing.T) {
	needSandbox(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	outside := filepath.Join(t.TempDir(), "escaped.txt")
	code := fmt.Sprintf(`package escape

import (
	"net"
	"os"
)

func Escape() (error, error) {
	w := os.WriteFile(%q, []byte("out"), 0o600)
	c, n := net.Dial("tcp", %q)
	if n == nil {
		c.Close()
	}
	return w, n
}
`, outside, ln.Addr().String())
	model := &scripted{replies: []string{files("escape.go", code)}}
	res, err := Agent{Model: model, Attempts: 1, Runner: Sandbox{CacheDir: t.TempDir()}}.Solve(context.Background(), exercises(t)["escape"], nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Passed {
		t.Fatalf("the escape was not refused: %+v", res.Steps)
	}
	if _, err := os.Stat(outside); err == nil {
		t.Fatal("a file was written outside the copy")
	}
}

// Only the solution files are written: a test file or a path outside the
// exercise in the answer is ignored.
func TestParseFilesKeepsOnlySolutionFiles(t *testing.T) {
	got, err := parseFiles(`{"files":{"greeting.go":"a","greeting_test.go":"b","../../x":"c"}}`, []string{"greeting.go"})
	if err != nil || len(got) != 1 || got["greeting.go"] != "a" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := parseFiles(`{"files":{"other.go":"x"}}`, []string{"greeting.go"}); err == nil {
		t.Fatal("an answer with no solution file must be refused")
	}
}

func TestReplayNeedsAKnownExercise(t *testing.T) {
	h := Host{Exercises: exercises(t)}
	_, err := h.Replay(context.Background(), types.Episode{Attrs: types.Attrs{"exercise": "nope"}}, nil)
	if !errors.Is(err, ports.ErrCannotReplay) {
		t.Fatalf("%v", err)
	}
}

// With one solution file, a fenced Go block is accepted when the JSON is not
// usable; with two, it is not (which file would it be?).
func TestParseFilesAcceptsAFencedBlock(t *testing.T) {
	text := "Here it is:\n```go\npackage greeting\n\nfunc Hello(n string) string { return n }\n```\n"
	got, err := parseFiles(text, []string{"greeting.go"})
	if err != nil || !strings.HasPrefix(got["greeting.go"], "package greeting") {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := parseFiles(text, []string{"a.go", "b.go"}); err == nil {
		t.Fatal("a fenced block was guessed into one of two files")
	}
}

// A first attempt that passes yields a training example whose prompt has
// no lessons and the original stub.
func TestPassingFirstAttemptYieldsAnExample(t *testing.T) {
	needSandbox(t)
	ex := exercises(t)["greeting"]
	model := &scripted{replies: []string{files("greeting.go", right)}}
	lesson := types.Lesson{Text: "Mind the exclamation mark."}
	res, err := Agent{Model: model, Runner: Sandbox{CacheDir: t.TempDir()}}.Solve(context.Background(), ex, []types.Lesson{lesson})
	if err != nil {
		t.Fatal(err)
	}
	e := res.Example
	if e == nil || strings.Contains(e.User, lesson.Text) || !strings.Contains(e.User, "panic(\"not implemented\")") || e.Answer == "" {
		t.Fatalf("example %+v", e)
	}
	if res.PromptChars <= len(e.System)+len(e.User) {
		t.Fatalf("the lessons should make the prompt longer: %d vs %d", res.PromptChars, len(e.System)+len(e.User))
	}
}

// The test files reach the prompt only with ShowTests, marked read-only.
func TestShowTests(t *testing.T) {
	needSandbox(t)
	ex := exercises(t)["greeting"]
	for _, show := range []bool{false, true} {
		model := &scripted{replies: []string{files("greeting.go", right)}}
		if _, err := (Agent{Model: model, ShowTests: show, Runner: Sandbox{CacheDir: t.TempDir()}}).Solve(context.Background(), ex, nil); err != nil {
			t.Fatal(err)
		}
		has := strings.Contains(model.prompts[0], `<test file="greeting_test.go" read-only="true">`) &&
			strings.Contains(model.prompts[0], `Hello("Ada")`)
		if has != show {
			t.Fatalf("ShowTests=%v: tests in prompt %v\n%s", show, has, model.prompts[0])
		}
	}
}
