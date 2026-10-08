package parametric

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Tasks split into two fixed halves, and the dataset refuses held-out ones.
func TestDatasetNeverTrainsOnHeldOutTasks(t *testing.T) {
	var train, held []string
	for _, task := range []string{"bob", "acronym", "anagram", "isogram", "pangram", "leap", "clock", "grains"} {
		if HeldOut(task) {
			held = append(held, task)
		} else {
			train = append(train, task)
		}
	}
	if len(train) == 0 || len(held) == 0 {
		t.Fatalf("the split put everything on one side: %v / %v", train, held)
	}
	dir := t.TempDir()
	if _, _, err := WriteDataset(dir, []Example{{Task: held[0], User: "u", Answer: "a"}}); err == nil {
		t.Fatal("a held-out example was accepted")
	}
	var exs []Example
	for i := 0; i < 12; i++ {
		exs = append(exs, Example{Task: train[i%len(train)], System: "s", User: "solve", Answer: `{"files":{}}`})
	}
	tr, va, err := WriteDataset(dir, exs)
	if err != nil || tr+va != 12 || va == 0 {
		t.Fatalf("%d train, %d valid, %v", tr, va, err)
	}
	f, _ := os.Open(filepath.Join(dir, "train.jsonl"))
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Scan()
	if !strings.Contains(sc.Text(), `"role":"assistant"`) {
		t.Fatalf("not chat format: %s", sc.Text())
	}
}

func runs(tasks map[string]bool, chars int) []Run {
	var rs []Run
	for task, ok := range tasks {
		rs = append(rs, Run{Task: task, Passed: ok, PromptChars: chars})
	}
	return rs
}

// Retention is measured on what the base solved; gain on held-out tasks.
func TestEvaluate(t *testing.T) {
	all := []string{"bob", "acronym", "anagram", "isogram", "pangram", "leap", "clock", "grains"}
	base, text, good, forgetful := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for i, task := range all {
		base[task] = i < 4
		text[task] = i < 6
		good[task] = true
		forgetful[task] = i >= 4
	}
	ev := Evaluate(runs(base, 900), runs(text, 1400), runs(good, 900))
	if !ev.Passes || ev.Retained != 1 || ev.TokensRatio >= 1 || ev.Gain < 0 {
		t.Fatalf("a good adapter: %+v", ev)
	}
	ev = Evaluate(runs(base, 900), runs(text, 1400), runs(forgetful, 900))
	if ev.Passes || !strings.HasPrefix(ev.Why, "forgets") {
		t.Fatalf("a forgetful adapter: %+v", ev)
	}
}

// Using a version and rolling back append to the log; rollback returns to
// what was in use before.
func TestRegistryRollback(t *testing.T) {
	r := Registry{Dir: t.TempDir()}
	now := time.Now()
	for _, n := range []string{"v1", "v2"} {
		if err := r.Add(Version{Name: n, At: now}); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Add(Version{Name: "v1"}); err == nil {
		t.Fatal("a duplicate version was added")
	}
	if err := r.Use("v3", "", now); err == nil {
		t.Fatal("an unknown version was put in use")
	}
	_ = r.Use("v1", "first", now)
	_ = r.Use("v2", "second", now)
	prev, err := r.Rollback("forgets", now)
	if err != nil || prev != "v1" {
		t.Fatalf("rollback to %q: %v", prev, err)
	}
	s, _ := r.Load()
	if s.Active != "v1" {
		t.Fatalf("active %q", s.Active)
	}
	if prev, _ := r.Rollback("again", now); prev != "" {
		t.Fatalf("second rollback to %q, want the base model", prev)
	}
	if _, err := r.Rollback("nothing left", now); err == nil {
		t.Fatal("rolled back past the start")
	}
}
