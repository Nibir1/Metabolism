package bench

import (
	"fmt"
	"slices"
	"sort"

	"github.com/Nibir1/metabolism/types"
)

// Savability is the second half of Phase 5a's stop condition (D-031): could
// the "answer now or read more" decision be predicted well enough to save at
// least half of what answer-only follow-up rounds cost?
//
// An answer-only round is a follow-up planner round whose plan only replied:
// the full planner prompt (rules, tools, context: about 17k characters) was
// sent only to write an answer from what earlier rounds read. A habit would
// instead send a small "answer from what you read" prompt: the request and
// the execution report. It saves tokens, not calls.
//
// The predictor here is deliberately simple and free: a table of how often
// the next round only answered, keyed by what is known before it (what the
// previous round did, whether all of it worked, which round this is). It is
// learned on the earlier part of the journal and tested on the later part, as
// a habit would be used. It fires only where its training precision is at
// least MinPrecision. A false alarm is a lookup cut short: the turn answers
// before it has read what it needed.
type Savability struct {
	Rounds      int     `json:"follow_up_rounds"` // follow-up rounds in the test part
	AnswerOnly  int     `json:"answer_only"`      // of which only answered
	TrainRounds int     `json:"train_rounds"`     // follow-up rounds learned from
	Fired       int     `json:"fired"`            // test rounds the predictor said "answer now"
	Hits        int     `json:"hits"`             // fired on an answer-only round
	FalseAlarms int     `json:"false_alarms"`     // fired on a round that read more
	Precision   float64 `json:"precision"`
	Recall      float64 `json:"recall"`

	// Token side, on sized rounds only (recorded with per-round sizes).
	FullPrompt   int     `json:"full_prompt_chars"` // median first-round input: the fixed full prompt
	AnswerChars  int     `json:"answer_only_chars"` // input of the test part's sized answer-only rounds
	SavedChars   int     `json:"saved_chars"`       // what the hits would have saved
	SavedShare   float64 `json:"saved_share"`       // SavedChars / AnswerChars
	SizedAnswers int     `json:"sized_answer_rounds"`

	Rules  []SaveRule `json:"rules"` // the keys the predictor fires on
	Passes bool       `json:"passes"`
	Why    string     `json:"why"`
}

// SaveRule is one key the predictor fires on, with its training counts.
type SaveRule struct {
	Key        string  `json:"key"`
	Seen       int     `json:"seen"`
	AnswerOnly int     `json:"answer_only"`
	Precision  float64 `json:"precision"`
}

// Thresholds of the savability test (D-031: half of its cost savable, and
// lookups rarely cut short).
const (
	MinPrecision = 0.9
	MinSaved     = 0.5
	minSeen      = 5 // a key needs this many training rounds to fire
)

type followUp struct {
	key        string
	answerOnly bool
	chars      int
	at         int // order in the journal
}

// MeasureSavability runs the test on eps in time order, learning on the
// first trainShare of follow-up rounds.
func MeasureSavability(eps []types.Episode, trainShare float64) Savability {
	var s Savability
	slices.SortStableFunc(eps, func(a, b types.Episode) int { return a.StartedAt.Compare(b.StartedAt) })
	var rounds []followUp
	var firstRound []int
	for _, ep := range eps {
		if len(ep.Plans) == 0 {
			continue
		}
		byRound := map[int][]types.Step{}
		for _, st := range ep.Steps {
			byRound[st.Round] = append(byRound[st.Round], st)
		}
		for _, p := range ep.Plans {
			if p.Round <= 1 {
				if p.InputChars > 0 {
					firstRound = append(firstRound, p.InputChars)
				}
				continue
			}
			rounds = append(rounds, followUp{
				key:        roundKey(byRound[p.Round-1], p.Round),
				answerOnly: p.Answered,
				chars:      p.InputChars,
				at:         len(rounds),
			})
		}
	}
	if len(rounds) == 0 {
		s.Why = "no follow-up rounds with decision records yet"
		return s
	}
	cut := int(float64(len(rounds)) * trainShare)
	train, test := rounds[:cut], rounds[cut:]
	s.TrainRounds = len(train)

	type count struct{ seen, answer int }
	table := map[string]*count{}
	for _, r := range train {
		c := table[r.key]
		if c == nil {
			c = &count{}
			table[r.key] = c
		}
		c.seen++
		if r.answerOnly {
			c.answer++
		}
	}
	fires := map[string]bool{}
	for k, c := range table {
		if c.seen >= minSeen && float64(c.answer)/float64(c.seen) >= MinPrecision {
			fires[k] = true
			s.Rules = append(s.Rules, SaveRule{Key: k, Seen: c.seen, AnswerOnly: c.answer, Precision: float64(c.answer) / float64(c.seen)})
		}
	}
	sort.Slice(s.Rules, func(i, j int) bool { return s.Rules[i].Seen > s.Rules[j].Seen })

	if len(firstRound) > 0 {
		slices.Sort(firstRound)
		s.FullPrompt = firstRound[len(firstRound)/2]
	}
	for _, r := range test {
		s.Rounds++
		if r.answerOnly {
			s.AnswerOnly++
			if r.chars > 0 {
				s.SizedAnswers++
				s.AnswerChars += r.chars
			}
		}
		if !fires[r.key] {
			continue
		}
		s.Fired++
		if r.answerOnly {
			s.Hits++
			if r.chars > 0 && s.FullPrompt > 0 {
				s.SavedChars += min(s.FullPrompt, r.chars)
			}
		} else {
			s.FalseAlarms++
		}
	}
	if s.Fired > 0 {
		s.Precision = float64(s.Hits) / float64(s.Fired)
	}
	if s.AnswerOnly > 0 {
		s.Recall = float64(s.Hits) / float64(s.AnswerOnly)
	}
	if s.AnswerChars > 0 {
		s.SavedShare = float64(s.SavedChars) / float64(s.AnswerChars)
	}
	switch {
	case s.SizedAnswers == 0:
		s.Why = "no sized answer-only rounds in the test part yet (sizes are recorded from day 3)"
	case s.Fired == 0:
		s.Why = fmt.Sprintf("no situation predicts an answer-only round with ≥ %.0f%% precision", 100*MinPrecision)
	case s.Precision < MinPrecision:
		s.Why = fmt.Sprintf("precision %.0f%% on the test part: too many lookups would be cut short", 100*s.Precision)
	case s.SavedShare < MinSaved:
		s.Why = fmt.Sprintf("saves %.0f%% of answer-only input, under the %.0f%% bar", 100*s.SavedShare, 100*MinSaved)
	default:
		s.Passes = true
		s.Why = fmt.Sprintf("saves %.0f%% of answer-only input at %.0f%% precision", 100*s.SavedShare, 100*s.Precision)
	}
	return s
}

// roundKey is what is known before a follow-up round: what the previous
// round did (its first step's tool and action), whether every step worked,
// and which round this is (2, 3, or 4 and later).
func roundKey(prev []types.Step, round int) string {
	what := "nothing"
	if len(prev) > 0 {
		what = prev[0].Tool
		if prev[0].Action != "" {
			what += " " + prev[0].Action
		}
	}
	ok := "all ok"
	for _, st := range prev {
		if !st.OK {
			ok = "a failure"
			break
		}
	}
	r := fmt.Sprint(round)
	if round >= 4 {
		r = "4+"
	}
	return fmt.Sprintf("after %s (%s), round %s", what, ok, r)
}
