package audit

import (
	"fmt"
	"strings"
)

// Markdown renders the scorecard as summary.md: a count, then every
// criterion grouped by phase, then what to do next, with the open items
// a person can move listed first.
func Markdown(r Report) string {
	var b strings.Builder
	n := r.Count()
	fmt.Fprintf(&b, "# Metabolism scorecard · %s\n\n", r.Now.Local().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "%d criteria: %d met, %d short, %d waiting on evidence, %d manual, %d not built.\n",
		len(r.Criteria), n[Met], n[Short], n[Waiting], n[Manual], n[Unbuilt])
	fmt.Fprintf(&b, "Real use: %s (%d real episodes, %d with lessons off). Automated runs: %d episodes.\n\n",
		stageWords(r.Stage), r.Evidence.Real.Episodes, r.Evidence.Real.LessonsOff, r.Evidence.AutoEpisodes)

	b.WriteString("| Phase | Criterion | Status | Bar | Now |\n|---|---|---|---|---|\n")
	for _, c := range r.Criteria {
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s |\n", c.Phase, cell(c.Name), mark(c.Status), cell(c.Bar), cell(c.Have))
	}

	score := 0
	for _, it := range r.Rubric {
		score += it.Score
	}
	fmt.Fprintf(&b, "\n## Rubric (vision.md): %d of 9\n\n| C | Criterion | Score | Evidence | Still needs |\n|---|---|---|---|---|\n", score)
	for _, it := range r.Rubric {
		fmt.Fprintf(&b, "| %s | %s | %d | %s | %s |\n", it.C, it.Name, it.Score, cell(it.Because), cell(strings.Join(it.Needs, ", ")))
	}

	b.WriteString("\n## Next\n\n")
	for _, st := range []Status{Short, Waiting, Unbuilt, Manual} {
		for _, c := range r.Criteria {
			if c.Status == st && c.Next != "" {
				fmt.Fprintf(&b, "- **%s** (%s): %s\n", c.Name, c.Status, c.Next)
			}
		}
	}
	b.WriteString("\nThis scorecard is the input to the Phase 9 audit, not its verdict: a rubric criterion is claimed only against vision.md's bars.\n")
	return b.String()
}

func mark(s Status) string {
	switch s {
	case Met:
		return "✔ met"
	case Short:
		return "✘ short"
	case Waiting:
		return "… waiting"
	case Manual:
		return "✋ manual"
	}
	return "○ not built"
}

func stageWords(s Stage) string {
	switch s {
	case StageBaseline:
		return "collecting the lessons-off baseline"
	case StageLearning:
		return "baseline in; learning on real turns"
	}
	return "none yet"
}

func cell(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
