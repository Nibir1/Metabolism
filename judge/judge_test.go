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

// stubJudge records whether it was asked.
type stubJudge struct{ asked *int }

func (stubJudge) Name() string { return "stub" }
func (s stubJudge) Compare(context.Context, types.Episode, []types.Outcome, types.ReplayResult, types.ReplayResult) (types.Verdict, error) {
	*s.asked++
	return types.Verdict{Prefer: types.PreferWith, Confidence: 0.9, Reason: "model"}, nil
}

func TestOutcomeJudgeComparesFactsFirst(t *testing.T) {
	asked := 0
	o := Outcome{Model: stubJudge{asked: &asked}}
	answered := types.ReplayResult{Rounds: 3, End: types.ReplayEndAnswered, Text: "it does X"}
	ranOut := types.ReplayResult{Rounds: 4, End: types.ReplayEndBudget}
	acting := types.ReplayResult{Rounds: 1, End: types.ReplayEndUnexecuted}
	firstPlan := types.ReplayResult{}
	ctx := context.Background()

	if v, _ := o.Compare(ctx, types.Episode{}, nil, answered, ranOut); v.Prefer != types.PreferWith || v.Confidence < 0.9 {
		t.Errorf("answered vs ran out: %+v", v)
	}
	if v, _ := o.Compare(ctx, types.Episode{}, nil, ranOut, answered); v.Prefer != types.PreferWithout {
		t.Errorf("ran out vs answered: %+v", v)
	}
	if v, _ := o.Compare(ctx, types.Episode{}, nil, answered, answered); v.Prefer != types.PreferNeither {
		t.Errorf("both answered: %+v", v)
	}
	if v, _ := o.Compare(ctx, types.Episode{}, nil, ranOut, ranOut); v.Prefer != types.PreferNeither {
		t.Errorf("both ran out: %+v", v)
	}
	if asked != 0 {
		t.Fatalf("the model was asked %d times about measurable outcomes", asked)
	}
	// The same end and the same action: facts only, never the model (D-029).
	for _, pair := range [][2]types.ReplayResult{{acting, acting}, {firstPlan, firstPlan}} {
		if v, _ := o.Compare(ctx, types.Episode{}, nil, pair[0], pair[1]); v.Prefer != types.PreferNeither || !strings.HasPrefix(v.Reason, "facts") {
			t.Errorf("the same action: %+v", v)
		}
	}
	if asked != 0 {
		t.Fatalf("the model was asked %d times about plans that end on the same action", asked)
	}
	// Different actions, or ends that cannot be compared: the model.
	runs := func(cmd string) types.ReplayResult {
		return types.ReplayResult{Rounds: 1, End: types.ReplayEndUnexecuted, Steps: []types.Step{{Tool: "shell", Subject: cmd}}}
	}
	for _, pair := range [][2]types.ReplayResult{{runs("go test ./..."), runs("go test ./ledger")}, {answered, acting}} {
		if v, _ := o.Compare(ctx, types.Episode{}, nil, pair[0], pair[1]); v.Reason != "model" {
			t.Errorf("a pair with different actions was not given to the model: %+v", v)
		}
	}
	if asked != 2 || o.Name() != "outcome+stub" {
		t.Fatalf("asked %d, name %q", asked, o.Name())
	}
}

// Facts decide plans that end the same way: quoting is not an action, a
// failed step counts, model calls do not (D-029).
func TestOutcomeJudgeFacts(t *testing.T) {
	asked := 0
	o := Outcome{Model: stubJudge{asked: &asked}}
	ctx := context.Background()
	cd := func(subject string, calls int) types.ReplayResult {
		return types.ReplayResult{Rounds: 1, End: types.ReplayEndUnexecuted, Usage: types.Usage{ModelCalls: calls},
			Steps: []types.Step{{Tool: "shell", Subject: subject}}}
	}
	if v, _ := o.Compare(ctx, types.Episode{}, nil, cd(`cd /a/b && go test ./...`, 2), cd(`cd "/a/b"  &&  go test ./...`, 2)); v.Prefer != types.PreferNeither {
		t.Errorf("quoting only: %+v", v)
	}
	if v, _ := o.Compare(ctx, types.Episode{}, nil, cd("x", 4), cd("x", 8)); v.Prefer != types.PreferNeither {
		t.Errorf("call counts vary between two replays of one baseline; they are not a verdict: %+v", v)
	}
	failing := cd("x", 2)
	failing.Steps = append([]types.Step{{Tool: "file", Action: "read", Subject: "nope", Err: "no such file"}}, failing.Steps...)
	if v, _ := o.Compare(ctx, types.Episode{}, nil, failing, cd("x", 2)); v.Prefer != types.PreferWithout || !strings.Contains(v.Reason, "failed step") {
		t.Errorf("a failed step: %+v", v)
	}
	if asked != 0 {
		t.Fatalf("the model was asked %d times", asked)
	}
}

// gradingJudge grades plans one at a time.
type gradingJudge struct {
	stubJudge
	graded *int
}

func (g gradingJudge) Grade(context.Context, types.Episode, []types.Outcome, types.ReplayResult, types.ReplayResult) (types.Verdict, error) {
	*g.graded++
	return types.Verdict{Prefer: types.PreferNeither, Confidence: 1, Reason: "graded"}, nil
}

