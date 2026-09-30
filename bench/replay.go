package bench

import (
	"context"
	"fmt"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Comparison is one episode replayed with and without a lesson set, and the
// judge's verdict on the pair.
type Comparison struct {
	EpisodeID types.ID           `json:"episode_id"`
	With      types.ReplayResult `json:"with"`
	Without   types.ReplayResult `json:"without"`
	Verdict   types.Verdict      `json:"verdict"`
}

// ReplaySummary tallies a replay run. It is the raw material of the nutrient
// test (Phase 2): a lesson is kept only if With wins by a margin and loses
// nowhere that previously succeeded.
type ReplaySummary struct {
	Compared    int          `json:"compared"`
	PreferWith  int          `json:"prefer_with"`
	PreferNone  int          `json:"prefer_neither"`
	PreferOld   int          `json:"prefer_without"`
	Comparisons []Comparison `json:"comparisons"`
}

// Replay runs every episode through host twice, with and without lessons,
// and asks judge which is better. It stops at the first error: a partial
// comparison would be evidence about a different set of episodes than the
// one asked for.
func Replay(ctx context.Context, host ports.Host, judge ports.Judge, ds Dataset,
	eps []types.Episode, lessons []types.Lesson) (ReplaySummary, error) {
	var s ReplaySummary
	for _, ep := range eps {
		without, err := host.Replay(ctx, ep, nil)
		if err != nil {
			return s, fmt.Errorf("replay %s without lessons: %w", ep.ID, err)
		}
		with, err := host.Replay(ctx, ep, lessons)
		if err != nil {
			return s, fmt.Errorf("replay %s with lessons: %w", ep.ID, err)
		}
		v, err := judge.Compare(ctx, ep, ds.Outcomes[ep.ID], with, without)
		if err != nil {
			return s, fmt.Errorf("judge %s: %w", ep.ID, err)
		}
		s.Compared++
		switch v.Prefer {
		case types.PreferWith:
			s.PreferWith++
		case types.PreferWithout:
			s.PreferOld++
		default:
			s.PreferNone++
		}
		s.Comparisons = append(s.Comparisons, Comparison{EpisodeID: ep.ID, With: with, Without: without, Verdict: v})
	}
	return s, nil
}
