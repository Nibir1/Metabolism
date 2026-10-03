package types

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

func TestNewIDShapeAndOrder(t *testing.T) {
	a := NewID(t0)
	b := NewID(t0.Add(time.Millisecond))
	if !a.Valid() || !b.Valid() {
		t.Fatalf("minted invalid ids %q %q", a, b)
	}
	if a >= b {
		t.Fatalf("ids should sort by creation time: %q !< %q", a, b)
	}
	if c, d := NewID(t0), NewID(t0); c == d {
		t.Fatal("two ids in the same millisecond collided")
	}
	for _, bad := range []ID{"", "xyz", ID(strings.Repeat("A", 28)), ID(strings.Repeat("0", 27))} {
		if bad.Valid() {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func validEpisode() Episode {
	return Episode{
		ID:        NewID(t0),
		Host:      "helix",
		Scope:     Scope{Level: ScopeProject, Key: "helix"},
		StartedAt: t0,
		EndedAt:   t0.Add(time.Second),
		Request:   Request{Text: "run the tests", Provenance: ProvUserTyped},
		End:       RunDone,
	}
}

func TestEpisodeValidate(t *testing.T) {
	if err := validEpisode().Validate(); err != nil {
		t.Fatalf("valid episode rejected: %v", err)
	}

	bad := validEpisode()
	bad.ID = "nope"
	bad.Host = ""
	bad.Scope = Scope{Level: ScopeGlobal, Key: "x"}
	bad.EndedAt = t0.Add(-time.Second)
	bad.Request.Provenance = "rumour"
	bad.End = "sort-of"
	bad.Usage.ModelCalls = -1
	err := bad.Validate()
	if err == nil {
		t.Fatal("invalid episode accepted")
	}
	// Every problem is reported at once, not just the first.
	for _, want := range []string{"id", "host", "scope", "ended_at", "provenance", "run end", "negative"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q: %v", want, err)
		}
	}
}

func TestProvenanceTrust(t *testing.T) {
	trusted := map[Provenance]bool{
		ProvUserTyped: true, ProvUserVoice: true, ProvSelf: true,
		ProvUserVoiceUnreliable: false, ProvToolOutput: false,
		ProvRetrievedWeb: false, ProvFileContent: false,
	}
	for p, want := range trusted {
		if !p.Valid() {
			t.Errorf("%s should be valid", p)
		}
		if p.Trusted() != want {
			t.Errorf("%s trusted = %v, want %v", p, p.Trusted(), want)
		}
	}
}

func TestOutcomeNegative(t *testing.T) {
	neg := map[OutcomeKind]bool{
		OutcomeSuccess: false, OutcomePartial: false,
		OutcomeFailure: true, OutcomeUndone: true, OutcomeDeclined: true, OutcomeRepeated: true,
	}
	for k, want := range neg {
		if k.Negative() != want {
			t.Errorf("%s negative = %v, want %v", k, k.Negative(), want)
		}
	}
}

func TestLifecycle(t *testing.T) {
	allowed := [][2]LessonState{
		{StateCandidate, StateQuarantined},
		{StateCandidate, StateTrial},
		{StateQuarantined, StateTrial},
		{StateTrial, StateAccepted},
		{StateAccepted, StateSuperseded},
		{StateTrial, StateSuperseded},
		{StateSuperseded, StateTrial},
		// Only by the user's rollback, which the metabolome checks (D-028).
		{StateEliminated, StateTrial},
		{StateAccepted, StateEliminated},
	}
	for _, tr := range allowed {
		if !tr[0].CanTransition(tr[1]) {
			t.Errorf("%s → %s should be allowed", tr[0], tr[1])
		}
	}
	forbidden := [][2]LessonState{
		// No skipping the nutrient test.
		{StateCandidate, StateAccepted},
		// Quarantine is left only through trial.
		{StateQuarantined, StateAccepted},
		// Elimination is final.
		{StateEliminated, StateCandidate},
		{StateEliminated, StateAccepted},
		// A superseded lesson earns acceptance again through trial.
		{StateSuperseded, StateAccepted},
		{StateCandidate, StateSuperseded},
	}
	for _, tr := range forbidden {
		if tr[0].CanTransition(tr[1]) {
			t.Errorf("%s → %s should be forbidden", tr[0], tr[1])
		}
	}
	for _, s := range []LessonState{StateCandidate, StateQuarantined, StateSuperseded, StateEliminated} {
		if s.Injectable() {
			t.Errorf("%s must not be injectable", s)
		}
	}
}

func TestLifecycleEventValidate(t *testing.T) {
	ev := LifecycleEvent{ID: NewID(t0), LessonID: NewID(t0), At: t0, To: StateCandidate, Reason: "reflected"}
	if err := ev.Validate(); err != nil {
		t.Fatalf("creation event rejected: %v", err)
	}
	ev.From, ev.To = StateCandidate, StateAccepted
	if ev.Validate() == nil {
		t.Fatal("a skip past the nutrient test was accepted")
	}
	ev.From, ev.To, ev.Reason = StateCandidate, StateTrial, ""
	if ev.Validate() == nil {
		t.Fatal("a state change without a reason was accepted")
	}
}

func TestCreditMean(t *testing.T) {
	if got := (Credit{}).Mean(); got != 0.5 {
		t.Fatalf("uninformed credit mean = %v, want 0.5", got)
	}
	if got := (Credit{Successes: 8, Failures: 0}).Mean(); got != 0.9 {
		t.Fatalf("mean = %v, want 0.9", got)
	}
}
