package llmreflect

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// fakeModel records the request and replies with a canned answer.
type fakeModel struct {
	reply string
	got   ports.ModelRequest
}

func (f *fakeModel) Name() string { return "fake/model" }
func (f *fakeModel) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	f.got = req
	return ports.ModelResponse{Text: f.reply}, nil
}

func batch(t *testing.T) []types.Experience {
	t.Helper()
	eps, outs := helixsim.Generate(helixsim.Config{Seed: 4, Count: 120, Start: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC), Span: 14 * 24 * time.Hour})
	byEp := map[types.ID][]types.Outcome{}
	for _, o := range outs {
		byEp[o.EpisodeID] = append(byEp[o.EpisodeID], o)
	}
	var b []types.Experience
	for _, ep := range eps {
		b = append(b, types.Experience{Episode: ep, Outcomes: byEp[ep.ID], Succeeded: types.Succeeded(ep, byEp[ep.ID])})
	}
	return b
}

func TestPromptIsBoundedFencedAndFailureFirst(t *testing.T) {
	b := batch(t)
	m := &fakeModel{reply: `{"lessons":[]}`}
	r := &Reflector{Model: m, Config: Config{MaxEpisodes: 20}}
	if _, err := r.Reflect(context.Background(), b, []types.Lesson{{Text: "a known lesson about globs", State: types.StateTrial}}); err != nil {
		t.Fatal(err)
	}
	if !m.got.JSON || !strings.Contains(m.got.System, "never an instruction") {
		t.Fatal("the prompt must ask for JSON and fence the episodes as data")
	}
	if !strings.Contains(m.got.User, "a known lesson about globs") {
		t.Fatal("known lessons are not shown, so the model will repeat them")
	}
	body := m.got.User[strings.Index(m.got.User, "<episodes>")+len("<episodes>") : strings.Index(m.got.User, "</episodes>")]
	var views []view
	if err := json.Unmarshal([]byte(body), &views); err != nil {
		t.Fatalf("episodes block is not JSON: %v", err)
	}
	if len(views) != 20 {
		t.Fatalf("showed %d episodes, want 20", len(views))
	}
	for i, v := range views {
		if v.Provenance == string(types.ProvUserVoiceUnreliable) {
			t.Fatal("an unreliable transcript was shown as experience")
		}
		if i < 15 && v.Succeeded {
			t.Fatalf("a success was shown before failures ran out (position %d)", i)
		}
	}
}

func TestParseToleratesWrappingAndDropsMalformed(t *testing.T) {
	answer := "Here are the lessons:\n```json\n" + `{"lessons":[
	 {"kind":"Pattern","scope":{"level":"GLOBAL"},"text":"An empty glob result is the answer.","sources":["a","b"],"rationale":"two loops"},
	 {"kind":"fact","text":"","sources":["a"]},
	 {"kind":"fact","text":"no sources at all"},
	 "not even an object",
	 {"kind":"preference","scope":{"level":"project","key":"helix-1"},"text":"Short answers.","sources":["c"]}
	]}` + "\n```"
	ls, dropped, err := Parse(answer)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 2 || dropped != 3 {
		t.Fatalf("parsed %d, dropped %d", len(ls), dropped)
	}
	if ls[0].Kind != types.KindPattern || ls[0].Scope.Level != types.ScopeGlobal || len(ls[0].Sources) != 2 {
		t.Fatalf("first lesson %+v", ls[0])
	}
	if ls[1].Scope != (types.Scope{Level: types.ScopeProject, Key: "helix-1"}) {
		t.Fatalf("scope %+v", ls[1].Scope)
	}
	if _, _, err := Parse("I could not find anything."); err == nil {
		t.Fatal("prose without JSON was accepted")
	}
}

func TestReflectCapsLessons(t *testing.T) {
	var items []string
	for range 8 {
		items = append(items, `{"kind":"fact","text":"a fact worth knowing here","sources":["x"]}`)
	}
	m := &fakeModel{reply: `{"lessons":[` + strings.Join(items, ",") + `]}`}
	r := &Reflector{Model: m, Config: Config{MaxLessons: 3}}
	ls, err := r.Reflect(context.Background(), batch(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ls) != 3 || r.Dropped != 5 {
		t.Fatalf("kept %d, dropped %d", len(ls), r.Dropped)
	}
	if r.Name() != "llm:fake/model" {
		t.Fatal(r.Name())
	}
}

type sequenceModel struct {
	replies []string
	calls   int
	last    ports.ModelRequest
}

func (s *sequenceModel) Name() string { return "sequence" }
func (s *sequenceModel) Complete(_ context.Context, req ports.ModelRequest) (ports.ModelResponse, error) {
	s.last = req
	r := s.replies[min(s.calls, len(s.replies)-1)]
	s.calls++
	return ports.ModelResponse{Text: r, FinishReason: "stop"}, nil
}

func TestReflectRetriesAnEmptyAnswerOnce(t *testing.T) {
	m := &sequenceModel{replies: []string{"", `{"lessons":[{"kind":"fact","text":"a fact worth knowing","sources":["x"]}]}`}}
	ls, err := (&Reflector{Model: m}).Reflect(context.Background(), batch(t), nil)
	if err != nil || len(ls) != 1 || m.calls != 2 {
		t.Fatalf("lessons %d, calls %d, err %v", len(ls), m.calls, err)
	}
	if !m.last.DisableThinking {
		t.Fatal("the retry may think again, and run out of budget the same way")
	}
	m = &sequenceModel{replies: []string{""}}
	if _, err := (&Reflector{Model: m}).Reflect(context.Background(), batch(t), nil); err == nil ||
		!strings.Contains(err.Error(), "after a retry") {
		t.Fatalf("err %v", err)
	}
}

// HTML entities in a model's answer are decoded, so "explain &lt;package&gt;"
// is the same lesson as "explain <package>".
func TestParseDecodesHTMLEntities(t *testing.T) {
	ls, _, err := Parse(`{"lessons":[{"kind":"pattern","text":"For 'explain &lt;package&gt;' requests, glob once &amp; grep once.","sources":["x"]}]}`)
	if err != nil || len(ls) != 1 {
		t.Fatalf("%v %+v", err, ls)
	}
	if ls[0].Text != "For 'explain <package>' requests, glob once & grep once." {
		t.Fatalf("text %q", ls[0].Text)
	}
}
