// Package episodic is Metabolism's first tier of memory: past episodes,
// retrieved as cases for a new request (the Memento idea: learn from
// experience by recalling it, not by retraining).
//
// Retrieval is lexical: BM25 over the request text and the subjects of the
// steps taken, boosted for the same project and for recency. That is a
// deliberate baseline. It needs no model, runs locally, and is easy to
// reason about, so whatever later tiers add can be measured against it.
// Embeddings can come later through a port if lexical recall proves to be the
// bottleneck, and bench.EvaluateRecall is what would show that.
//
// The index is derived state: it is rebuilt from the journal and never
// written anywhere, so it survives restarts exactly as well as the journal
// does (rubric C3) and can never disagree with it.
//
// Every query can be asked "as of" a moment in the past. Only episodes that
// started before it, and outcomes known by then, are visible. That is what
// lets the offline evaluation replay history without leaking the future.
package episodic

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// Options tunes retrieval. The zero value selects the defaults.
type Options struct {
	// HalfLife is how quickly old cases fade. They fade only to half weight,
	// never to nothing: a failure from last year is still a failure.
	HalfLife time.Duration
	// ScopeBoost multiplies the score of a case from the same scope.
	ScopeBoost float64
	// SubjectWeight is how much a step subject's words count relative to the
	// request's.
	SubjectWeight float64
}

func (o Options) withDefaults() Options {
	if o.HalfLife <= 0 {
		o.HalfLife = 30 * 24 * time.Hour
	}
	if o.ScopeBoost <= 0 {
		o.ScopeBoost = 1.5
	}
	if o.SubjectWeight <= 0 {
		o.SubjectWeight = 0.5
	}
	return o
}

// BM25 parameters: the usual values.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// Query asks for cases similar to a request.
type Query struct {
	Text  string
	Scope types.Scope
	// K bounds the result (default 5).
	K int
	// AsOf hides episodes that started at or after it, and outcomes that
	// arrived after it. Zero means "everything in the index".
	AsOf time.Time
	// IncludeUntrusted admits episodes whose request has untrusted
	// provenance (an unreliable transcript). Off by default: a guess at what
	// was said is not a case to learn from.
	IncludeUntrusted bool
	// Exclude never returns this episode (a query about an episode itself).
	Exclude types.ID
}

// Case is one retrieved episode, with what was known about how it turned out.
type Case struct {
	Episode   types.Episode   `json:"episode"`
	Outcomes  []types.Outcome `json:"outcomes"`
	Succeeded bool            `json:"succeeded"`
	Score     float64         `json:"score"`
}

type doc struct {
	ep    types.Episode
	outs  []types.Outcome
	tf    map[string]float64
	len   float64
	terms []string // distinct terms, for document frequency
}

// Index holds episodes for retrieval. It is not safe for concurrent use.
type Index struct {
	opts    Options
	docs    []*doc
	byID    map[types.ID]*doc
	df      map[string]int
	lenSum  float64
	lastSeq int64
	pending map[types.ID][]types.Outcome // outcomes seen before their episode
}

// New returns an empty index.
func New(opts Options) *Index {
	return &Index{
		opts:    opts.withDefaults(),
		byID:    map[types.ID]*doc{},
		df:      map[string]int{},
		pending: map[types.ID][]types.Outcome{},
	}
}

// Len is the number of episodes indexed.
func (ix *Index) Len() int { return len(ix.docs) }

// Get returns an indexed episode.
func (ix *Index) Get(id types.ID) (types.Episode, bool) {
	d, ok := ix.byID[id]
	if !ok {
		return types.Episode{}, false
	}
	return d.ep, true
}

// Add indexes an episode. Adding one already present does nothing.
func (ix *Index) Add(ep types.Episode) {
	if _, ok := ix.byID[ep.ID]; ok {
		return
	}
	d := &doc{ep: ep, tf: map[string]float64{}}
	for _, t := range Tokenize(ep.Request.Text) {
		d.tf[t]++
		d.len++
	}
	for _, s := range ep.Steps {
		for _, t := range Tokenize(s.Subject + " " + s.Action) {
			d.tf[t] += ix.opts.SubjectWeight
			d.len += ix.opts.SubjectWeight
		}
	}
	for t := range d.tf {
		d.terms = append(d.terms, t)
		ix.df[t]++
	}
	d.outs = append(d.outs, ix.pending[ep.ID]...)
	delete(ix.pending, ep.ID)
	ix.docs = append(ix.docs, d)
	ix.byID[ep.ID] = d
	ix.lenSum += d.len
}

