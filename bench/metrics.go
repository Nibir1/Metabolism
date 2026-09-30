// Package bench measures whether a host is getting better. Phase 0 exists so
// that every later claim of learning is judged against a baseline measured the
// same way (docs/roadmap.md; docs/decisions.md, D-009).
//
// The metrics are defined on the host-agnostic Episode and Outcome records,
// so any host is measured identically. docs/helix-integration.md lists which
// Helix signal feeds each field.
package bench

import (
	"context"
	"fmt"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// CharsPerToken is the estimate used to turn characters into tokens. It is
// the same four-characters-per-token figure Helix labels as an estimate in
// /cost, and results that use it say "est".
const CharsPerToken = 4

// Dataset is the episodes and outcomes a measurement is computed from.
type Dataset struct {
	Episodes []types.Episode
	// Outcomes by episode, in journal order.
	Outcomes map[types.ID][]types.Outcome
}

// Load reads every episode and outcome from a journal.
func Load(ctx context.Context, j ports.Journal) (Dataset, error) {
	ds := Dataset{Outcomes: map[types.ID][]types.Outcome{}}
	q := types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode, types.RecordOutcome}}
	for r, err := range j.Scan(ctx, q) {
		if err != nil {
			return Dataset{}, fmt.Errorf("load: %w", err)
		}
		switch r.Kind {
		case types.RecordEpisode:
			ds.Episodes = append(ds.Episodes, *r.Episode)
		case types.RecordOutcome:
			ds.Outcomes[r.Outcome.EpisodeID] = append(ds.Outcomes[r.Outcome.EpisodeID], *r.Outcome)
		}
	}
	return ds, nil
}

// Metrics summarises a set of episodes. Rates are fractions in [0,1]. A rate
// whose denominator is zero is reported as 0, and the denominators are
// included so a reader can tell "none" from "no data".
type Metrics struct {
	Episodes int `json:"episodes"`
	Steps    int `json:"steps"`
	// Finished counts episodes that ended done and collected no negative
	// outcome. It is the "good outcome" every cost metric is divided by.
	Finished int `json:"finished"`

	FinishRate float64 `json:"finish_rate"`
	// OutcomeCoverage is the share of episodes with at least one outcome
	// label. Learning needs labels; Phase 1 requires this ≥ 90% for a host.
	OutcomeCoverage float64 `json:"outcome_coverage"`
	// Cost of all episodes, successful or not, per finished one: failures
	// are paid for too.
	ModelCallsPerFinished float64 `json:"model_calls_per_finished"`
	EstTokensPerFinished  float64 `json:"est_tokens_per_finished"`

	BudgetExhaustionRate float64 `json:"budget_exhaustion_rate"`
	FailedStepRate       float64 `json:"failed_step_rate"` // failed steps / all steps
	UndoRate             float64 `json:"undo_rate"`
	DeclineRate          float64 `json:"decline_rate"`
	RepeatRate           float64 `json:"repeat_rate"`
}

// Finished reports whether an episode counts as a good outcome
// (types.Succeeded).
func Finished(ep types.Episode, outs []types.Outcome) bool { return types.Succeeded(ep, outs) }

// Compute measures eps using the outcomes in ds.
func Compute(eps []types.Episode, outcomes map[types.ID][]types.Outcome) Metrics {
	var (
		m                          Metrics
		calls, chars               int
		exhausted, failedSteps     int
		undone, declined, repeated int
		labelled                   int
	)
	for _, ep := range eps {
		outs := outcomes[ep.ID]
		m.Episodes++
		m.Steps += len(ep.Steps)
		failedSteps += ep.FailedSteps()
		calls += ep.Usage.ModelCalls
		chars += ep.Usage.InputChars + ep.Usage.OutputChars
		if ep.End == types.RunBudgetExhausted {
			exhausted++
		}
		if len(outs) > 0 {
			labelled++
		}
		if Finished(ep, outs) {
			m.Finished++
		}
		if has(outs, types.OutcomeUndone) {
			undone++
		}
		if has(outs, types.OutcomeDeclined) {
			declined++
		}
		if has(outs, types.OutcomeRepeated) {
			repeated++
		}
	}
	m.FinishRate = ratio(m.Finished, m.Episodes)
	m.OutcomeCoverage = ratio(labelled, m.Episodes)
	m.ModelCallsPerFinished = ratio(calls, m.Finished)
	m.EstTokensPerFinished = ratio(chars, m.Finished) / CharsPerToken
	m.BudgetExhaustionRate = ratio(exhausted, m.Episodes)
	m.FailedStepRate = ratio(failedSteps, m.Steps)
	m.UndoRate = ratio(undone, m.Episodes)
	m.DeclineRate = ratio(declined, m.Episodes)
	m.RepeatRate = ratio(repeated, m.Episodes)
	return m
}

func has(outs []types.Outcome, k types.OutcomeKind) bool {
	for _, o := range outs {
		if o.Kind == k {
			return true
		}
	}
	return false
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
