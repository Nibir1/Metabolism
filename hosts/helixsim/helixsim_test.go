package helixsim

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/types"
)

var cfg = Config{Seed: 7, Count: 300, Start: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), Span: 28 * 24 * time.Hour}

func TestGenerateIsDeterministic(t *testing.T) {
	e1, o1 := Generate(cfg)
	e2, o2 := Generate(cfg)
	a, _ := json.Marshal([]any{e1, o1})
	b, _ := json.Marshal([]any{e2, o2})
	if string(a) != string(b) {
		t.Fatal("the same seed produced different fixtures")
	}
	e3, _ := Generate(Config{Seed: 8, Count: cfg.Count, Start: cfg.Start, Span: cfg.Span})
	if reflect.DeepEqual(e1, e3) {
		t.Fatal("different seeds produced identical fixtures")
	}
}

func TestGeneratedRecordsAreValid(t *testing.T) {
	eps, outs := Generate(cfg)
	if len(eps) != cfg.Count {
		t.Fatalf("got %d episodes, want %d", len(eps), cfg.Count)
	}
	ids := map[types.ID]bool{}
	seen := map[string]bool{}
	for _, ep := range eps {
		if err := ep.Validate(); err != nil {
			t.Fatalf("episode %s: %v", ep.ID, err)
		}
		if ids[ep.ID] {
			t.Fatalf("duplicate id %s", ep.ID)
		}
		ids[ep.ID] = true
		seen[ep.Attrs["scenario"]] = true
		if ep.EndedAt.After(cfg.Start.Add(cfg.Span + time.Hour)) {
			t.Fatalf("episode %s outside the span", ep.ID)
		}
	}
	for _, o := range outs {
		if err := o.Validate(); err != nil {
			t.Fatalf("outcome %s: %v", o.ID, err)
		}
		if !ids[o.EpisodeID] {
			t.Fatalf("outcome %s labels an unknown episode", o.ID)
		}
	}
	for _, name := range ScenarioNames() {
		if !seen[name] {
			t.Errorf("scenario %s never generated in %d episodes", name, cfg.Count)
		}
	}
}

func TestReplayOnlyTheMatchingLessonHelps(t *testing.T) {
	eps, _ := Generate(cfg)
	var glob types.Episode
	for _, ep := range eps {
		if ep.Attrs["scenario"] == "empty-glob-loop" {
			glob = ep
			break
		}
	}
	ctx := context.Background()
	h := Host{}
	base, err := h.Replay(ctx, glob, nil)
	if err != nil {
		t.Fatal(err)
	}
	unrelated, _ := h.Replay(ctx, glob, []types.Lesson{{Text: "use file-edit instead of sed"}})
	fixed, _ := h.Replay(ctx, glob, []types.Lesson{{ID: "x", Text: "an empty-glob result is an answer"}})

	if !reflect.DeepEqual(base, unrelated) {
		t.Error("an unrelated lesson changed the replay")
	}
	v, _ := judge.Heuristic{}.Compare(ctx, glob, nil, fixed, base)
	if v.Prefer != types.PreferWith {
		t.Errorf("judge did not prefer the fixed replay: %+v", v)
	}
	v, _ = judge.Heuristic{}.Compare(ctx, glob, nil, unrelated, base)
	if v.Prefer != types.PreferNeither {
		t.Errorf("judge preferred an identical replay: %+v", v)
	}
}

func TestReplayCannotFixWhatIsNotThePlannersFault(t *testing.T) {
	eps, _ := Generate(cfg)
	for _, ep := range eps {
		if ep.Attrs["scenario"] != "unreliable-voice" {
			continue
		}
		all := []types.Lesson{}
		for _, s := range scenarios {
			all = append(all, types.Lesson{Text: s.fix})
		}
		got, _ := Host{}.Replay(context.Background(), ep, all)
		if !reflect.DeepEqual(got.Steps, ep.Steps) {
			t.Fatal("a lesson repaired a misheard request")
		}
		return
	}
	t.Fatal("no unreliable-voice episode generated")
}
