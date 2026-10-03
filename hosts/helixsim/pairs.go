package helixsim

import (
	"slices"
	"strings"

	"github.com/Nibir1/metabolism/types"
)

// Lessons that come in pairs with a correct lesson, for contradictions and
// supersession (D-027).
//
//   - A paraphrase says what a correct lesson says in other words. It
//     repairs the same scenario the same way, so delivering both adds
//     nothing to delivering either.
//   - An alternative repairs the same scenario another way, and is as good
//     alone. Delivered together with its rival, the two instructions
//     contradict each other and the turn fails: the planner follows half of
//     each.

// PairSpec is a lesson paired with one of CorrectLessons.
type PairSpec struct {
	LessonSpec
	// With is the text of the correct lesson it pairs with.
	With string
	// Conflict says whether delivering both fails the turn (an alternative);
	// otherwise it is a paraphrase.
	Conflict bool
}

type alternative struct {
	tag      string
	scenario string
	against  string // the rival's fix tag
	plan     fixedPlan
}

var alternatives = []alternative{
	{tag: "widen-once", scenario: "empty-glob-loop", against: "empty-glob", plan: fixedPlan{
		steps: []types.Step{
			{Tool: "file", Action: "glob", Subject: "**/*config*.py", OK: true},
			{Tool: "file", Action: "glob", Subject: "../**/*config*.py", OK: true},
		},
		reply: "There is no Python config loader here or one directory up; both searches found nothing.",
	}},
	{tag: "write-whole", scenario: "sed-noop", against: "file-edit", plan: fixedPlan{
		steps: []types.Step{
			{Tool: "file", Action: "read", Subject: "main.go", OK: true},
			{Tool: "file", Action: "write", Subject: "main.go", OK: true},
			{Tool: "shell", Subject: "go build ./...", OK: true},
		},
		reply: "Rewrote main.go with the rename; the build passes.",
	}},
}

// PairLessons lists the paraphrases and alternatives, each with the correct
// lesson it pairs with.
func PairLessons() []PairSpec {
	correct := func(tag string) string {
		for _, s := range CorrectLessons() {
			if strings.Contains(s.Text, "("+tag+")") {
				return s.Text
			}
		}
		panic("helixsim: no correct lesson tagged " + tag)
	}
	return []PairSpec{
		{LessonSpec: LessonSpec{Text: "If a search finds nothing, report that nothing was found rather than broadening it (empty-glob).",
			Kind: types.KindPattern, Good: true, Why: "paraphrase: repairs empty-glob-loop as its rival does", From: "empty-glob-loop"},
			With: correct("empty-glob")},
		{LessonSpec: LessonSpec{Text: "Look at git status and the staged diff before a commit; never git add -A (review-staged).",
			Kind: types.KindPattern, Good: true, Why: "paraphrase: repairs undone-commit as its rival does", From: "undone-commit"},
			With: correct("review-staged")},
		{LessonSpec: LessonSpec{Text: "When a search finds nothing, widen it once to the parent directory, then answer (widen-once).",
			Kind: types.KindPattern, Good: true, Why: "alternative: repairs empty-glob-loop alone, fails with its rival", From: "empty-glob-loop"},
			With: correct("empty-glob"), Conflict: true},
		{LessonSpec: LessonSpec{Text: "Rename by rewriting the whole file with the file tool's write, then build (write-whole).",
			Kind: types.KindPattern, Good: true, Why: "alternative: repairs sed-noop alone, fails with its rival", From: "sed-noop"},
			With: correct("file-edit"), Conflict: true},
	}
}

// alternativeIn returns the alternative among lessons that repairs sc, and
// whether its rival is delivered too, which makes the turn fail.
func alternativeIn(sc scenario, lessons []types.Lesson) (alt alternative, ok, conflict bool) {
	has := func(tag string) bool {
		return slices.ContainsFunc(lessons, func(l types.Lesson) bool { return strings.Contains(l.Text, "("+tag+")") })
	}
	for _, a := range alternatives {
		if a.scenario == sc.name && has(a.tag) {
			return a, true, has(a.against)
		}
	}
	return alternative{}, false, false
}

func knowsAlternative(text string) bool {
	return slices.ContainsFunc(alternatives, func(a alternative) bool { return strings.Contains(text, "("+a.tag+")") })
}

// Contradicted reports whether lessons delivered together on ep contradict
// each other there: an alternative with its rival, on the scenario both
// repair.
func Contradicted(ep types.Episode, lessons []types.Lesson) bool {
	sc, ok := lookup(ep.Attrs["scenario"])
	if !ok {
		return false
	}
	_, ok, conflict := alternativeIn(sc, lessons)
	return ok && conflict
}
