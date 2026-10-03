package bench

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// PairRow is one of helixsim.PairLessons in one history: a correct lesson
// (older) and its paraphrase or rival (newer), and where each ended.
type PairRow struct {
	Seed     uint64            `json:"seed"`
	Conflict bool              `json:"rival"`
	Older    string            `json:"older"`
	Newer    string            `json:"newer"`
	OlderEnd types.LessonState `json:"older_final"`
	NewerEnd types.LessonState `json:"newer_final"`
	// BothInForce: neither was set aside; for rivals, every turn both are
	// delivered on fails.
	BothInForce bool `json:"both_in_force"`
	// Superseded says which was set aside and why.
	Superseded string `json:"superseded,omitempty"`
	At         int    `json:"superseded_at,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// PairsResult is the contradiction and supersession check on the simulator
// (Phase 4c, D-027). Every planted lesson is good alone.
type PairsResult struct {
	Seeds int  `json:"seeds"`
	Turns int  `json:"turns"`
	Pairs bool `json:"pair_decisions"`

	Rivals            int `json:"rivals"`
	RivalsBothInForce int `json:"rivals_both_in_force"`
	Paraphrases       int `json:"paraphrases"`
	ParaphrasesBoth   int `json:"paraphrases_both_in_force"`
	// GoodEliminated counts lessons, every one good alone, that ended
	// eliminated: the permanent mistake.
	GoodEliminated int `json:"good_eliminated"`
	// Lessons in no pair that ended superseded: wrong.
	Unpaired           int `json:"unpaired"`
	UnpairedSuperseded int `json:"unpaired_superseded"`
	// In the second half of each history: turns that failed because two
	// rivals were delivered together, and turns that spent two budget places
	// on two lessons that say the same thing.
	ContradictedTurns int       `json:"contradicted_turns_second_half"`
	DoubledTurns      int       `json:"doubled_turns_second_half"`
	Rows              []PairRow `json:"rows"`
}

// EvaluatePairs plants every correct lesson and every paraphrase and rival
// in trial, plays turns as EvaluateCredit does, and settles every 100 turns
// with rule. With rule.WithoutPairs it shows what the ledger does without
// pair decisions.
func EvaluatePairs(ctx context.Context, seeds []uint64, turns int, rule ledger.Rule) (PairsResult, error) {
	rule = rule.Defaults()
	res := PairsResult{Seeds: len(seeds), Turns: turns, Pairs: !rule.WithoutPairs}
	dir, err := os.MkdirTemp("", "metabolism-pairs-")
	if err != nil {
		return res, err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	for _, seed := range seeds {
		if err := pairsSeed(ctx, filepath.Join(dir, fmt.Sprintf("seed-%d.db", seed)), seed, turns, rule, &res); err != nil {
			return res, err
		}
	}
	for _, r := range res.Rows {
		if r.Conflict {
			res.Rivals++
			if r.BothInForce {
				res.RivalsBothInForce++
			}
		} else {
			res.Paraphrases++
			if r.BothInForce {
				res.ParaphrasesBoth++
			}
		}
	}
	return res, nil
}

func pairsSeed(ctx context.Context, path string, seed uint64, turns int, rule ledger.Rule, res *PairsResult) error {
	j, err := sqlite.Open(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	start := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	span := 56 * 24 * time.Hour
	hist, histOuts := helixsim.Generate(helixsim.Config{Seed: seed ^ 0x9a125, Count: 200,
		Start: start.Add(-14 * 24 * time.Hour), Span: 13 * 24 * time.Hour})
	for _, e := range hist {
		if err := j.AppendEpisode(ctx, e); err != nil {
			return err
		}
	}
	for _, o := range histOuts {
		if err := j.AppendOutcome(ctx, o); err != nil {
			return err
		}
	}
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	byText := map[string]types.ID{}
	plant := func(i int, s helixsim.LessonSpec) error {
		// One minute apart, so older and newer are what the plant order says.
		m.SetClock(func() time.Time { return start.Add(time.Duration(i-30) * time.Minute) })
		var src []types.ID
		for _, e := range hist {
			if e.Attrs["scenario"] == s.From && len(src) < 3 {
				src = append(src, e.ID)
			}
		}
		l, err := m.Create(ctx, types.Lesson{ID: plantedID(seed, 200+i), Kind: s.Kind, Scope: types.Scope{Level: types.ScopeGlobal},
			Text: s.Text, Sources: src, Provenance: types.ProvUserTyped, Origin: "planted"}, "planted by pairs-eval")
		if err != nil {
			return err
		}
		ev, err := m.RecordEvidence(ctx, types.Evidence{LessonID: l.ID, Kind: types.EvidenceNutrientTest,
			Result: types.ResultPass, Summary: "planted by pairs-eval: the replay test is skipped", Compared: 2, Wins: 2})
		if err != nil {
			return err
		}
		byText[s.Text] = l.ID
		return m.Move(ctx, l.ID, types.StateTrial, "planted by pairs-eval", ev.ID)
	}
	n := 0
	for _, s := range helixsim.CorrectLessons() {
		if err := plant(n, s); err != nil {
			return err
		}
		n++
	}
	var rows []PairRow
	for _, p := range helixsim.PairLessons() {
		if err := plant(n, p.LessonSpec); err != nil {
			return err
		}
		n++
		rows = append(rows, PairRow{Seed: seed, Conflict: p.Conflict, Older: p.With, Newer: p.Text})
	}
	paired := map[types.ID]bool{}
	for _, r := range rows {
		paired[byText[r.Older]], paired[byText[r.Newer]] = true, true
	}
	m.SetClock(func() time.Time { return start })

	eps, outs := helixsim.Generate(helixsim.Config{Seed: seed, Count: turns, Start: start, Span: span})
	by := map[types.ID][]types.Outcome{}
	for _, o := range outs {
		by[o.EpisodeID] = append(by[o.EpisodeID], o)
	}
	turn := 0
	onTurn := func(ep types.Episode, delivered []types.Lesson) {
		turn++
		if turn <= turns/2 {
			return
		}
		if helixsim.Contradicted(ep, delivered) {
			res.ContradictedTurns++
		}
		for _, p := range helixsim.PairLessons() {
			if p.Conflict {
				continue
			}
			has := func(text string) bool {
				return slices.ContainsFunc(delivered, func(l types.Lesson) bool { return l.Text == text })
			}
			if has(p.Text) && has(p.With) {
				res.DoubledTurns++
			}
		}
	}
	onSettle := func(at int, ds []ledger.Decision) {
		for _, d := range ds {
			if d.To != types.StateSuperseded {
				continue
			}
			for i := range rows {
				older, newer := byText[rows[i].Older], byText[rows[i].Newer]
				if rows[i].At == 0 && (d.Lesson.ID == older || d.Lesson.ID == newer) && (d.Other == older || d.Other == newer) {
					rows[i].At, rows[i].Reason = at, d.Reason
					rows[i].Superseded = "older"
					if d.Lesson.ID == newer {
						rows[i].Superseded = "newer"
					}
				}
			}
		}
	}
	r := rand.New(rand.NewPCG(seed, 0x9a1257)) //nolint:gosec // reproducible coin flips
	budget := helixsim.Host{}.Describe().Budget
	if err := playTurns(ctx, j, m, eps, by, 100, rule, budget, r, start, start.Add(span+24*time.Hour), onTurn, onSettle, nil); err != nil {
		return err
	}
	state := func(text string) types.LessonState {
		e, _ := m.Get(byText[text])
		return e.Lesson.State
	}
	for i := range rows {
		rows[i].OlderEnd, rows[i].NewerEnd = state(rows[i].Older), state(rows[i].Newer)
		rows[i].BothInForce = rows[i].OlderEnd.Injectable() && rows[i].NewerEnd.Injectable()
	}
	res.Rows = append(res.Rows, rows...)
	for text, id := range byText {
		if state(text) == types.StateEliminated {
			res.GoodEliminated++
		}
		if paired[id] {
			continue
		}
		res.Unpaired++
		if state(text) == types.StateSuperseded {
			res.UnpairedSuperseded++
		}
	}
	return nil
}
