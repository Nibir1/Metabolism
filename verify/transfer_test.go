package verify

import (
	"context"
	"errors"
	"testing"

	"github.com/Nibir1/metabolism/types"
)

// A lesson learned from the first half of the journal helps on the second
// half, which it never saw: the transfer evidence says so, names the
// held-out group, and leaves the lesson's state alone.
func TestTransferOnHeldOutEpisodes(t *testing.T) {
	ctx := context.Background()
	w := newWorld(t)
	early := map[types.ID]bool{}
	for _, ep := range w.eps[:len(w.eps)/2] {
		early[ep.ID] = true
	}
	id := w.lessonFrom(t, "empty-glob-loop", spec(t, "(empty-glob)", true), types.ProvUserTyped)
	for _, src := range func() []types.ID { e, _ := w.m.Get(id); return e.Lesson.Sources }() {
		if !early[src] {
			t.Fatalf("source %s is not in the first half; the test needs it there", src)
		}
	}
	v := w.verifier()
	if _, err := v.Transfer(ctx, w.m, id, []string{"later"}, func(ep types.Episode) bool { return !early[ep.ID] }); err == nil {
		t.Fatal("a candidate is not a tested lesson: transfer must refuse it")
	}
	if rep, err := v.Test(ctx, w.m, id); err != nil || rep.To != types.StateTrial {
		t.Fatalf("nutrient test: %v %+v", err, rep)
	}

	rep, err := v.Transfer(ctx, w.m, id, []string{"later"}, func(ep types.Episode) bool { return !early[ep.ID] })
	if err != nil {
		t.Fatal(err)
	}
	ev := rep.Evidence
	if ev.Kind != types.EvidenceTransfer || !ev.Passed() || len(ev.Groups) != 1 || ev.Groups[0] != "later" {
		t.Fatalf("transfer evidence: %+v", ev)
	}
	for _, eid := range ev.Episodes {
		if early[eid] {
			t.Fatalf("episode %s is not held out", eid)
		}
	}
	if e, _ := w.m.Get(id); e.Lesson.State != types.StateTrial {
		t.Fatalf("transfer moved the lesson to %s", e.Lesson.State)
	}

	// Held-out groups with nothing like the lesson give no evidence at all.
	_, err = v.Transfer(ctx, w.m, id, []string{"none"}, func(types.Episode) bool { return false })
	if !errors.Is(err, ErrNoHeldOut) {
		t.Fatalf("no held-out episodes: %v", err)
	}
}
