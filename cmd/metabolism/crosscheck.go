package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/types"
)

// pairJudge is a judge that can compare a pair exactly as it was presented
// to another judge.
type pairJudge interface {
	Name() string
	ComparePair(ctx context.Context, pair judge.Pair) (types.Verdict, error)
}

// crossRecord is one re-judged comparison, saved next to the comparisons.
type crossRecord struct {
	Lesson     types.ID         `json:"lesson"`
	Episode    types.ID         `json:"episode"`
	Regression bool             `json:"regression_set"`
	First      string           `json:"first_judge"`
	FirstSays  types.Preference `json:"first_prefer"`
	Second     string           `json:"second_judge"`
	SecondSays types.Preference `json:"second_prefer"`
	Confidence float64          `json:"second_confidence"`
	Reason     string           `json:"second_reason"`
}

// cmdCrosscheck re-judges saved comparisons with a second judge, shown exactly
// the pair the first judge saw, and reports how often they agree. It is a
// weaker check than human labels (two models can share blind spots) and is
// reported as such.
func cmdCrosscheck(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("crosscheck", flag.ContinueOnError)
	fs.SetOutput(out)
	from := fs.String("from", "", "comparisons file written by verify -save (required)")
	judgeName := fs.String("judge", "deepseek", "second judge: ollama, deepseek, jev")
	judgeModel := fs.String("judge-model", "", "model for the second judge")
	allowRemote := fs.Bool("allow-remote", false, "allow a remote judge to receive the saved comparisons")
	labelled := fs.Bool("labelled", false, "re-judge only the comparisons a person labelled, and score both judges against those labels (Phase 2)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" {
		return errors.New("-from is required")
	}
	comps, err := readNDJSON[comparisonRecord](*from)
	if err != nil {
		return err
	}
	var human map[string]types.Preference
	if *labelled {
		comps, human, err = labelledOnly(*from, comps)
		if err != nil {
			return err
		}
	}
	if len(comps) == 0 {
		return fmt.Errorf("%s has no comparisons to re-judge", *from)
	}
	rem := remote{allowed: *allowRemote, out: out}
	j, err := pickJudge(*judgeName, *judgeModel, rem)
	if err != nil {
		return err
	}
	pj, ok := j.(pairJudge)
	if !ok {
		return fmt.Errorf("-judge %q cannot re-judge a saved pair (use ollama, deepseek or jev)", *judgeName)
	}
	if isRemote(*judgeName) {
		rem.notice("%d saved comparisons (requests, how they went, both plans) go to %s", len(comps), pj.Name())
	}

	path := crossPath(*from, pj.Name())
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)

	sets := map[string][]bench.Labelled{}
	var vsHuman, firstVsHuman []bench.Labelled
	first := ""
	for i, c := range comps {
		pair := judge.Pair{Request: c.Request, Original: c.Original, PlanA: c.PlanA, PlanB: c.PlanB, WithIsA: c.WithIsA}
		v, err := pj.ComparePair(ctx, pair)
		if err != nil {
			return fmt.Errorf("comparison %d (%s): %w", i+1, c.Episode, err)
		}
		first = c.Judge
		rec := crossRecord{Lesson: c.Lesson, Episode: c.Episode, Regression: c.Regression, First: c.Judge,
			FirstSays: c.JudgePrefer, Second: pj.Name(), SecondSays: v.Prefer, Confidence: v.Confidence, Reason: v.Reason}
		if err := enc.Encode(rec); err != nil {
			return err
		}
		if h, ok := human[string(c.Lesson)+"/"+string(c.Episode)]; ok {
			vsHuman = append(vsHuman, bench.Labelled{Human: h, Judge: v.Prefer})
			firstVsHuman = append(firstVsHuman, bench.Labelled{Human: h, Judge: c.JudgePrefer})
		}
		// Score() calls its fields Human and Judge; here they are the second
		// and first judge.
		l := bench.Labelled{Human: v.Prefer, Judge: c.JudgePrefer}
		sets["all"] = append(sets["all"], l)
		if c.Regression {
			sets["past successes"] = append(sets["past successes"], l)
		} else {
			sets["episodes the lesson is about"] = append(sets["episodes the lesson is about"], l)
		}
	}

	if *labelled {
		say(out, "against your %d labels (the Phase 2 bar is ≥ 80%%):\n", len(vsHuman))
		for _, x := range []struct {
			name string
			ls   []bench.Labelled
		}{{pj.Name(), vsHuman}, {first + " (its saved verdicts)", firstVsHuman}} {
			a := bench.Score(x.ls)
			say(out, "  %-40s agree %d of %d (%s), Cohen's κ %.2f\n", x.name, a.Agree, a.N, pct(a.Rate), a.Kappa)
		}
		a := bench.Score(vsHuman)
		for k, n := range a.Confused {
			say(out, "    you→%s %s: %d\n", pj.Name(), k, n)
		}
		say(out, "\n")
	}
	say(out, "%s against %s on %d saved comparisons (the same view, the same A/B order)\n\n", pj.Name(), first, len(comps))
	for _, name := range []string{"all", "episodes the lesson is about", "past successes"} {
		ls := sets[name]
		if len(ls) == 0 {
			continue
		}
		a := bench.Score(ls)
		say(out, "  %-30s agree %d of %d (%s), Cohen's κ %.2f\n", name, a.Agree, a.N, pct(a.Rate), a.Kappa)
	}
	all := bench.Score(sets["all"])
	if len(all.Confused) > 0 {
		say(out, "\n  where they differ (%s → %s):\n", pj.Name(), first)
		for k, n := range all.Confused {
			say(out, "    %-20s %d\n", k, n)
		}
	}
	say(out, "\nThis is judge-against-judge agreement, not the human-labelled Phase 2 criterion:\n")
	say(out, "two models can agree on the same mistake. Saved: %s\n", path)
	return nil
}

// labelledOnly keeps the comparisons a person labelled, with each label as
// a preference about the lesson.
func labelledOnly(from string, comps []comparisonRecord) ([]comparisonRecord, map[string]types.Preference, error) {
	labels, err := readNDJSON[labelRecord](from + ".labels")
	if err != nil {
		return nil, nil, fmt.Errorf("no labels for %s: %w", from, err)
	}
	done := map[string]string{}
	for _, l := range labels {
		done[string(l.Lesson)+"/"+string(l.Episode)] = l.Label
	}
	var out []comparisonRecord
	human := map[string]types.Preference{}
	for _, c := range comps {
		k := string(c.Lesson) + "/" + string(c.Episode)
		if label, ok := done[k]; ok {
			out = append(out, c)
			human[k] = judge.Pair{WithIsA: c.WithIsA}.Verdict(label, 1, "").Prefer
		}
	}
	return out, human, nil
}

// crossPath names the saved cross verdicts after the second judge, keeping
// only characters every filesystem accepts ("llm:ollama/qwen3:8b" has colons,
// which Windows refuses).
func crossPath(from, judgeName string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-':
			return r
		}
		return '_'
	}, judgeName)
	return from + ".crosscheck." + safe
}