// AddOutcome attaches an outcome to its episode. An outcome that arrives
// before its episode is held until the episode is added.
func (ix *Index) AddOutcome(o types.Outcome) {
	if d, ok := ix.byID[o.EpisodeID]; ok {
		d.outs = append(d.outs, o)
		return
	}
	ix.pending[o.EpisodeID] = append(ix.pending[o.EpisodeID], o)
}

// Refresh adds everything the journal gained since the last Refresh.
// Returns how many records were read.
func (ix *Index) Refresh(ctx context.Context, j ports.Journal) (int, error) {
	n := 0
	q := types.JournalQuery{
		Kinds:    []types.RecordKind{types.RecordEpisode, types.RecordOutcome},
		AfterSeq: ix.lastSeq,
	}
	for r, err := range j.Scan(ctx, q) {
		if err != nil {
			return n, err
		}
		switch r.Kind {
		case types.RecordEpisode:
			ix.Add(*r.Episode)
		case types.RecordOutcome:
			ix.AddOutcome(*r.Outcome)
		}
		ix.lastSeq = r.Seq
		n++
	}
	return n, nil
}

// Retrieve returns up to q.K cases for q, best first. Cases with no word in
// common with the request are never returned: similarity by scope or
// recency alone is not similarity.
func (ix *Index) Retrieve(q Query) []Case {
	if q.K <= 0 {
		q.K = 5
	}
	terms := Tokenize(q.Text)
	if len(terms) == 0 || len(ix.docs) == 0 {
		return nil
	}
	now := q.AsOf
	if now.IsZero() {
		now = time.Now()
	}
	n := float64(len(ix.docs))
	avgLen := ix.lenSum / n
	if avgLen == 0 {
		avgLen = 1
	}

	var cases []Case
	for _, d := range ix.docs {
		if d.ep.ID == q.Exclude {
			continue
		}
		if !q.AsOf.IsZero() && !d.ep.StartedAt.Before(q.AsOf) {
			continue
		}
		if !q.IncludeUntrusted && !d.ep.Request.Provenance.Trusted() {
			continue
		}
		s := 0.0
		for _, t := range terms {
			f := d.tf[t]
			if f == 0 {
				continue
			}
			idf := math.Log(1 + (n-float64(ix.df[t])+0.5)/(float64(ix.df[t])+0.5))
			s += idf * f * (bm25K1 + 1) / (f + bm25K1*(1-bm25B+bm25B*d.len/avgLen))
		}
		if s <= 0 {
			continue
		}
		if q.Scope.Level != "" && d.ep.Scope == q.Scope {
			s *= ix.opts.ScopeBoost
		}
		age := now.Sub(d.ep.StartedAt)
		if age < 0 {
			age = 0
		}
		s *= 0.5 + 0.5*math.Exp2(-float64(age)/float64(ix.opts.HalfLife))

		outs := d.outs
		if !q.AsOf.IsZero() {
			outs = nil
			for _, o := range d.outs {
				if !o.At.After(q.AsOf) {
					outs = append(outs, o)
				}
			}
		}
		cases = append(cases, Case{Episode: d.ep, Outcomes: outs, Succeeded: types.Succeeded(d.ep, outs), Score: s})
	}
	slices.SortStableFunc(cases, func(a, b Case) int {
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		}
		return b.Episode.StartedAt.Compare(a.Episode.StartedAt)
	})
	if len(cases) > q.K {
		cases = cases[:q.K]
	}
	return cases
}

// stopwords are words too common in requests to say anything about them.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "to": true, "of": true,
	"in": true, "on": true, "for": true, "is": true, "it": true, "this": true, "that": true,
	"my": true, "me": true, "i": true, "you": true, "please": true, "can": true, "with": true,
	"what": true, "does": true, "do": true, "be": true, "are": true, "at": true, "from": true,
}

// Tokenize splits text into lowercase words and path parts, dropping
// stopwords and single characters.
func Tokenize(s string) []string {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
	out := fields[:0]
	for _, f := range fields {
		if len(f) > 1 && !stopwords[f] {
			out = append(out, f)
		}
	}
	return out
}
