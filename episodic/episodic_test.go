package episodic

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/types"
)

var t0 = time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)

var projA = types.Scope{Level: types.ScopeProject, Key: "helix-aaaa"}
var projB = types.Scope{Level: types.ScopeProject, Key: "webapp-bbbb"}

func mk(at time.Time, scope types.Scope, text string, end types.RunEnd, subjects ...string) types.Episode {
	ep := types.Episode{
		ID: types.NewID(at), Host: "t", Scope: scope, StartedAt: at, EndedAt: at.Add(time.Second),
		Request: types.Request{Text: text, Provenance: types.ProvUserTyped}, End: end,
	}
	for _, s := range subjects {
		ep.Steps = append(ep.Steps, types.Step{Tool: "shell", Subject: s, OK: true})
	}
	return ep
}

func texts(cs []Case) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Episode.Request.Text)
	}
	return out
}

func TestRetrieveRanksBySimilarity(t *testing.T) {
	ix := New(Options{})
	ix.Add(mk(t0, projA, "run the parser tests", types.RunDone, "go test ./parser/..."))
	ix.Add(mk(t0, projA, "commit my changes", types.RunDone, "git commit"))
	ix.Add(mk(t0, projA, "explain the sandbox rules", types.RunDone))

	got := ix.Retrieve(Query{Text: "the parser tests are failing", AsOf: t0.Add(time.Hour)})
	if len(got) != 1 || got[0].Episode.Request.Text != "run the parser tests" {
		t.Fatalf("retrieved %v", texts(got))
	}
	if ix.Retrieve(Query{Text: "the and of"}) != nil {
		t.Fatal("a query of stopwords retrieved something")
	}
}

func TestStepSubjectsAreSearchableButWeighLess(t *testing.T) {
	ix := New(Options{})
	ix.Add(mk(t0, projA, "build it", types.RunDone, "go build ./cmd/helix"))
	ix.Add(mk(t0, projA, "helix build is broken", types.RunDone))
	got := ix.Retrieve(Query{Text: "helix", AsOf: t0.Add(time.Hour)})
	if len(got) != 2 || got[0].Episode.Request.Text != "helix build is broken" {
		t.Fatalf("a request match should outrank a subject match: %v", texts(got))
	}
}

func TestScopeAndRecencyBoost(t *testing.T) {
	ix := New(Options{})
	ix.Add(mk(t0, projB, "run the tests", types.RunDone))
	ix.Add(mk(t0, projA, "run the tests", types.RunDone))
	got := ix.Retrieve(Query{Text: "run the tests", Scope: projA, AsOf: t0.Add(time.Hour)})
	if got[0].Episode.Scope != projA {
		t.Fatal("same-scope case should rank first")
	}

	ix = New(Options{})
	ix.Add(mk(t0, projA, "run the tests", types.RunDone))
	ix.Add(mk(t0.Add(60*24*time.Hour), projA, "run the tests", types.RunDone))
	got = ix.Retrieve(Query{Text: "run the tests", AsOf: t0.Add(61 * 24 * time.Hour)})
	if !got[0].Episode.StartedAt.After(t0) {
		t.Fatal("the recent case should rank first")
	}
	if got[1].Score < got[0].Score/2 {
		t.Fatal("old cases fade to half weight, not to nothing")
	}
}

func TestAsOfHidesTheFuture(t *testing.T) {
	ix := New(Options{})
	past := mk(t0, projA, "deploy the site", types.RunDone)
	future := mk(t0.Add(2*time.Hour), projA, "deploy the site", types.RunDone)
	ix.Add(past)
	ix.Add(future)
	// The undo arrives after the query time: as of then, the case succeeded.
	ix.AddOutcome(types.Outcome{ID: types.NewID(t0), EpisodeID: past.ID, At: t0.Add(3 * time.Hour),
		Kind: types.OutcomeUndone, Source: types.SourceDerived})

	got := ix.Retrieve(Query{Text: "deploy", AsOf: t0.Add(time.Hour)})
	if len(got) != 1 || got[0].Episode.ID != past.ID {
		t.Fatalf("as of t+1h only the past episode exists, got %d", len(got))
	}
	if !got[0].Succeeded || len(got[0].Outcomes) != 0 {
		t.Fatal("an outcome from the future leaked into the past")
	}
	later := ix.Retrieve(Query{Text: "deploy", AsOf: t0.Add(4 * time.Hour), Exclude: future.ID})
	if len(later) != 1 || later[0].Succeeded {
		t.Fatalf("as of t+4h the undo is known: %+v", later)
	}
}

func TestUntrustedIsExcludedByDefault(t *testing.T) {
	ix := New(Options{})
	ep := mk(t0, projA, "make a new bench called test", types.RunDone)
	ep.Request.Provenance = types.ProvUserVoiceUnreliable
	ix.Add(ep)
	if got := ix.Retrieve(Query{Text: "new bench"}); len(got) != 0 {
		t.Fatal("an unreliable transcript was retrieved by default")
	}
	if got := ix.Retrieve(Query{Text: "new bench", IncludeUntrusted: true}); len(got) != 1 {
		t.Fatal("IncludeUntrusted did not admit it")
	}
}

func TestOutcomeBeforeEpisodeIsKept(t *testing.T) {
	ix := New(Options{})
	ep := mk(t0, projA, "commit it", types.RunDone)
	ix.AddOutcome(types.Outcome{ID: types.NewID(t0), EpisodeID: ep.ID, At: t0, Kind: types.OutcomeUndone, Source: types.SourceDerived})
	ix.Add(ep)
	if got := ix.Retrieve(Query{Text: "commit"}); len(got) != 1 || got[0].Succeeded {
		t.Fatalf("pending outcome lost: %+v", got)
	}
}

// The index is derived from the journal: after a restart it rebuilds to the
// same answers, and Refresh reads only what is new.
func TestRebuildFromJournalAfterRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "j.db")
	j, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	a := mk(t0, projA, "run the parser tests", types.RunDone)
	b := mk(t0.Add(time.Minute), projA, "fix the parser bug", types.RunFailed)
	for _, ep := range []types.Episode{a, b} {
		if err := j.AppendEpisode(ctx, ep); err != nil {
			t.Fatal(err)
		}
	}
	ix := New(Options{})
	if n, err := ix.Refresh(ctx, j); err != nil || n != 2 {
		t.Fatalf("refresh: %d, %v", n, err)
	}
	before := texts(ix.Retrieve(Query{Text: "parser"}))
	if n, _ := ix.Refresh(ctx, j); n != 0 {
		t.Fatalf("second refresh re-read %d records", n)
	}
	_ = j.Close()

	j, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = j.Close() }()
	rebuilt := New(Options{})
	if _, err := rebuilt.Refresh(ctx, j); err != nil {
		t.Fatal(err)
	}
	after := texts(rebuilt.Retrieve(Query{Text: "parser"}))
	if strings.Join(before, "|") != strings.Join(after, "|") || len(after) != 2 {
		t.Fatalf("before restart %v, after %v", before, after)
	}
}

func TestTokenize(t *testing.T) {
	got := strings.Join(Tokenize("Please fix internal/ai/planner.go, it's BROKEN!"), " ")
	if got != "fix internal ai planner go broken" {
		t.Fatalf("Tokenize = %q", got)
	}
}
