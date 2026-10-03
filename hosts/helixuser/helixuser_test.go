package helixuser

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/ports"
)

type fakeModel struct {
	answer string
	got    ports.ModelRequest
}

func (f *fakeModel) Name() string { return "fake" }
func (f *fakeModel) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	f.got = req
	return ports.ModelResponse{Text: f.answer}, nil
}

var projects = []Project{{Path: "Dev/metabolism", Tree: "project Dev/metabolism"}, {Path: "Dev/helix", Tree: "project Dev/helix"}}

func TestScreenDropsWhatNoSyntheticUserMaySend(t *testing.T) {
	for _, bad := range []string{
		"delete the build cache in Dev/helix",
		"git push --force Dev/helix",
		"sudo make install in Dev/metabolism",
		"rm -rf Dev/metabolism/bin",
		"upgrade all brew packages",
		"/purge",
		"what does the judge package do?", // names no project
		"",
		"go test ./internal/agent/... in Dev/helix", // runs as a shell line
		"look for a file called lessons.go in Dev/metabolism",
		"Find the Makefile in Dev/metabolism",
		"git log --oneline -20 in Dev/helix",
	} {
		if Screen(Request{Kind: "explain", Text: bad}, projects) {
			t.Errorf("screen passed %q", bad)
		}
	}
	for _, ok := range []string{
		"what does the judge package in Dev/metabolism do?",
		"run the tests for the ledger package in Dev/metabolism",
		"which branch is Dev/helix on?",
	} {
		if !Screen(Request{Kind: "explain", Text: ok}, projects) {
			t.Errorf("screen dropped %q", ok)
		}
	}
	if !Screen(Request{Kind: "general", Text: "what is a mutex?"}, projects) {
		t.Error("a general question needs no project")
	}
}

func TestGenerateMixesFreshRequestsWithRepeats(t *testing.T) {
	m := &fakeModel{answer: `Here you go: {"requests":[
		{"kind":"explain","text":"what does the ledger package in Dev/metabolism do?"},
		{"kind":"explain","text":"what does the  ledger package in Dev/metabolism do?"},
		{"kind":"run","text":"run go vet on Dev/helix"},
		{"kind":"find","text":"delete the old logs in Dev/helix"},
		{"kind":"general","text":"how do Go channels close?"},
		{"kind":"history","text":"what changed in the last commit of Dev/helix?"}
	]}`}
	earlier := []string{"build Dev/helix", "where is the Makefile in Dev/metabolism?", "x", "y", "z", "a", "b", "c", "d", "e"}
	got, err := Generator{Model: m}.Generate(context.Background(), projects, 6, 7, earlier)
	if err != nil {
		t.Fatal(err)
	}
	fresh, repeats := 0, 0
	for _, q := range got {
		if strings.Contains(q.Text, "delete") {
			t.Errorf("a destructive request got through: %q", q.Text)
		}
		if q.Repeat {
			repeats++
		} else {
			fresh++
		}
	}
	// 10% of 6 is 0, so a day of 6 has no repeats; the duplicate and the
	// destructive request are dropped, leaving 4 fresh.
	if fresh != 4 || repeats != 0 {
		t.Fatalf("fresh %d repeats %d: %+v", fresh, repeats, got)
	}
	if !strings.Contains(m.got.User, "Dev/helix") || !m.got.JSON || !m.got.DisableThinking {
		t.Fatalf("model request %+v", m.got)
	}

	got, _ = Generator{Model: m, RepeatShare: 0.5}.Generate(context.Background(), projects, 6, 7, earlier)
	repeats = 0
	for _, q := range got {
		if q.Repeat {
			repeats++
		}
	}
	if repeats != 3 || len(got) != 6 {
		t.Fatalf("with half repeats: %d repeats of %d", repeats, len(got))
	}
}

func TestGenerateFailsLoudlyOnUnusableOutput(t *testing.T) {
	m := &fakeModel{answer: `{"requests":[{"kind":"find","text":"rm -rf everything"}]}`}
	if _, err := (Generator{Model: m}).Generate(context.Background(), projects, 5, 1, nil); err == nil {
		t.Fatal("an answer with no usable request was accepted")
	}
}

func TestBuildProjectOutlinesTheTree(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "Dev", "proj")
	for _, f := range []string{"cmd/tool/main.go", "ledger/ledger.go", "docs/vision.md", ".git/HEAD", "README.md"} {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755)
		_ = os.WriteFile(filepath.Join(root, f), []byte("# Proj\nA tool.\n"), 0o644)
	}
	p, err := BuildProject(home, "Dev/proj")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"project Dev/proj", "cmd/tool", "ledger", "docs", "README begins:", "A tool."} {
		if !strings.Contains(p.Tree, want) {
			t.Errorf("outline lacks %q:\n%s", want, p.Tree)
		}
	}
	if strings.Contains(p.Tree, ".git") {
		t.Error("hidden directories belong out of the outline")
	}
}

// One bad element or a broken document costs what is broken, not the day.
func TestParseSurvivesABadAnswer(t *testing.T) {
	ps := []Project{{Path: "Dev/Metabolism"}}
	good := `{"kind":"explain","text":"explain the ledger in Dev/Metabolism"}`
	for name, text := range map[string]string{
		"trailing text":   `{"requests":[` + good + `]} hope this helps {}`,
		"bad element":     `{"requests":[` + good + `,{"kind":"explain","text":"x in Dev/Metabolism","repeat":"no"},{"kind":7}]}`,
		"unescaped quote": `{"requests":[` + good + `,{"kind":"explain","text":"say "hi" in Dev/Metabolism"}]}`,
		"cut off":         `{"requests":[` + good + `,{"kind":"explain","text":"unfinished`,
	} {
		got, _, err := Parse(text, ps)
		if err != nil || len(got) == 0 || got[0].Text != "explain the ledger in Dev/Metabolism" {
			t.Errorf("%s: %v %+v", name, err, got)
		}
	}
	if _, _, err := Parse("I cannot help with that.", ps); err == nil {
		t.Error("an answer with no JSON gave no reason")
	}
}
