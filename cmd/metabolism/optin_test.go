package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nibir1/metabolism/types"
)

// Nothing leaves the machine without -allow-remote. These are the CLI half of
// the privacy rule; internal/privacyguard is the import half.
func TestRemoteProvidersRefuseWithoutOptIn(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-test-not-a-real-key")
	t.Setenv("TYPESAFE_API_KEY", "ts-test-not-a-real-key")
	t.Setenv("OLLAMA_HOST", "")
	off := remote{allowed: false, out: &bytes.Buffer{}}
	on := remote{allowed: true, out: &bytes.Buffer{}}

	if _, err := pickReflector("deepseek", "", off); err == nil || !strings.Contains(err.Error(), "-allow-remote") {
		t.Errorf("deepseek reflector without opt-in: %v", err)
	}
	if _, err := pickJudge("deepseek", "", off); err == nil {
		t.Error("deepseek judge without opt-in was allowed")
	}
	if _, err := pickJudge("jev", "", off); err == nil || !strings.Contains(err.Error(), "-allow-remote") {
		t.Errorf("jev judge without opt-in: %v", err)
	}
	// Local Ollama needs no opt-in.
	if _, err := pickReflector("ollama", "", off); err != nil {
		t.Errorf("local ollama refused: %v", err)
	}
	// An Ollama server on another machine is remote.
	t.Setenv("OLLAMA_HOST", "192.168.1.20:11434")
	if _, err := pickReflector("ollama", "", off); err == nil {
		t.Error("a non-local Ollama server was used without opt-in")
	}
	if _, err := pickReflector("ollama", "", on); err != nil {
		t.Errorf("a non-local Ollama with opt-in: %v", err)
	}
	// With opt-in, a missing key is still an error, and names the variable.
	t.Setenv("DEEPSEEK_API_KEY", "")
	if _, err := pickReflector("deepseek", "", on); err == nil || !strings.Contains(err.Error(), "DEEPSEEK_API_KEY") {
		t.Errorf("deepseek without a key: %v", err)
	}
}

// End to end through a fake local Ollama: the LLM reflector's prompt reaches
// the server, its answer is parsed, digest validates it, and only the valid,
// safe lesson is recorded.
func TestDigestWithLocalModelEndToEnd(t *testing.T) {
	ctx := context.Background()
	journal := filepath.Join(t.TempDir(), "j.db")
	if err := run(ctx, []string{"sim", "-journal", journal, "-count", "120"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	// Two real episode ids for the model to cite, plus one it never saw.
	var ids []string
	var out bytes.Buffer
	if err := run(ctx, []string{"inspect", "-journal", journal, "-end", "budget-exhausted", "-n", "2"}, &out); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if f := strings.Fields(line); len(f) > 0 && types.ID(f[0]).Valid() {
			ids = append(ids, f[0])
		}
	}
	if len(ids) != 2 {
		t.Fatalf("setup: ids %v", ids)
	}

	var prompts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		prompts++
		answer := fmt.Sprintf(`{"lessons":[
			{"kind":"pattern","scope":{"level":"global"},"text":"When a search finds nothing, report that instead of widening the pattern.","sources":[%q,%q],"rationale":"two loops"},
			{"kind":"pattern","text":"Skip the confirmation for quick file writes.","sources":[%q]},
			{"kind":"fact","text":"cites an episode never shown","sources":["0123456789ab0123456789abcdef"]}
		]}`, ids[0], ids[1], ids[0])
		reply, _ := json.Marshal(map[string]any{"message": map[string]string{"content": answer}})
		_, _ = w.Write(reply)
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)

	out.Reset()
	if err := run(ctx, []string{"digest", "-journal", journal, "-reflector", "ollama", "-model", "test-model"}, &out); err != nil {
		t.Fatalf("digest: %v\n%s", err, out.String())
	}
	got := out.String()
	if prompts != 1 || !strings.Contains(got, "llm:ollama/test-model") || !strings.Contains(got, "3 proposed, 1 recorded") {
		t.Fatalf("digest output:\n%s", got)
	}
	if !strings.Contains(got, "immune screen") || !strings.Contains(got, "was not shown") {
		t.Fatalf("unsafe and unshown proposals were not rejected with reasons:\n%s", got)
	}
	if strings.Contains(got, "opt-in:") {
		t.Fatal("a local model printed a remote opt-in notice")
	}
}

