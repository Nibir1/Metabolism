package judge

import (
	"context"
	"fmt"
	"strings"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Outcome judges replays on what happened before asking a model what it
// thinks (D-024, D-029).
//
// A replay that ran its reads ended in one of two measurable ways: it
// answered within its rounds, or it ran out of them with no answer. That
// difference is a fact, and comparing facts adds none of the noise a model
// adds when it confidently prefers one of two equally good answers.
//
// D-029 narrowed what the model sees. On the automated days, 159 of 180
// decisive verdicts came from the model choosing between two plans that
// ended the same way: 58 of them identical but for the reply's wording,
// 101 stopped at different actions, where a forced choice between two
// reasonable plans is decisive almost every time. The same happens between
// two baselines, so the noise floor rose until no lesson could clear it.
//
//   - Both measurable, different ends: the one that answered wins.
//   - The same end and the same action (both answered, both ran out, or both
//     stopped at the same step), or both measurable: facts first. Fewer
//     failed steps wins. Model calls are not judged: the first day under
//     D-029 found 5 of 6 decisive baseline-against-baseline facts were call
//     counts (6 against 4, 4 against 8), a planner varying with itself. Cost
//     is measured by credit, over many turns.
//   - The same facts: a tie if the two answers say the same thing (SameAnswer),
//     and otherwise the model compares the answers (D-033). D-029 called every
//     such pair a tie, "wording aside"; human labels then showed the wording
//     is the answer: on 30 labelled pairs the judge called 21 ties where the
//     person called 6, and agreed with the person on 9 (κ 0.06). Near-identical
//     answers stay ties, so a planner rewording itself adds no verdicts.
//   - Otherwise (different actions where a replay stops, or ends that cannot
//     be compared): the model grades each plan on its own, "does it do what
//     was asked?", when it can (Grader), and the pair is decisive only when
//     one plan does and the other does not. Two reasonable plans are a tie.
type Outcome struct {
	Model ports.Judge
}

// Grader is a model judge that can grade each plan of a pair on its own,
// instead of choosing between them (D-029).
type Grader interface {
	Grade(ctx context.Context, original types.Episode, outcomes []types.Outcome, with, without types.ReplayResult) (types.Verdict, error)
}

var _ ports.Judge = Outcome{}

// Name implements ports.Judge.
func (o Outcome) Name() string { return "outcome+" + o.Model.Name() }

// measurable reports whether a replay's end is a fact a judge can compare,
// and whether that fact is "answered".
func measurable(r types.ReplayResult) (ok, answered bool) {
	switch r.End {
	case types.ReplayEndAnswered:
		return true, true
	case types.ReplayEndBudget:
		return true, false
	}
	return false, false
}

// Compare implements ports.Judge.
func (o Outcome) Compare(ctx context.Context, original types.Episode, outcomes []types.Outcome, with, without types.ReplayResult) (types.Verdict, error) {
	okW, ansW := measurable(with)
	okWo, ansWo := measurable(without)
	switch {
	case okW && okWo && ansW != ansWo:
		v := types.Verdict{Prefer: types.PreferWith, Confidence: 0.95,
			Reason: fmt.Sprintf("outcome: with the lesson it answered in %d rounds; without, it ran out after %d", with.Rounds, without.Rounds)}
		if ansWo {
			v = types.Verdict{Prefer: types.PreferWithout, Confidence: 0.95,
				Reason: fmt.Sprintf("outcome: without the lesson it answered in %d rounds; with, it ran out after %d", without.Rounds, with.Rounds)}
		}
		return v, nil
	case okW && okWo, with.End == without.End && sameAction(with, without):
		v := facts(with, without)
		if v.Prefer != types.PreferNeither || SameAnswer(with.Text, without.Text) {
			return v, nil
		}
		// The same facts, different answers: which answers better is the
		// model's to say (D-033).
		mv, err := o.Model.Compare(ctx, original, outcomes, with, without)
		if err != nil {
			return types.Verdict{}, err
		}
		mv.Reason = "answers differ, the facts do not: " + mv.Reason
		return mv, nil
	}
	if g, ok := o.Model.(Grader); ok {
		return g.Grade(ctx, original, outcomes, with, without)
	}
	return o.Model.Compare(ctx, original, outcomes, with, without)
}

// facts compares two replays that ended the same way on what can be
// counted: failed steps.
func facts(with, without types.ReplayResult) types.Verdict {
	fw, fwo := failedSteps(with), failedSteps(without)
	switch {
	case fw < fwo:
		return types.Verdict{Prefer: types.PreferWith, Confidence: 0.9,
			Reason: fmt.Sprintf("facts: the same end, %d failed step(s) with the lesson against %d without", fw, fwo)}
	case fw > fwo:
		return types.Verdict{Prefer: types.PreferWithout, Confidence: 0.9,
			Reason: fmt.Sprintf("facts: the same end, %d failed step(s) with the lesson against %d without", fw, fwo)}
	}
	return types.Verdict{Prefer: types.PreferNeither, Confidence: 1,
		Reason: "facts: the same end, the same action, the same failed steps; wording and model calls are not judged"}
}

// SameAnswerAt is how much two answers' words must overlap (Jaccard, on
// lower-cased words) for them to count as the same answer.
const SameAnswerAt = 0.85

// SameAnswer reports whether two replies say the same thing: both empty, or
// sharing at least SameAnswerAt of their words.
func SameAnswer(a, b string) bool {
	wa, wb := words(a), words(b)
	if len(wa) == 0 || len(wb) == 0 {
		return len(wa) == len(wb)
	}
	inter := 0
	for w := range wa {
		if wb[w] {
			inter++
		}
	}
	return float64(inter)/float64(len(wa)+len(wb)-inter) >= SameAnswerAt
}

func words(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '_' && r != '.' && r != '/'
	}) {
		out[w] = true
	}
	return out
}

func failedSteps(r types.ReplayResult) int {
	n := 0
	for _, s := range r.Steps {
		if !s.OK && s.Err != "" {
			n++
		}
	}
	return n
}

// sameAction reports whether two replays end on the same action: their last
// steps match once quoting and spacing are set aside (cd /x and cd "/x" are
// one action), or neither has a step.
func sameAction(a, b types.ReplayResult) bool {
	la, lb := lastStep(a), lastStep(b)
	return la == lb
}

func lastStep(r types.ReplayResult) string {
	if len(r.Steps) == 0 {
		return ""
	}
	s := r.Steps[len(r.Steps)-1]
	subj := strings.NewReplacer(`"`, "", "'", "").Replace(s.Subject)
	return s.Tool + "|" + s.Action + "|" + strings.Join(strings.Fields(subj), " ")
}