// A model that can grade each plan on its own is asked that, not to choose.
func TestOutcomeJudgePrefersGrading(t *testing.T) {
	asked, graded := 0, 0
	o := Outcome{Model: gradingJudge{stubJudge{asked: &asked}, &graded}}
	a := types.ReplayResult{End: types.ReplayEndUnexecuted, Steps: []types.Step{{Tool: "shell", Subject: "go test ./..."}}}
	b := types.ReplayResult{End: types.ReplayEndUnexecuted, Steps: []types.Step{{Tool: "shell", Subject: "go vet ./..."}}}
	if v, _ := o.Compare(context.Background(), types.Episode{}, nil, a, b); v.Reason != "graded" || graded != 1 || asked != 0 {
		t.Fatalf("verdict %+v, graded %d, asked %d", v, graded, asked)
	}
}

func TestUnsupportedClaims(t *testing.T) {
	edit := types.Step{Tool: "file", Action: "edit", Subject: "retry.go", OK: true}
	test := types.Step{Tool: "shell", Subject: "go test ./...", OK: true}
	build := types.Step{Tool: "shell", Subject: "go build ./...", OK: true}
	read := types.Step{Tool: "file", Action: "read", Subject: "go.mod", OK: true}
	cases := []struct {
		name  string
		r     types.ReplayResult
		unsup bool
	}{
		{"false fix: edit, no test, claims pass", types.ReplayResult{Steps: []types.Step{edit}, Text: "Done; the tests pass."}, true},
		{"stale evidence: test before edit", types.ReplayResult{Steps: []types.Step{test, edit}, Text: "Tests pass."}, true},
		{"no steps, claims it builds", types.ReplayResult{Text: "It builds cleanly."}, true},
		{"real fix: edit then test", types.ReplayResult{Steps: []types.Step{edit, test}, Text: "Made the change, then ran the tests after it: they pass."}, false},
		{"build then claim", types.ReplayResult{Steps: []types.Step{read, build}, Text: "Built the Go module; it compiles cleanly."}, false},
		{"no claim at all", types.ReplayResult{Steps: []types.Step{edit}, Text: "Which file should I change?"}, false},
		{"a question about passing is not a claim of success", types.ReplayResult{Text: "Should the tests be rerun?"}, false},
	}
	for _, c := range cases {
		if got := UnsupportedClaim(c.r) != ""; got != c.unsup {
			t.Errorf("%s: unsupported = %v (%q)", c.name, got, UnsupportedClaim(c.r))
		}
	}
}

// The same facts and different answers go to the model; the same answer,
// reworded slightly, stays a tie without asking it (D-033).
func TestOutcomeJudgeComparesDifferentAnswers(t *testing.T) {
	asked := 0
	o := Outcome{Model: stubJudge{asked: &asked}}
	ctx := context.Background()
	reply := func(text string) types.ReplayResult {
		return types.ReplayResult{Rounds: 2, End: types.ReplayEndAnswered, Text: text,
			Steps: []types.Step{{Tool: "file", Action: "read", Subject: "verify/verify.go", OK: true}, {Tool: "response", OK: true}}}
	}
	a := reply("verify runs the nutrient test: it replays episodes with and without a lesson and judges each pair.")
	reworded := reply("Verify runs the nutrient test: it replays episodes with and without a lesson, and judges each pair.")
	other := reply("It is a package.")
	if v, _ := o.Compare(ctx, types.Episode{}, nil, a, reworded); v.Prefer != types.PreferNeither || asked != 0 {
		t.Fatalf("the same answer reworded: %+v, asked %d", v, asked)
	}
	v, _ := o.Compare(ctx, types.Episode{}, nil, a, other)
	if asked != 1 || v.Prefer != types.PreferWith || !strings.HasPrefix(v.Reason, "answers differ") {
		t.Fatalf("different answers: %+v, asked %d", v, asked)
	}
}

func TestSameAnswer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"", "", true},
		{"", "something", false},
		{"Run go test ./...", "run GO test ./...", true},
		{"read go.mod for the module path", "read package.json for the scripts", false},
	} {
		if got := SameAnswer(c.a, c.b); got != c.same {
			t.Errorf("SameAnswer(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

// orderBiased always prefers plan A; steady prefers the plan with "good".
type orderBiased struct{}

func (orderBiased) Name() string { return "biased" }
func (orderBiased) ComparePair(_ context.Context, p Pair) (types.Verdict, error) {
	return p.Verdict("a", 0.9, "first"), nil
}

type steady struct{}

func (steady) Name() string { return "steady" }
func (steady) ComparePair(_ context.Context, p Pair) (types.Verdict, error) {
	better := "b"
	if strings.Contains(strings.Join(p.PlanA, " "), "good") {
		better = "a"
	}
	return p.Verdict(better, 0.8, "content"), nil
}

// A preference that follows the order becomes a tie; one that follows the
// plans survives the swap.
func TestBothOrders(t *testing.T) {
	ctx := context.Background()
	pair := Pair{PlanA: []string{"good plan"}, PlanB: []string{"other plan"}, WithIsA: true}
	if v, _ := (Both{Inner: orderBiased{}}).ComparePair(ctx, pair); v.Prefer != types.PreferNeither {
		t.Fatalf("an order-biased judge: %+v", v)
	}
	v, _ := (Both{Inner: steady{}}).ComparePair(ctx, pair)
	if v.Prefer != types.PreferWith || !strings.HasPrefix(v.Reason, "both orders") {
		t.Fatalf("a steady judge: %+v", v)
	}
	if n := (Both{Inner: steady{}}).Name(); n != "both+steady" {
		t.Fatalf("name %q", n)
	}
}
