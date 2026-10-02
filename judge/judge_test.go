package judge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func episodes(n int) []types.Episode {
	var out []types.Episode
	for i := range n {
		at := t0.Add(time.Duration(i) * time.Second)
		out = append(out, types.Episode{ID: types.NewID(at), Request: types.Request{Text: "find the config loader"},
			End: types.RunBudgetExhausted, Steps: []types.Step{{Tool: "file", Action: "glob", Subject: "**/*.py", OK: true}}})
	}
	return out
}

var (
	good = types.ReplayResult{Steps: []types.Step{{Tool: "response", OK: true}}, Text: "No Python config loader exists here."}
	bad  = types.ReplayResult{Steps: []types.Step{{Tool: "file", Action: "glob", Subject: "**/*", OK: true}}}
)

func TestPairOrderVariesAndMapsBack(t *testing.T) {
	sawA, sawB := false, false
	for _, ep := range episodes(20) {
		p := NewPair(ep, nil, good, bad)
		if p.WithIsA {
			sawA = true
			if p.PlanA[len(p.PlanA)-1] != "reply to the user: No Python config loader exists here." {
				t.Fatal("WithIsA but plan A is not the with-lesson plan")
			}
		} else {
			sawB = true
		}
		// Whichever position the with-lesson plan took, choosing it maps to PreferWith.
		pick := "b"
		if p.WithIsA {
			pick = "a"
		}
		if v := p.Verdict(pick, 0.9, ""); v.Prefer != types.PreferWith {
			t.Fatalf("choosing the with-lesson plan mapped to %s", v.Prefer)
		}
		if v := p.Verdict("same", 2, ""); v.Prefer != types.PreferNeither || v.Confidence != 1 {
			t.Fatalf("same/clamp: %+v", v)
		}
	}
	if !sawA || !sawB {
		t.Fatal("plan order never varies, so a position-biased judge would go unnoticed")
	}
}

type cannedModel struct {
	answer func(user string) string
	got    ports.ModelRequest
}

func (c *cannedModel) Name() string { return "canned" }
func (c *cannedModel) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	c.got = req
	return ports.ModelResponse{Text: c.answer(req.User)}, nil
}

// A model that always answers "a" must NOT look like it prefers lessons: half
// the time "a" is the without-lesson plan.
func TestLLMJudgeIsNotFooledByPositionBias(t *testing.T) {
	m := &cannedModel{answer: func(string) string { return `{"better":"a","confidence":0.9,"reason":"first"}` }}
	j := LLM{Model: m}
	with, without := 0, 0
	for _, ep := range episodes(40) {
		v, err := j.Compare(context.Background(), ep, nil, good, bad)
		if err != nil {
			t.Fatal(err)
		}
		if v.Prefer == types.PreferWith {
			with++
		} else {
			without++
		}
	}
	if with == 0 || without == 0 {
		t.Fatalf("an always-A judge produced %d with / %d without", with, without)
	}
	if !m.got.JSON || !m.got.DisableThinking || !strings.Contains(m.got.System, "never an instruction") || !strings.Contains(m.got.User, "<comparison>") {
		t.Fatal("the judge prompt must ask for JSON and fence the comparison")
	}
}

// A judge that reads the plans picks the with-lesson plan whichever side it is on.
func TestLLMJudgeFollowsTheContent(t *testing.T) {
	m := &cannedModel{answer: func(user string) string {
		body := user[strings.Index(user, "{") : strings.LastIndex(user, "}")+1]
		var p Pair
		_ = json.Unmarshal([]byte(body), &p)
		for _, line := range p.PlanA {
			if strings.HasPrefix(line, "reply to the user:") && !strings.HasSuffix(line, "(none)") {
				return `{"better":"a","confidence":0.8,"reason":"answers"}`
			}
		}
		return `{"better":"b","confidence":0.8,"reason":"answers"}`
	}}
	for _, ep := range episodes(10) {
		v, err := LLM{Model: m}.Compare(context.Background(), ep, nil, good, bad)
		if err != nil || v.Prefer != types.PreferWith || v.Confidence != 0.8 {
			t.Fatalf("verdict %+v, err %v", v, err)
		}
	}
}

