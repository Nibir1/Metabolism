// Package parametric is Phase 7, the parametric tier (C7, D-032): stable,
// credited lessons consolidated into a model's weights, and checked for
// forgetting before an adapter is used.
//
// The experiment it supports is narrow on purpose. A small local model is
// run with the promoted lessons as text; the runs that passed become
// training examples whose prompts no longer carry the lessons; a LoRA
// adapter is trained on them. The adapter is then judged against the same
// model with the text lessons, on held-out tasks (gain, and tokens per task)
// and on tasks the base model already solved (retention). If it forgets, it
// is rolled back. Everything trains on this machine (adapters/remote/mlx).
package parametric

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// PromoteRule is which lessons are stable enough for weights: accepted,
// credited on many turns, and not changed for a while (roadmap Phase 7).
type PromoteRule struct {
	MinTurns int           // delivered turns credited (default 50)
	MinAge   time.Duration // since the lesson was accepted (default 7 days)
}

func (r PromoteRule) withDefaults() PromoteRule {
	if r.MinTurns <= 0 {
		r.MinTurns = 50
	}
	if r.MinAge <= 0 {
		r.MinAge = 7 * 24 * time.Hour
	}
	return r
}

// Promote lists the lessons the rule admits, oldest acceptance first.
func Promote(m *metabolome.Metabolome, tallies map[types.ID]*ledger.Tally, now time.Time, rule PromoteRule) []types.Lesson {
	rule = rule.withDefaults()
	type cand struct {
		l  types.Lesson
		at time.Time
	}
	var cs []cand
	for _, l := range m.Lessons(types.StateAccepted) {
		e, _ := m.Get(l.ID)
		var accepted time.Time
		for _, ev := range e.Events {
			if ev.To == types.StateAccepted {
				accepted = ev.At
			}
		}
		t := tallies[l.ID]
		if t == nil || t.Exposed < rule.MinTurns || now.Sub(accepted) < rule.MinAge {
			continue
		}
		cs = append(cs, cand{l, accepted})
	}
	slices.SortFunc(cs, func(a, b cand) int { return a.at.Compare(b.at) })
	out := make([]types.Lesson, len(cs))
	for i, c := range cs {
		out[i] = c.l
	}
	return out
}

// Example is one training example: a prompt without lessons, and an answer
// the host verified (a coding host's tests passed).
type Example struct {
	Task   string `json:"task"`
	System string `json:"system"`
	User   string `json:"user"`
	Answer string `json:"answer"`
}

// HeldOut reports whether a task is in the held-out half: never trained
// on, and where gains are measured. A fixed hash, so every run agrees.
func HeldOut(task string) bool {
	h := fnv.New32a()
	_, _ = h.Write([]byte(task))
	return h.Sum32()%2 == 1
}

// WriteDataset writes examples in the chat format MLX's LoRA trainer reads
// (train.jsonl and valid.jsonl in dir). Held-out tasks are refused: training
// on them would make the gain measurement meaningless. About one example in
// ten goes to validation, and at least one.
func WriteDataset(dir string, examples []Example) (train, valid int, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, 0, err
	}
	var tr, va []Example
	for i, ex := range examples {
		if HeldOut(ex.Task) {
			return 0, 0, fmt.Errorf("example for %s is from a held-out task", ex.Task)
		}
		if i%10 == 9 {
			va = append(va, ex)
		} else {
			tr = append(tr, ex)
		}
	}
	if len(va) == 0 && len(tr) > 1 {
		va, tr = tr[len(tr)-1:], tr[:len(tr)-1]
	}
	for name, set := range map[string][]Example{"train.jsonl": tr, "valid.jsonl": va} {
		f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			return 0, 0, err
		}
		enc := json.NewEncoder(f)
		for _, ex := range set {
			if err := enc.Encode(map[string]any{"messages": []map[string]string{
				{"role": "system", "content": ex.System},
				{"role": "user", "content": ex.User},
				{"role": "assistant", "content": ex.Answer},
			}}); err != nil {
				_ = f.Close()
				return 0, 0, err
			}
		}
		if err := f.Close(); err != nil {
			return 0, 0, err
		}
	}
	return len(tr), len(va), nil
}

