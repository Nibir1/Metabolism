package ledger

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

const (
	searchReq = "find the settings file, the glob came back empty"
	commitReq = "commit my changes to git"
)

// relevanceFixture is a journal with past turns on two topics and one trial
// lesson learned from the search topic. play adds live turns on a topic:
// delivered and withheld counts, each with how many succeeded.
func relevanceFixture(t *testing.T) (context.Context, *sqlite.Journal, *metabolome.Metabolome, types.Lesson, func(req string, n, wins int, delivered bool)) {
	t.Helper()
	ctx := context.Background()
	j, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "j.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	at := t0.Add(-100 * time.Hour)
	var sources []types.ID
	for i := range 40 {
		at = at.Add(time.Minute)
		ep := episode(i, at, types.RunDone, nil, nil)
		ep.Request.Text = commitReq
		if i%2 == 0 {
			ep.Request.Text = searchReq
			if len(sources) < 3 {
				sources = append(sources, ep.ID)
			}
		}
		if err := j.AppendEpisode(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		t.Fatal(err)
	}
	m.SetClock(func() time.Time { return t0.Add(-50 * time.Hour) })
	l, err := m.Create(ctx, types.Lesson{Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
		Text: "When a glob comes back empty, say so instead of searching again.", Sources: sources,
		Provenance: types.ProvUserTyped}, "test")
	if err != nil {
		t.Fatal(err)
	}
	ev, _ := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
		Result: types.ResultPass, Summary: "replay", Compared: 3, Wins: 3})
	if err := m.Move(ctx, l.ID, types.StateTrial, "replay passed", ev.ID); err != nil {
		t.Fatal(err)
	}
	e, _ := m.Get(l.ID)
	l = e.Lesson
	at = t0.Add(-40 * time.Hour)
	play := func(req string, n, wins int, delivered bool) {
		for i := range n {
			at = at.Add(time.Minute)
			end := types.RunFailed
			if i < wins {
				end = types.RunDone
			}
			ep := episode(i, at, end, nil, nil)
			ep.Request.Text = req
			if delivered {
				ep.Exposure = []types.ID{l.ID}
			} else {
				ep.Withheld = []types.ID{l.ID}
			}
			if err := j.AppendEpisode(ctx, ep); err != nil {
				t.Fatal(err)
			}
		}
	}
	return ctx, j, m, l, play
}

// A lesson that helps on its own topic is accepted on the turns that bear on
// it, though the turns on another topic dilute it below the bar (D-026).
func TestRelevanceCreditsTheTurnsThatBearOnTheLesson(t *testing.T) {
	ctx, j, m, l, play := relevanceFixture(t)
	play(searchReq, 40, 36, true)   // 90%
	play(searchReq, 20, 10, false)  // 50%
	play(commitReq, 200, 140, true) // 70%, the lesson changes nothing here
	play(commitReq, 100, 70, false)

	all, err := Credit(ctx, j, m, t0, Rule{AllTurns: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := decide(l, *all.Lessons[l.ID], Rule{}.Defaults()); d.To != "" {
		t.Fatalf("setup: over every turn the help should be diluted below the bar, got %q: %s", d.To, d.Reason)
	}
	related, err := Credit(ctx, j, m, t0, Rule{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := related.Lessons[l.ID]
	if got.Exposed != 40 || got.Withheld != 20 || got.Unrelated != 300 || got.All == nil || got.All.Exposed != 240 {
		t.Fatalf("tally: %d delivered, %d withheld, %d unrelated, all %+v", got.Exposed, got.Withheld, got.Unrelated, got.All)
	}
	d := decide(l, *got, Rule{}.Defaults())
	if d.To != types.StateAccepted || !strings.Contains(d.Reason, "bear on it") {
		t.Fatalf("to %q: %s", d.To, d.Reason)
	}
}

// Harm anywhere counts: a lesson that does harm on turns that do not bear on
// it is still eliminated, on every turn.
func TestRelevanceKeepsHarmElsewhereInView(t *testing.T) {
	ctx, j, m, l, play := relevanceFixture(t)
	play(searchReq, 40, 28, true) // 70% vs 70%: no help, no harm here
	play(searchReq, 20, 14, false)
	play(commitReq, 200, 60, true) // 30% vs 70%: harm elsewhere
	play(commitReq, 100, 70, false)

	related, err := Credit(ctx, j, m, t0, Rule{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d := decide(l, *related.Lessons[l.ID], Rule{}.Defaults())
	if d.To != types.StateEliminated || !strings.Contains(d.Reason, "over every turn") {
		t.Fatalf("to %q: %s", d.To, d.Reason)
	}
}
