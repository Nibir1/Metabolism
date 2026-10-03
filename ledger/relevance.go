package ledger

import (
	"context"
	"fmt"
	"time"

	"github.com/Nibir1/metabolism/episodic"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Relevant says whether a turn bears on a lesson. Only relevant turns are
// credited to it; nil counts every turn.
type Relevant func(lesson types.ID, ep types.Episode) bool

// Relevance decides which turns bear on which lessons (D-026).
//
// The coin flip is on every turn a lesson is in scope for, but most of those
// turns are about something else: a lesson about empty searches has nothing
// to do with a commit. Counting them all dilutes the turns that matter, and
// a lesson that stopped helping on its ~10% of turns looks neutral over all
// of them. So the ledger counts a turn for a lesson only when the turn's
// request resembles what the lesson is about.
//
// What it is about is its neighbourhood: its source episodes plus the
// episodes episodic memory finds for its text and for each source's request,
// as the nutrient test finds its replays (verify.relevant). A turn bears on
// it when most of the turn's nearest past requests are in that
// neighbourhood. Both are taken from the episodes before the lesson's era,
// so they do not move while the era lasts.
//
// The comparison stays fair: relevance depends only on requests, which exist
// before the coin is flipped, so it removes the same kind of turns from both
// arms. A lesson whose neighbourhood is empty (no sources and no similar
// past request) is credited on every turn, and so is a turn whose request
// has no words: there is nothing to filter by.
type Relevance struct {
	ix    *episodic.Index
	seq   int64
	hoods map[eraKey]map[types.ID]bool
	memo  map[turnKey]bool
}

type eraKey struct {
	lesson types.ID
	era    time.Time
}

type turnKey struct {
	eraKey
	text string
}

// Relevance's retrieval sizes: a neighbourhood as large as the nutrient
// test's relevant set, and a turn's nearest few requests.
const (
	hoodSize  = 16
	neighbors = 5
)

// NewRelevance returns an empty Relevance. Keep one across settles: it reads
// only what the journal gained since the last call.
func NewRelevance() *Relevance {
	// No fading with age: which requests resemble each other should not
	// depend on when the ledger runs.
	return &Relevance{
		ix:    episodic.New(episodic.Options{HalfLife: 1 << 62}),
		hoods: map[eraKey]map[types.ID]bool{},
		memo:  map[turnKey]bool{},
	}
}

// Update reads what j gained and returns the relevance of the turns to m's
// trial and accepted lessons in their eras (Eras).
func (r *Relevance) Update(ctx context.Context, j ports.Journal, m *metabolome.Metabolome, eras map[types.ID]time.Time) (Relevant, error) {
	// Requests only: a turn's steps depend on what was delivered into it,
	// and relevance must not depend on the coin.
	for rec, err := range j.Scan(ctx, types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode}, AfterSeq: r.seq}) {
		if err != nil {
			return nil, fmt.Errorf("ledger: relevance: %w", err)
		}
		ep := *rec.Episode
		ep.Steps = nil
		r.ix.Add(ep)
		r.seq = rec.Seq
	}
	lessons := map[types.ID]types.Lesson{}
	for _, l := range m.Lessons(types.StateTrial, types.StateAccepted) {
		lessons[l.ID] = l
	}
	return func(id types.ID, ep types.Episode) bool {
		l, ok := lessons[id]
		if !ok {
			return true
		}
		return r.relevant(l, eras[id], ep)
	}, nil
}

func (r *Relevance) relevant(l types.Lesson, era time.Time, ep types.Episode) bool {
	k := turnKey{eraKey{l.ID, era}, ep.Request.Text}
	if v, ok := r.memo[k]; ok {
		return v
	}
	// With nothing to compare (no neighbourhood, or a request with no
	// words), there is nothing to filter by: the turn counts.
	hood := r.hood(l, era)
	v := len(hood) == 0 || len(episodic.Tokenize(ep.Request.Text)) == 0
	if !v {
		near := r.ix.Retrieve(episodic.Query{Text: ep.Request.Text, K: neighbors, AsOf: era})
		in := 0
		for _, c := range near {
			if hood[c.Episode.ID] {
				in++
			}
		}
		v = 2*in > len(near)
	}
	r.memo[k] = v
	return v
}

// hood is the lesson's neighbourhood among the episodes before era.
func (r *Relevance) hood(l types.Lesson, era time.Time) map[types.ID]bool {
	k := eraKey{l.ID, era}
	if h, ok := r.hoods[k]; ok {
		return h
	}
	h := map[types.ID]bool{}
	queries := []string{l.Text}
	for _, id := range l.Sources {
		if ep, ok := r.ix.Get(id); ok && (era.IsZero() || ep.StartedAt.Before(era)) {
			h[id] = true
			queries = append(queries, ep.Request.Text)
		}
	}
	for _, q := range queries {
		for _, c := range r.ix.Retrieve(episodic.Query{Text: q, Scope: l.Scope, K: hoodSize, AsOf: era}) {
			h[c.Episode.ID] = true
		}
	}
	r.hoods[k] = h
	return h
}