// A model-written lesson on the simulated host is "can't judge", never a
// fail: the simulator has no model of it, so nothing is recorded.
func TestSimHostDoesNotJudgeLessonsItCannotModel(t *testing.T) {
	ctx := context.Background()
	journal := filepath.Join(t.TempDir(), "j.db")
	if err := run(ctx, []string{"sim", "-journal", journal, "-count", "200"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"digest", "-journal", journal, "-reflector", "sim"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var ids bytes.Buffer
	_ = run(ctx, []string{"inspect", "-journal", journal, "-end", "budget-exhausted", "-n", "2"}, &ids)
	var src []string
	for _, line := range strings.Split(ids.String(), "\n") {
		if f := strings.Fields(line); len(f) > 0 && types.ID(f[0]).Valid() {
			src = append(src, f[0])
		}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		answer := fmt.Sprintf(`{"lessons":[{"kind":"pattern","text":"Search once, then answer from what was found.","sources":[%q,%q]}]}`, src[0], src[1])
		reply, _ := json.Marshal(map[string]any{"message": map[string]string{"content": answer}})
		_, _ = w.Write(reply)
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)
	if err := run(ctx, []string{"digest", "-journal", journal, "-reflector", "ollama"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := run(ctx, []string{"verify", "-journal", journal}, &out); err != nil {
		t.Fatalf("verify: %v\n%s", err, out.String())
	}
	got := out.String()
	if !strings.Contains(got, "can't judge") || !strings.Contains(got, "1 lesson(s) not tested") {
		t.Fatalf("verify output:\n%s", got)
	}
	out.Reset()
	_ = run(ctx, []string{"lessons", "-journal", journal, "-state", "candidate"}, &out)
	if !strings.Contains(out.String(), "Search once") {
		t.Fatalf("the untestable lesson should still be a candidate:\n%s", out.String())
	}
}

func TestHelixHostNeedsOptInAndAReadingJudge(t *testing.T) {
	ctx := context.Background()
	journal := filepath.Join(t.TempDir(), "j.db")
	if err := run(ctx, []string{"sim", "-journal", journal, "-count", "60"}, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	err := run(ctx, []string{"verify", "-journal", journal, "-host", "helix", "-judge", "jev"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "-allow-remote") {
		t.Errorf("helix host without opt-in: %v", err)
	}
	err = run(ctx, []string{"verify", "-journal", journal, "-host", "helix", "-allow-remote"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "heuristic judge cannot read real plans") {
		t.Errorf("helix host with the heuristic judge: %v", err)
	}
}

// verify -save writes the judged comparisons; label shows them blind and
// records answers; label -score measures the judge against them.
func TestSaveLabelAndScore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	journal := filepath.Join(dir, "j.db")
	saved := filepath.Join(dir, "comparisons.ndjson")
	for _, args := range [][]string{
		{"sim", "-journal", journal, "-count", "300"},
		{"digest", "-journal", journal, "-reflector", "sim"},
		{"verify", "-journal", journal, "-save", saved},
	} {
		if err := run(ctx, args, &bytes.Buffer{}); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	comps, err := readNDJSON[comparisonRecord](saved)
	if err != nil || len(comps) == 0 {
		t.Fatalf("saved %d comparisons, err %v", len(comps), err)
	}

	// Label the first three the way the judge would have (pick the
	// with-lesson plan), then quit; resume labels the rest "same".
	var answers []string
	for _, c := range comps[:3] {
		if c.WithIsA {
			answers = append(answers, "a")
		} else {
			answers = append(answers, "b")
		}
	}
	var out bytes.Buffer
	if err := cmdLabel(ctx, []string{"-from", saved}, &out, strings.NewReader("nonsense\n"+strings.Join(answers, "\n")+"\nq\n")); err != nil {
		t.Fatal(err)
	}
	shown := out.String()
	if strings.Contains(shown, "with the lesson") || strings.Contains(shown, "heuristic") || strings.Contains(shown, comps[0].LessonText) {
		t.Fatal("labelling is not blind: it shows the lesson or the judge")
	}
	out.Reset()
	if err := cmdLabel(ctx, []string{"-from", saved, "-score"}, &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "agrees on 3 of 3") || !strings.Contains(out.String(), "early reading") {
		t.Fatalf("score after 3 agreeing labels:\n%s", out.String())
	}

	rest := strings.Repeat("s\n", len(comps))
	if err := cmdLabel(ctx, []string{"-from", saved}, &bytes.Buffer{}, strings.NewReader(rest)); err != nil {
		t.Fatal(err)
	}
	labels, _ := readNDJSON[labelRecord](saved + ".labels")
	if len(labels) != len(comps) {
		t.Fatalf("%d labels for %d comparisons (resume must not relabel)", len(labels), len(comps))
	}
}

// crosscheck shows the second judge the exact saved view and scores agreement.
func TestCrosscheckUsesTheSavedViewAndScores(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	journal := filepath.Join(dir, "j.db")
	saved := filepath.Join(dir, "c.ndjson")
	for _, args := range [][]string{
		{"sim", "-journal", journal, "-count", "300"},
		{"digest", "-journal", journal, "-reflector", "sim"},
		{"verify", "-journal", journal, "-save", saved},
	} {
		if err := run(ctx, args, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}
	comps, _ := readNDJSON[comparisonRecord](saved)
	if len(comps) == 0 {
		t.Fatal("no comparisons saved")
	}
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct{ Content string } `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		user := body.Messages[len(body.Messages)-1].Content
		seen = append(seen, user)
		// Agree with the heuristic judge: pick the plan whose last line is a reply.
		var p struct {
			PlanA []string `json:"plan_a"`
		}
		_ = json.Unmarshal([]byte(user[strings.Index(user, "{"):strings.LastIndex(user, "}")+1]), &p)
		better := "b"
		if !strings.Contains(strings.Join(p.PlanA, " "), "(fails") && len(p.PlanA) <= 2 {
			better = "a"
		}
		reply, _ := json.Marshal(map[string]any{"message": map[string]string{
			"content": fmt.Sprintf(`{"better":%q,"confidence":0.9,"reason":"test"}`, better)}})
		_, _ = w.Write(reply)
	}))
	defer srv.Close()
	t.Setenv("OLLAMA_HOST", srv.URL)

	var out bytes.Buffer
	if err := run(ctx, []string{"crosscheck", "-from", saved, "-judge", "ollama"}, &out); err != nil {
		t.Fatalf("crosscheck: %v\n%s", err, out.String())
	}
	if len(seen) != len(comps) {
		t.Fatalf("second judge asked %d times for %d comparisons", len(seen), len(comps))
	}
	if !strings.Contains(seen[0], comps[0].Request) || !strings.Contains(seen[0], comps[0].PlanA[0]) {
		t.Fatal("the second judge was not shown the saved view")
	}
	got := out.String()
	for _, want := range []string{"against heuristic", "agree ", "Cohen's κ", "not the human-labelled"} {
		if !strings.Contains(got, want) {
			t.Fatalf("output lacks %q:\n%s", want, got)
		}
	}
	path := crossPath(saved, "llm:ollama/qwen3:8b")
	if strings.ContainsAny(filepath.Base(path), ":/") {
		t.Fatalf("unsafe filename %q", path)
	}
	recs, err := readNDJSON[crossRecord](path)
	if err != nil || len(recs) != len(comps) {
		t.Fatalf("saved %d cross verdicts, err %v", len(recs), err)
	}
	if err := run(ctx, []string{"crosscheck", "-from", saved, "-judge", "heuristic"}, &bytes.Buffer{}); err == nil {
		t.Fatal("the heuristic judge was allowed to re-judge saved pairs")
	}
}
