package mine

import (
	"testing"
	"time"

	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)

func ep(i int, text string, steps ...types.Step) types.Episode {
	at := t0.Add(time.Duration(i) * time.Minute)
	return types.Episode{ID: types.NewID(at), Request: types.Request{Text: text}, Steps: steps, StartedAt: at, EndedAt: at}
}

// Each recurring failure is sorted by what could answer it: a person
// undoing or declining a kind of step is evidence for a guard; the same step
// failing is a mistake, for a lesson; a refusal nobody made is left out.
func TestPatternsSortByWhatAnswersThem(t *testing.T) {
	push := types.Step{Tool: "shell", Subject: "cd repo && git push --force", OK: true}
	refused := types.Step{Tool: "file", Action: "write", Subject: "write notes/NOTES.md", Err: "refused: the confirmation this step needs was not given"}
	missing := types.Step{Tool: "file", Action: "read", Subject: "read /x/y/z.go", Err: "stat /x/y/z.go: no such file or directory"}
	var eps []types.Episode
	outs := map[types.ID][]types.Outcome{}
	for i := range 3 {
		e := ep(i, "ship it "+string(rune('a'+i)), push)
		eps = append(eps, e)
		outs[e.ID] = []types.Outcome{{Kind: types.OutcomeUndone}}
	}
	for i := range 3 {
		e := ep(10+i, "write my notes "+string(rune('a'+i)), refused)
		eps = append(eps, e)
		outs[e.ID] = []types.Outcome{{Kind: types.OutcomeDeclined}}
	}
	for i := range 4 {
		eps = append(eps, ep(20+i, "make a scratch file "+string(rune('a'+i)), refused)) // the daemon refused
	}
	for i := range 3 {
		eps = append(eps, ep(30+i, "explain z "+string(rune('a'+i)), missing))
	}
	eps = append(eps, ep(40, "once", types.Step{Tool: "shell", Subject: "make", Err: "exit status 2"})) // not recurring

	ps := Patterns(eps, outs, Options{})
	got := map[string]Pattern{}
	for _, p := range ps {
		got[string(p.Kind)+"/"+p.Why] = p
	}
	if p := got["guard/undone"]; p.Step != "shell: git push" || p.Count != 3 || p.Proposal == "" {
		t.Errorf("undone: %+v", p)
	}
	if p := got["guard/declined"]; p.Step != "file write *.md" || p.Count != 3 {
		t.Errorf("declined: %+v", p)
	}
	if p := got["excluded/refused unattended"]; p.Count != 4 || p.Proposal != "" {
		t.Errorf("unattended: %+v", p)
	}
	if p := got["lesson/failed"]; p.Step != "file read *.go" || p.Error != "stat <path>: no such file or directory" || p.Count != 3 {
		t.Errorf("failed: %+v", p)
	}
	if len(ps) != 4 || ps[0].Kind != KindGuard || ps[len(ps)-1].Kind != KindExcluded {
		t.Errorf("patterns %+v", ps)
	}
}

// The same request repeated is one request: a pattern needs distinct ones.
func TestPatternsNeedDistinctRequests(t *testing.T) {
	missing := types.Step{Tool: "file", Action: "read", Subject: "read a.go", Err: "no such file"}
	var eps []types.Episode
	for i := range 5 {
		eps = append(eps, ep(i, "explain a", missing))
	}
	if ps := Patterns(eps, nil, Options{}); len(ps) != 0 {
		t.Fatalf("one request repeated made a pattern: %+v", ps)
	}
}