// Run is one task's result in an evaluation arm.
type Run struct {
	Task        string `json:"task"`
	Passed      bool   `json:"passed"`
	PromptChars int    `json:"prompt_chars"`
}

// Eval is an adapter judged against the base model (C7's bar).
type Eval struct {
	// OldTasks are the tasks the base model passed with no lessons; Retained
	// is the share the adapter still passes.
	OldTasks int     `json:"old_tasks"`
	Retained float64 `json:"retained"`
	// On held-out tasks: the base model with the text lessons, and the
	// adapter with none.
	HeldOut     int     `json:"held_out"`
	TextPass    float64 `json:"text_pass"`
	AdapterPass float64 `json:"adapter_pass"`
	Gain        float64 `json:"gain"` // AdapterPass − TextPass
	// TokensRatio is the adapter's prompt size per task over the text
	// lessons' (characters, as a proxy for tokens).
	TokensRatio float64 `json:"tokens_ratio"`
	RolledBack  bool    `json:"rolled_back"` // the rollback check passed
	Passes      bool    `json:"passes"`
	Why         string  `json:"why"`
}

// MinRetention is C7's bar: the adapter keeps ≥ 95% of what the base did.
const MinRetention = 0.95

// Evaluate compares the arms: base (no lessons, every task), text (base with
// text lessons) and adapter (no lessons), the last two on every task.
func Evaluate(base, text, adapter []Run) Eval {
	var ev Eval
	passed := func(rs []Run) map[string]bool {
		m := map[string]bool{}
		for _, r := range rs {
			if r.Passed {
				m[r.Task] = true
			}
		}
		return m
	}
	b, t, a := passed(base), passed(text), passed(adapter)
	kept := 0
	for task := range b {
		ev.OldTasks++
		if a[task] {
			kept++
		}
	}
	if ev.OldTasks > 0 {
		ev.Retained = float64(kept) / float64(ev.OldTasks)
	}
	held := map[string]bool{}
	for _, r := range text {
		if HeldOut(r.Task) {
			held[r.Task] = true
		}
	}
	ev.HeldOut = len(held)
	tp, ap := 0, 0
	for task := range held {
		if t[task] {
			tp++
		}
		if a[task] {
			ap++
		}
	}
	if ev.HeldOut > 0 {
		ev.TextPass = float64(tp) / float64(ev.HeldOut)
		ev.AdapterPass = float64(ap) / float64(ev.HeldOut)
	}
	ev.Gain = ev.AdapterPass - ev.TextPass
	chars := func(rs []Run) float64 {
		n, c := 0, 0
		for _, r := range rs {
			if HeldOut(r.Task) {
				n++
				c += r.PromptChars
			}
		}
		if n == 0 {
			return 0
		}
		return float64(c) / float64(n)
	}
	if tc := chars(text); tc > 0 {
		ev.TokensRatio = chars(adapter) / tc
	}
	switch {
	case ev.OldTasks == 0:
		ev.Why = "the base model solved nothing without lessons: no old-task suite"
	case ev.Retained < MinRetention:
		ev.Why = fmt.Sprintf("forgets: keeps %.0f%% of what the base model solved (bar %.0f%%)", 100*ev.Retained, 100*MinRetention)
	case ev.Gain < 0:
		ev.Why = fmt.Sprintf("worse than the text lessons on held-out tasks (%.0f%% against %.0f%%)", 100*ev.AdapterPass, 100*ev.TextPass)
	case ev.TokensRatio >= 1:
		ev.Why = "no cheaper than the text lessons"
	default:
		ev.Passes = true
		ev.Why = fmt.Sprintf("keeps %.0f%%, %+.0f points against the text lessons at %.0f%% of their prompt",
			100*ev.Retained, 100*ev.Gain, 100*ev.TokensRatio)
	}
	return ev
}

// ErrNoVersion: the registry has no such adapter version.
var ErrNoVersion = errors.New("no such adapter version")
