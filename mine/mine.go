// Package mine finds failures that recur the same way in a journal, and
// sorts them by what could answer them (Phase 5c, docs/habits.md, D-031).
//
// A guard only restricts: it adds a question or a refusal before a kind of
// step, after the user approves it. The evidence for one is a person pushing
// back on that kind of step, again and again: undoing what it did, or
// declining it when asked. A step that simply fails the same way (a guessed
// path, a tool used wrongly) is not that: it is a mistake, and a lesson is
// what answers a mistake. Refusals with no person behind them (an unattended
// daemon refuses every confirmation by policy) are nobody's judgement and
// are left out.
//
// So the miner proposes nothing on its own authority. It lists patterns,
// each with its evidence and the kind of answer it calls for; a guard is
// only ever proposed text for the user to approve in the host.
package mine

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/Nibir1/metabolism/types"
)

// Kind is what could answer a pattern.
type Kind string

const (
	// KindGuard: a person undid or declined this kind of step repeatedly.
	KindGuard Kind = "guard"
	// KindLesson: this kind of step failed the same way repeatedly.
	KindLesson Kind = "lesson"
	// KindExcluded: refused with no person behind it (unattended policy).
	KindExcluded Kind = "excluded"
)

// Pattern is one recurring failure.
type Pattern struct {
	Kind Kind `json:"kind"`
	// Step is the kind of step: tool, action, and what it acts on, reduced
	// (a command's first words, a file's extension).
	Step string `json:"step"`
	// Error is the failure, with paths and numbers reduced ("" for undone).
	Error string `json:"error,omitempty"`
	// Why says what made it this kind: "undone", "declined", "failed",
	// "refused unattended".
	Why      string     `json:"why"`
	Count    int        `json:"count"`
	Requests int        `json:"distinct_requests"`
	Episodes []types.ID `json:"episodes"`
	Example  string     `json:"example_request"`
	// Proposal is the guard to put to the user, or the lesson's gist; empty
	// for excluded patterns.
	Proposal string `json:"proposal,omitempty"`
}

// Options sets how often a failure must recur to count.
type Options struct {
	MinCount    int // default 3
	MinRequests int // distinct requests, default 2
}

// refusedRe matches a step refused because its confirmation was not given.
var refusedRe = regexp.MustCompile(`(?i)refused|declined|not given|confirmation`)

// Patterns finds the recurring failures in eps, with their outcomes.
func Patterns(eps []types.Episode, outs map[types.ID][]types.Outcome, o Options) []Pattern {
	if o.MinCount <= 0 {
		o.MinCount = 3
	}
	if o.MinRequests <= 0 {
		o.MinRequests = 2
	}
	type acc struct {
		p        Pattern
		requests map[string]bool
	}
	groups := map[string]*acc{}
	add := func(kind Kind, why, step, errText string, ep types.Episode, proposal string) {
		key := string(kind) + "|" + why + "|" + step + "|" + errText
		a := groups[key]
		if a == nil {
			a = &acc{p: Pattern{Kind: kind, Step: step, Error: errText, Why: why, Example: ep.Request.Text, Proposal: proposal},
				requests: map[string]bool{}}
			groups[key] = a
		}
		if slices.Contains(a.p.Episodes, ep.ID) {
			return
		}
		a.p.Count++
		a.p.Episodes = append(a.p.Episodes, ep.ID)
		a.requests[strings.ToLower(strings.TrimSpace(ep.Request.Text))] = true
	}
	for _, ep := range eps {
		undone, declined := false, false
		for _, out := range outs[ep.ID] {
			switch out.Kind {
			case types.OutcomeUndone:
				undone = true
			case types.OutcomeDeclined:
				declined = true
			}
		}
		for _, s := range ep.Steps {
			step := StepKind(s)
			refused := !s.OK && refusedRe.MatchString(s.Err)
			switch {
			case refused && declined:
				add(KindGuard, "declined", step, "", ep,
					fmt.Sprintf("Do not plan %s here: you declined it when asked.", step))
			case refused:
				add(KindExcluded, "refused unattended", step, reduce(s.Err), ep, "")
			case !s.OK:
				add(KindLesson, "failed", step, reduce(s.Err), ep,
					fmt.Sprintf("%s keeps failing with %q: a lesson for the reflector, not a guard.", step, reduce(s.Err)))
			case undone && mutates(s):
				add(KindGuard, "undone", step, "", ep,
					fmt.Sprintf("Ask before %s here: you undid it afterwards.", step))
			}
		}
	}
	var out []Pattern
	for _, a := range groups {
		a.p.Requests = len(a.requests)
		if a.p.Count >= o.MinCount && a.p.Requests >= o.MinRequests {
			out = append(out, a.p)
		}
	}
	order := map[Kind]int{KindGuard: 0, KindLesson: 1, KindExcluded: 2}
	slices.SortFunc(out, func(x, y Pattern) int {
		if d := order[x.Kind] - order[y.Kind]; d != 0 {
			return d
		}
		if d := y.Count - x.Count; d != 0 {
			return d
		}
		return strings.Compare(x.Step, y.Step)
	})
	return out
}

// StepKind reduces a step to what kind of step it is: the tool and action,
// and for a command its first words, for a file its extension.
func StepKind(s types.Step) string {
	k := s.Tool
	if s.Action != "" {
		k += " " + s.Action
	}
	subj := strings.TrimSpace(s.Subject)
	switch s.Tool {
	case "shell", "git", "package":
		// The command after any leading "cd <dir> &&".
		if i := strings.LastIndex(subj, "&&"); i >= 0 {
			subj = strings.TrimSpace(subj[i+2:])
		}
		f := strings.Fields(subj)
		if len(f) > 2 {
			f = f[:2]
		}
		if len(f) > 0 {
			k += ": " + strings.Join(f, " ")
		}
	case "file":
		target := subj
		if f := strings.Fields(subj); len(f) > 1 {
			target = f[len(f)-1]
		}
		if ext := path.Ext(target); ext != "" && len(ext) < 8 {
			k += " *" + ext
		}
	}
	return k
}

var (
	pathRe = regexp.MustCompile(`(/|~)[^\s:'"]+`)
	numRe  = regexp.MustCompile(`\d+`)
)

// reduce strips paths and numbers from an error, so the same failure on
// different files is one pattern.
func reduce(s string) string {
	s = pathRe.ReplaceAllString(s, "<path>")
	s = numRe.ReplaceAllString(s, "N")
	if len(s) > 120 {
		s = s[:120]
	}
	return strings.TrimSpace(s)
}

func mutates(s types.Step) bool {
	switch s.Tool {
	case "file":
		return s.Action == "edit" || s.Action == "write"
	case "shell", "git", "package":
		return true
	}
	return false
}
