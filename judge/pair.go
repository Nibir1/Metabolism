package judge

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/Nibir1/metabolism/types"
)

// Pair is a with/without comparison as a judge sees it: the request, how the
// original attempt went, and two plans labelled A and B.
//
// Which plan is A is decided per episode by a hash, not fixed: a model judge
// that leans toward whichever option comes first would otherwise look like it
// prefers lessons. The mapping back is WithIsA.
type Pair struct {
	Request  string   `json:"request"`
	Original string   `json:"original_attempt"`
	PlanA    []string `json:"plan_a"`
	PlanB    []string `json:"plan_b"`
	WithIsA  bool     `json:"-"`
}

// NewPair builds the judge's view of one comparison.
func NewPair(original types.Episode, outcomes []types.Outcome, with, without types.ReplayResult) Pair {
	h := fnv.New32a()
	_, _ = h.Write([]byte(original.ID))
	p := Pair{Request: original.Request.Text, Original: describeOriginal(original, outcomes), WithIsA: h.Sum32()%2 == 0}
	a, b := planLines(with), planLines(without)
	if !p.WithIsA {
		a, b = b, a
	}
	p.PlanA, p.PlanB = a, b
	return p
}

// Verdict maps "a", "b" or "same" back to a preference about the lesson.
func (p Pair) Verdict(better string, confidence float64, reason string) types.Verdict {
	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}
	prefer := types.PreferNeither
	switch strings.ToLower(strings.TrimSpace(better)) {
	case "a":
		prefer = types.PreferWithout
		if p.WithIsA {
			prefer = types.PreferWith
		}
	case "b":
		prefer = types.PreferWith
		if p.WithIsA {
			prefer = types.PreferWithout
		}
	}
	return types.Verdict{Prefer: prefer, Confidence: confidence, Reason: reason}
}

// describeOriginal says in plain words how the original attempt went. A
// judge reads this, so it must not be jargon: "ended budget-exhausted" told
// the first live judge nothing, and it rewarded eight searches that never
// answered the user over one that did.
func describeOriginal(ep types.Episode, outcomes []types.Outcome) string {
	var parts []string
	switch ep.End {
	case types.RunDone:
		parts = append(parts, "it finished")
	case types.RunFailed:
		parts = append(parts, "it stopped on an error")
	case types.RunBudgetExhausted:
		parts = append(parts, "it ran out of its step budget before finishing")
	case types.RunOpenWork:
		parts = append(parts, "it stopped with the work not done")
	}
	for _, o := range outcomes {
		switch o.Kind {
		case types.OutcomeUndone:
			parts = append(parts, "the user then undid what it did")
		case types.OutcomeDeclined:
			parts = append(parts, "the user declined one of its steps")
		case types.OutcomeRepeated:
			parts = append(parts, "the user asked the same thing again soon after, so it did not land")
		}
	}
	var failed []string
	for _, s := range ep.Steps {
		if !s.OK {
			failed = append(failed, strings.TrimSpace(s.Tool+" "+s.Subject)+": "+s.Err)
		}
	}
	if len(failed) > 0 {
		parts = append(parts, "failed steps: "+strings.Join(failed, "; "))
	}
	return strings.Join(parts, "; ")
}

// planLines lists a plan's steps and what it says to the user. A plan that
// says nothing is marked as such: a reader cannot notice an absence.
//
// A multi-round replay (D-021) also says which round planned each step, what
// each read that ran found, and how the replay ended, in plain words: a judge
// must be able to see a plan that kept rereading without ever answering.
func planLines(r types.ReplayResult) []string {
	multi := r.Rounds > 1
	var out []string
	for _, s := range r.Steps {
		line := strings.TrimSpace(s.Tool + " " + s.Action)
		if s.Subject != "" {
			line += ": " + s.Subject
		}
		switch {
		case s.Ran && s.OK:
			line += " (ran)"
		case !s.OK && s.Err != "":
			line += " (fails: " + s.Err + ")"
		}
		if multi && s.Round > 0 {
			line = fmt.Sprintf("round %d: %s", s.Round, line)
		}
		out = append(out, line)
	}
	if r.Text != "" {
		out = append(out, "reply to the user: "+r.Text)
	} else {
		out = append(out, "reply to the user: (none)")
	}
	switch r.End {
	case types.ReplayEndBudget:
		out = append(out, fmt.Sprintf("ended: ran out of follow-up rounds after %d rounds, still reading", r.Rounds))
	case types.ReplayEndUnexecuted:
		out = append(out, "ended: at a step that would change something; such steps are planned here, not run, and are not failures")
	}
	return out
}
