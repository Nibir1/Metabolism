package audit

import (
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/bench"
)

func find(t *testing.T, r Report, id string) Criterion {
	t.Helper()
	for _, c := range r.Criteria {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no criterion %s", id)
	return Criterion{}
}

// With no evidence at all, nothing is met: everything waits, is manual or is
// not built, and the audit waits on the rest.
func TestEmptyEvidenceMeetsNothing(t *testing.T) {
	r := Evaluate(Evidence{Now: time.Now()})
	if r.Stage != StageNone {
		t.Fatalf("stage %s", r.Stage)
	}
	if n := r.Count(); n[Met] != 0 || n[Short] != 0 {
		t.Fatalf("counts %v", n)
	}
	if c := find(t, r, "9.audit"); c.Status != Waiting {
		t.Fatalf("audit %+v", c)
	}
	for _, id := range []string{"6.transfer", "7.retention", "8.second-host", "0.public-benchmark", "2.planted-real"} {
		if c := find(t, r, id); c.Status != Unbuilt {
			t.Fatalf("%s should be unbuilt: %+v", id, c)
		}
	}
}

// The baseline needs both the count and the span, then the commit.
func TestBaselineStages(t *testing.T) {
	short := Real{Episodes: 400, LessonsOff: 400, LessonsOffSpan: 10 * 24 * time.Hour}
	if s := StageOf(short); s != StageBaseline {
		t.Fatalf("10 days: %s", s)
	}
	full := Real{Episodes: 400, LessonsOff: 320, LessonsOffSpan: 22 * 24 * time.Hour}
	if s := StageOf(full); s != StageLearning {
		t.Fatalf("22 days, 320 episodes: %s", s)
	}
	c := find(t, Evaluate(Evidence{Real: full}), "0.real-baseline")
	if c.Status != Short || !strings.Contains(c.Next, "helix-baseline") {
		t.Fatalf("uncommitted baseline %+v", c)
	}
	if c := find(t, Evaluate(Evidence{Real: full, BaselineCommitted: true}), "0.real-baseline"); c.Status != Met {
		t.Fatalf("committed baseline %+v", c)
	}
}

// Labels: too few wait, uniform ones are refused, and the bar is the
// labeller's own consistency (D-036): 80% of their self-agreement and κ.
func TestJudgeAgreement(t *testing.T) {
	self := bench.Agreement{N: 20, Rate: 0.75, Kappa: 0.62}
	cases := []struct {
		name string
		l    Labels
		want Status
	}{
		{"too few", Labels{Labelled: 10, Agreement: bench.Agreement{Rate: 0.9}}, Waiting},
		{"uniform", Labels{Labelled: 40, Uniform: true}, Short},
		{"no retest yet", Labels{Labelled: 65, Agreement: bench.Agreement{Rate: 0.9, Kappa: 0.8}}, Short},
		{"today's judge", Labels{Labelled: 65, Self: self, Agreement: bench.Agreement{Rate: 0.54, Kappa: 0.31}}, Short},
		{"rate enough, κ not", Labels{Labelled: 65, Self: self, Agreement: bench.Agreement{Rate: 0.62, Kappa: 0.40}}, Short},
		{"both enough", Labels{Labelled: 65, Self: self, Agreement: bench.Agreement{Rate: 0.62, Kappa: 0.52}}, Met},
	}
	for _, tc := range cases {
		if c := find(t, Evaluate(Evidence{Labels: tc.l}), "2.judge-agreement"); c.Status != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, c.Status, tc.want)
		}
	}
}

// The planning-mode criterion follows the measurement's verdict.
func TestPlanningMode(t *testing.T) {
	for verdict, want := range map[string]Status{
		"passes: thinking off costs 60% less":   Met,
		"stop: reasoning is 9% of planner cost": Short,
		"not shown harmless: P(...) is 0.49":    Short,
	} {
		th := bench.Thinking{Episodes: 80, Flipped: 80, Verdict: verdict}
		if c := find(t, Evaluate(Evidence{Thinking: th}), "5b.planning-mode"); c.Status != want {
			t.Errorf("%q: %s, want %s", verdict, c.Status, want)
		}
	}
}

// The markdown names every criterion and the next steps.
func TestMarkdown(t *testing.T) {
	r := Evaluate(Evidence{Now: time.Date(2026, 10, 8, 14, 0, 0, 0, time.UTC)})
	md := Markdown(r)
	for _, c := range r.Criteria {
		if !strings.Contains(md, c.Name) {
			t.Fatalf("markdown lacks %q", c.Name)
		}
	}
	if !strings.Contains(md, "## Next") || !strings.Contains(md, "not built") {
		t.Fatalf("markdown:\n%s", md)
	}
}

// The rubric scores a criterion 1 only when everything it needs is met; the
// three that rest on code and tests score 1 from the start (C2 once a
// lesson exists).
func TestRubric(t *testing.T) {
	r := Evaluate(Evidence{LessonsLearned: 3})
	got := map[string]int{}
	for _, it := range r.Rubric {
		got[it.C] = it.Score
	}
	for c, want := range map[string]int{"C1": 0, "C2": 1, "C3": 1, "C4": 0, "C5": 0, "C6": 1, "C7": 0, "C8": 0, "C9": 0} {
		if got[c] != want {
			t.Errorf("%s = %d, want %d", c, got[c], want)
		}
	}
	if !strings.Contains(Markdown(r), "Rubric (vision.md): 3 of 9") {
		t.Fatal("markdown lacks the rubric score")
	}
}
