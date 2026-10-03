package bench

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// DriftTags are the lessons the drift evaluation makes stop holding: the
// empty-search lesson and the exact-edit lesson. The others are controls.
var DriftTags = []string{"(empty-glob)", "(file-edit)"}

// DriftRow is one lesson in one history.
type DriftRow struct {
	Seed    uint64            `json:"seed"`
	Text    string            `json:"text"`
	Drifted bool              `json:"drifted"`
	AtDrift types.LessonState `json:"at_drift"` // its state when the world changed
	Final   types.LessonState `json:"final"`
	// After is how many turns after the change it was retired (0: not
	// retired). Reason is the retirement's reason.
	After  int    `json:"retired_after,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// DriftResult is the drift evaluation (Phase 4b, D-026).
type DriftResult struct {
	Seeds  int `json:"seeds"`
	Before int `json:"turns_before"`
	After  int `json:"turns_after"`

	// Drifted lessons that were accepted when the world changed, and how
	// many of them were retired, and how fast.
	DriftedAccepted int   `json:"drifted_accepted"`
	DriftedRetired  int   `json:"drifted_retired"`
	Latencies       []int `json:"latencies"` // turns after the change
	// Lessons that did not drift but were retired after the change: the
	// false retirements a sliding window risks.
	ControlsAccepted int        `json:"controls_accepted"`
	FalseRetirements int        `json:"false_retirements"`
	Rows             []DriftRow `json:"rows"`
}

// EvaluateDrift plays before turns with the simulator's correct lessons in
// trial (they earn acceptance), changes the world so the DriftTags lessons
// stop holding, then plays after turns and records which lessons are
// retired, and when.
func EvaluateDrift(ctx context.Context, seeds []uint64, before, after int) (DriftResult, error) {
	res := DriftResult{Seeds: len(seeds), Before: before, After: after}
	dir, err := os.MkdirTemp("", "metabolism-drift-")
	if err != nil {
		return res, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for _, seed := range seeds {
		rows, err := driftSeed(ctx, filepath.Join(dir, fmt.Sprintf("seed-%d.db", seed)), seed, before, after)
		if err != nil {
			return res, err
		}
		res.Rows = append(res.Rows, rows...)
	}
	for _, r := range res.Rows {
		if r.AtDrift != types.StateAccepted {
			continue
		}
		if r.Drifted {
			res.DriftedAccepted++
			if r.After > 0 {
				res.DriftedRetired++
				res.Latencies = append(res.Latencies, r.After)
			}
		} else {
			res.ControlsAccepted++
			if r.After > 0 {
				res.FalseRetirements++
			}
		}
	}
	slices.Sort(res.Latencies)
	return res, nil
}

func driftSeed(ctx context.Context, path string, seed uint64, before, after int) ([]DriftRow, error) {
	j, err := sqlite.Open(ctx, path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = j.Close() }()
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	span1, span2 := 42*24*time.Hour, 42*24*time.Hour
	hist, histOuts := helixsim.Generate(helixsim.Config{Seed: seed ^ 0xd41f7, Count: 200,
		Start: start.Add(-14 * 24 * time.Hour), Span: 13 * 24 * time.Hour})
	for _, e := range hist {
		if err := j.AppendEpisode(ctx, e); err != nil {
			return nil, err
		}
	}
	for _, o := range histOuts {
		if err := j.AppendOutcome(ctx, o); err != nil {
			return nil, err
		}
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return nil, err
	}
	m.SetClock(func() time.Time { return start })
	var rows []DriftRow
	var ids []types.ID
	for i, s := range helixsim.CorrectLessons() {
		var src []types.ID
		for _, e := range hist {
			if e.Attrs["scenario"] == s.From && len(src) < 3 {
				src = append(src, e.ID)
			}
		}
		l, err := m.Create(ctx, types.Lesson{ID: plantedID(seed, 100+i), Kind: s.Kind, Scope: types.Scope{Level: types.ScopeGlobal},
			Text: s.Text, Sources: src, Provenance: types.ProvUserTyped, Origin: "planted"}, "planted by drift-eval")
		if err != nil {
			return nil, err
		}
		ev, err := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
			Result: types.ResultPass, Summary: "planted by drift-eval: the replay test is skipped", Compared: 2, Wins: 2})
		if err != nil {
			return nil, err
		}
		if err := m.Move(ctx, l.ID, types.StateTrial, "planted by drift-eval", ev.ID); err != nil {
			return nil, err
		}
		drifted := false
		for _, tag := range DriftTags {
			drifted = drifted || strings.Contains(s.Text, tag)
		}
		rows = append(rows, DriftRow{Seed: seed, Text: s.Text, Drifted: drifted})
		ids = append(ids, l.ID)
	}

	rule := ledger.Rule{}.Defaults()
	budget := helixsim.Host{}.Describe().Budget
	r := rand.New(rand.NewPCG(seed, 0xd21f7)) //nolint:gosec // reproducible coin flips
	play := func(seedOffset uint64, count int, from time.Time, span time.Duration, drifts []helixsim.Drift,
		onSettle func(int, []ledger.Decision)) error {
		eps, outs := helixsim.Generate(helixsim.Config{Seed: seed ^ seedOffset, Count: count, Start: from, Span: span})
		by := map[types.ID][]types.Outcome{}
		for _, o := range outs {
			by[o.EpisodeID] = append(by[o.EpisodeID], o)
		}
		return playTurns(ctx, j, m, eps, by, 100, rule, budget, r, from, from.Add(span+2*time.Hour), nil, onSettle, drifts)
	}
	// Before: the lessons earn acceptance.
	if err := play(0x1, before, start, span1, nil, nil); err != nil {
		return nil, err
	}
	for i, id := range ids {
		e, _ := m.Get(id)
		rows[i].AtDrift = e.Lesson.State
	}
	// The world changes; after.
	change := start.Add(span1 + 3*time.Hour)
	var drifts []helixsim.Drift
	for _, tag := range DriftTags {
		drifts = append(drifts, helixsim.Drift{Tag: tag, At: change})
	}
	onSettle := func(turn int, ds []ledger.Decision) {
		for _, d := range ds {
			if d.To != types.StateEliminated {
				continue
			}
			for i, id := range ids {
				if id == d.Lesson.ID && rows[i].After == 0 {
					rows[i].After, rows[i].Reason = turn, d.Reason
				}
			}
		}
	}
	if err := play(0x2, after, change, span2, drifts, onSettle); err != nil {
		return nil, err
	}
	for i, id := range ids {
		e, _ := m.Get(id)
		rows[i].Final = e.Lesson.State
	}
	return rows, nil
}