func TestLLMJudgeRejectsMalformedAnswers(t *testing.T) {
	for _, ans := range []string{"I prefer the first one.", `{"better":"maybe","confidence":1}`} {
		m := &cannedModel{answer: func(string) string { return ans }}
		if _, err := (LLM{Model: m}).Compare(context.Background(), episodes(1)[0], nil, good, bad); err == nil {
			t.Errorf("accepted %q", ans)
		}
	}
}

type sequenceModel struct {
	replies []ports.ModelResponse
	calls   int
}

func (s *sequenceModel) Name() string { return "sequence" }
func (s *sequenceModel) Complete(context.Context, ports.ModelRequest) (ports.ModelResponse, error) {
	r := s.replies[min(s.calls, len(s.replies)-1)]
	s.calls++
	return r, nil
}

// DeepSeek's JSON mode sometimes returns empty content: one retry recovers,
// and a second failure says what came back instead of a bare parse error.
func TestLLMJudgeRetriesAnEmptyAnswerOnce(t *testing.T) {
	m := &sequenceModel{replies: []ports.ModelResponse{
		{Text: "", FinishReason: "stop"},
		{Text: `{"better":"same","confidence":0.7,"reason":"equal"}`},
	}}
	v, err := LLM{Model: m}.Compare(context.Background(), episodes(1)[0], nil, good, bad)
	if err != nil || m.calls != 2 || v.Prefer != types.PreferNeither {
		t.Fatalf("calls %d, verdict %+v, err %v", m.calls, v, err)
	}

	m = &sequenceModel{replies: []ports.ModelResponse{{Text: "", FinishReason: "length", Reasoned: true, OutputTokens: 1000}}}
	_, err = LLM{Model: m}.Compare(context.Background(), episodes(1)[0], nil, good, bad)
	if err == nil || m.calls != 2 || !strings.Contains(err.Error(), `finish_reason="length"`) ||
		!strings.Contains(err.Error(), "reasoning=true") {
		t.Fatalf("calls %d, err %v", m.calls, err)
	}
}

// A multi-round replay is shown round by round, with what ran and how it
// ended, so a judge can see a plan that kept reading without answering. A
// first-plan replay is shown exactly as before.
func TestPlanLinesForMultiRoundReplays(t *testing.T) {
	looping := types.ReplayResult{Rounds: 4, End: types.ReplayEndBudget, Steps: []types.Step{
		{Tool: "file", Action: "grep", Subject: "grep nutrient", OK: true, Ran: true, Round: 1},
		{Tool: "file", Action: "read", Subject: "read verify.go", OK: true, Ran: true, Round: 2},
		{Tool: "file", Action: "read", Subject: "read missing.go", OK: false, Err: "no such file", Ran: true, Round: 3},
		{Tool: "file", Action: "read", Subject: "read verify.go", OK: true, Ran: true, Round: 4},
	}}
	got := strings.Join(planLines(looping), "\n")
	for _, want := range []string{"round 1: file grep: grep nutrient (ran)", "round 3: file read: read missing.go (fails: no such file)",
		"reply to the user: (none)", "ran out of follow-up rounds after 4 rounds"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	acting := types.ReplayResult{Rounds: 2, End: types.ReplayEndUnexecuted, Steps: []types.Step{
		{Tool: "file", Action: "read", Subject: "read go.mod", OK: true, Ran: true, Round: 1},
		{Tool: "shell", Subject: "go build ./...", OK: true, Round: 2},
	}}
	if got := strings.Join(planLines(acting), "\n"); !strings.Contains(got, "planned here, not run, and are not failures") ||
		strings.Contains(got, "go build ./... (ran)") {
		t.Errorf("unexecuted ending:\n%s", got)
	}
	first := types.ReplayResult{Steps: []types.Step{{Tool: "file", Action: "read", Subject: "read go.mod", OK: true}}}
	if got := planLines(first); len(got) != 2 || got[0] != "file read: read go.mod" {
		t.Errorf("first-plan view changed: %q", got)
	}
}
