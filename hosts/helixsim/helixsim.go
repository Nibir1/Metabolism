// Package helixsim is a stand-in for Helix, Metabolism's first host, for the
// engine's own tests and benchmarks. It generates episodes shaped like the
// ones Helix will emit, and implements the Host and Judge ports so the replay
// harness can be exercised before the real Helix adapter exists.
//
// The scenarios are not invented failure modes. Each one reproduces a failure
// Helix's author found in a real run and fixed by hand (docs/helix-integration.md,
// "Its author already digests by hand"), so the simulator's lessons are the
// ones Metabolism should eventually learn on its own.
//
// Everything is deterministic for a given seed. Synthetic data is never a
// substitute for a real baseline; results produced from it say "helixsim" in
// their name.
package helixsim

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
)

// HostName is the Host field of every simulated episode.
const HostName = "helixsim"

// scenario is one kind of Helix turn.
type scenario struct {
	name   string
	weight int
	// fix is the tag a lesson's text must contain to repair this scenario in
	// Replay. Empty means no lesson can help (the failure is not the
	// planner's), which keeps the simulator honest about what learning can do.
	fix   string
	build func(g *gen, ep *types.Episode) []types.Outcome
}

// Scenarios, weighted roughly as a working session might mix them.
var scenarios = []scenario{
	{name: "answer", weight: 25, build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request.Text = g.pick("what does the planner package do?", "explain the sandbox rules", "summarise this diff")
		ep.Steps = []types.Step{{Tool: "response", OK: true}}
		g.usage(ep, 1)
		ep.End = types.RunDone
		return g.host(ep, types.OutcomeSuccess)
	}},
	{name: "edit-and-test", weight: 20, build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request.Text = g.pick("add input validation to parser.go", "bump the version constant", "fix the nil check in loader.go")
		ep.Steps = []types.Step{
			{Tool: "file", Action: "glob", Subject: "**/*.go", OK: true},
			{Tool: "file", Action: "read", Subject: "parser.go", OK: true},
			{Tool: "file", Action: "edit", Subject: "parser.go", OK: true},
			{Tool: "shell", Subject: "go test ./...", OK: true},
		}
		g.usage(ep, 2)
		ep.End = types.RunDone
		return g.host(ep, types.OutcomeSuccess)
	}},
	// "the model globbed eight times for it … before concluding what the
	// first result had already said"
	{name: "empty-glob-loop", weight: 10, fix: "empty-glob", build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request.Text = g.pick("find the python config loader", "where is the settings.py file?")
		for _, p := range []string{"**/config*.py", "**/config*.py", "**/*config*", "**/*loader*", "**/*.py", "**/*.py", "**/settings*", "**/*"} {
			ep.Steps = append(ep.Steps, types.Step{Tool: "file", Action: "glob", Subject: p, OK: true})
		}
		g.usage(ep, 5)
		ep.End = types.RunBudgetExhausted
		return g.host(ep, types.OutcomeFailure)
	}},
	// "a real run lost two thirds of a four-iteration budget to a
	// read package.json"
	{name: "missing-package-json", weight: 10, fix: "go-module", build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request.Text = g.pick("build the project", "check the dependencies are current")
		ep.Steps = []types.Step{
			{Tool: "file", Action: "read", Subject: "package.json", OK: false, Err: "no such file"},
			{Tool: "file", Action: "read", Subject: "package.json", OK: false, Err: "no such file"},
			{Tool: "file", Action: "read", Subject: "go.mod", OK: true},
			{Tool: "shell", Subject: "go build ./...", OK: true},
		}
		g.usage(ep, 4)
		ep.End = types.RunDone
		return g.host(ep, types.OutcomeSuccess)
	}},
	// "a real run closed 'run the tests' on a result predating its own edit"
	{name: "stale-test-evidence", weight: 8, fix: "fresh-evidence", build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request.Text = g.pick("fix the failing test and make sure it passes", "refactor the retry loop, tests must pass")
		ep.Steps = []types.Step{
			{Tool: "shell", Subject: "go test ./...", OK: true},
			{Tool: "file", Action: "edit", Subject: "retry.go", OK: true},
		}
		g.usage(ep, 2)
		ep.End = types.RunDone
		out := g.host(ep, types.OutcomeSuccess)
		return append(out, g.late(ep, types.OutcomeRepeated, types.SourceDerived, 10*time.Minute, "same request again"))
	}},
	// A package manager step the user always declines without a package name.
	{name: "declined-upgrade", weight: 7, fix: "ask-package", build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request.Text = g.pick("update my tools", "upgrade everything")
		ep.Steps = []types.Step{{Tool: "package", Action: "update", Subject: "brew upgrade", OK: false, Err: "declined by user"}}
		g.usage(ep, 1)
		ep.End = types.RunOpenWork
		out := g.host(ep, types.OutcomeFailure)
		return append(out, g.late(ep, types.OutcomeDeclined, types.SourceUser, 0, "confirmation declined"))
	}},
	// A commit that swept in unrelated files and was undone.
	{name: "undone-commit", weight: 5, fix: "review-staged", build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request.Text = g.pick("commit my changes", "commit this")
		ep.Steps = []types.Step{
			{Tool: "shell", Subject: "git add -A", OK: true},
			{Tool: "git", Action: "commit", OK: true},
		}
		g.usage(ep, 2)
		ep.End = types.RunDone
		out := g.host(ep, types.OutcomeSuccess)
		return append(out, g.late(ep, types.OutcomeUndone, types.SourceDerived, 3*time.Minute, "/undo"))
	}},
	// "An in-place sed whose pattern does not match exits 0 and changes nothing"
	{name: "sed-noop", weight: 8, fix: "file-edit", build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request.Text = g.pick("rename the Config field to Settings", "replace the old endpoint URL")
		ep.Steps = []types.Step{
			{Tool: "shell", Subject: "sed -i 's/Config/Settings/g' main.go", OK: true},
			{Tool: "shell", Subject: "go build ./...", OK: false, Err: "undefined: Settings"},
			{Tool: "shell", Subject: "sed -i '' 's/Config/Settings/g' main.go", OK: true},
			{Tool: "shell", Subject: "go build ./...", OK: false, Err: "undefined: Settings"},
		}
		g.usage(ep, 3)
		ep.End = types.RunFailed
		return g.host(ep, types.OutcomeFailure)
	}},
	// A misheard request. No lesson fixes a microphone.
	{name: "unreliable-voice", weight: 7, build: func(g *gen, ep *types.Episode) []types.Outcome {
		ep.Request = types.Request{Text: "make a new bench called test", Provenance: types.ProvUserVoiceUnreliable}
		ep.Steps = []types.Step{{Tool: "response", OK: true}}
		g.usage(ep, 1)
		ep.End = types.RunDone
		out := g.host(ep, types.OutcomeSuccess)
		return append(out, g.late(ep, types.OutcomeRepeated, types.SourceDerived, 30*time.Second, "asked again"))
	}},
}

// ScenarioNames lists the scenarios in a stable order.
func ScenarioNames() []string {
	names := make([]string, len(scenarios))
	for i, s := range scenarios {
		names[i] = s.name
	}
	return names
}

func lookup(name string) (scenario, bool) {
	for _, s := range scenarios {
		if s.name == name {
			return s, true
		}
	}
	return scenario{}, false
}

var projects = []string{"helix", "webapp", "dotfiles"}

// Config controls generation.
type Config struct {
	Seed  uint64
	Count int
	Start time.Time
	// Span is the period the episodes are spread across.
	Span time.Duration
}

// Generate produces Count episodes and their outcomes, deterministically for
// a given Config.
func Generate(cfg Config) ([]types.Episode, []types.Outcome) {
	g := &gen{r: rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15))}
	total := 0
	for _, s := range scenarios {
		total += s.weight
	}
	step := time.Duration(0)
	if cfg.Count > 0 {
		step = cfg.Span / time.Duration(cfg.Count)
	}

	eps := make([]types.Episode, 0, cfg.Count)
	var outs []types.Outcome
	for i := range cfg.Count {
		sc := g.weighted(total)
		start := cfg.Start.Add(time.Duration(i)*step + g.jitter(step/2))
		g.now = start
		ep := types.Episode{
			ID:        g.id(start),
			Host:      HostName,
			Scope:     types.Scope{Level: types.ScopeProject, Key: projects[g.r.IntN(len(projects))]},
			StartedAt: start,
			Request:   types.Request{Provenance: types.ProvUserTyped},
			Attrs:     types.Attrs{"scenario": sc.name, "machine": "sim-mac"},
		}
		if g.r.IntN(5) == 0 && ep.Request.Provenance == types.ProvUserTyped {
			ep.Request.Provenance = types.ProvUserVoice
		}
		o := sc.build(g, &ep)
		ep.EndedAt = start.Add(time.Duration(len(ep.Steps)+1) * 1500 * time.Millisecond)
		eps = append(eps, ep)
		outs = append(outs, o...)
	}
	return eps, outs
}

// gen holds the generator state. IDs come from the seeded source, not
// crypto/rand, so the same seed yields byte-identical fixtures.
type gen struct {
	r   *rand.Rand
	now time.Time
}

func (g *gen) id(at time.Time) types.ID {
	return types.ID(fmt.Sprintf("%012x%016x", at.UnixMilli()&0xffffffffffff, g.r.Uint64()))
}

func (g *gen) pick(options ...string) string { return options[g.r.IntN(len(options))] }

func (g *gen) jitter(max time.Duration) time.Duration {
	if max <= 0 {
		return 0
	}
	return time.Duration(g.r.Int64N(int64(max)))
}

func (g *gen) weighted(total int) scenario {
	n := g.r.IntN(total)
	for _, s := range scenarios {
		if n < s.weight {
			return s
		}
		n -= s.weight
	}
	return scenarios[len(scenarios)-1]
}

// usage sets a plausible cost: each planner call carries Helix's system prompt
// and context blocks (a few thousand characters) and returns a JSON plan.
func (g *gen) usage(ep *types.Episode, calls int) {
	ep.Usage.ModelCalls = calls
	for range calls {
		ep.Usage.InputChars += 3000 + g.r.IntN(3000)
		ep.Usage.OutputChars += 200 + g.r.IntN(600)
	}
}

func (g *gen) host(ep *types.Episode, kind types.OutcomeKind) []types.Outcome {
	return []types.Outcome{{ID: g.id(g.now), EpisodeID: ep.ID, At: ep.StartedAt.Add(5 * time.Second),
		Kind: kind, Source: types.SourceHost}}
}

func (g *gen) late(ep *types.Episode, kind types.OutcomeKind, src types.OutcomeSource, after time.Duration, note string) types.Outcome {
	return types.Outcome{ID: g.id(g.now), EpisodeID: ep.ID, At: ep.StartedAt.Add(10*time.Second + after),
		Kind: kind, Source: src, Note: note}
}

// Host simulates Helix's plan-only replay. A lesson whose text contains a
// scenario's fix tag turns that scenario's plan into the clean one; nothing
// else changes anything. Replay has no side effects, as the port requires.
type Host struct{}

var _ ports.Host = Host{}

// Describe returns a profile matching the budget planned for Helix.
func (Host) Describe() types.HostProfile {
	return types.HostProfile{
		Name:   HostName,
		Kinds:  []types.LessonKind{types.KindFact, types.KindPattern, types.KindPreference},
		Budget: types.Budget{MaxLessons: 8, MaxChars: 1600},
	}
}

// Replay re-plans ep with lessons in context. A lesson whose text contains
// the scenario's fix tag repairs it; a lesson carrying a harm tag damages the
// plan wherever it applies; anything else changes nothing.
func (Host) Replay(ctx context.Context, ep types.Episode, lessons []types.Lesson) (types.ReplayResult, error) {
	if err := ctx.Err(); err != nil {
		return types.ReplayResult{}, err
	}
	sc, ok := lookup(ep.Attrs["scenario"])
	if !ok {
		return types.ReplayResult{}, fmt.Errorf("helixsim: episode %s has unknown scenario %q", ep.ID, ep.Attrs["scenario"])
	}
	for _, l := range lessons {
		if !Knows(l.Text) {
			return types.ReplayResult{}, fmt.Errorf("%w: the simulator models only its own catalogue of lessons; %q needs a real host",
				ports.ErrCannotReplay, l.Text)
		}
	}
	res := types.ReplayResult{EpisodeID: ep.ID, Steps: slices.Clone(ep.Steps), Usage: ep.Usage}
	fixed := false
	if sc.fix != "" {
		for _, l := range lessons {
			if strings.Contains(l.Text, sc.fix) {
				fixed = true
				fp := fixedPlanFor(sc.name, ep)
				res.Steps = slices.Clone(fp.steps)
				res.Text = fp.reply
				res.Usage = types.Usage{ModelCalls: 1, InputChars: ep.Usage.InputChars / max(ep.Usage.ModelCalls, 1),
					OutputChars: ep.Usage.OutputChars / max(ep.Usage.ModelCalls, 1)}
				break
			}
		}
	}
	for _, l := range lessons {
		for _, h := range harms {
			if strings.Contains(l.Text, h.tag) && h.applies(sc.name) {
				res.Steps = append(res.Steps, types.Step{Tool: "shell", Subject: h.step, OK: false, Err: h.err})
				res.Usage.ModelCalls++
			}
		}
	}
	applyPoisons(&res, sc, lessons, fixed)
	return res, nil
}

// harm is a way a wrong lesson damages a plan in the simulator.
type harm struct {
	tag     string
	step    string
	err     string
	applies func(scenario string) bool
}

func everywhere(string) bool { return true }

func only(names ...string) func(string) bool {
	return func(s string) bool { return slices.Contains(names, s) }
}

var harms = []harm{
	// A lesson that sounds diligent and costs a failing step on every turn.
	{tag: "always-rerun", step: "go test ./... (re-run)", err: "exit status 1", applies: everywhere},
	// Plausible but wrong: assume a Node project, in a Go repository.
	{tag: "node-first", step: "cat package.json", err: "no such file",
		applies: only("missing-package-json", "edit-and-test", "stale-test-evidence")},
	// The exact failure the file tool was built to replace.
	{tag: "prefer-sed", step: "sed -i 's/a/b/' main.go", err: "pattern did not match",
		applies: only("edit-and-test", "sed-noop", "stale-test-evidence")},
}

// LessonSpec is a lesson the simulator knows the truth about.
type LessonSpec struct {
	Text string
	Kind types.LessonKind
	// Good says whether the lesson should survive a correct nutrient test.
	Good bool
	// Why says what makes it good or wrong.
	Why string
	// From is the scenario whose episodes the lesson would be learned from.
	From string
}

// CorrectLessons are lessons that repair a scenario (one per fixable scenario).
func CorrectLessons() []LessonSpec {
	return []LessonSpec{
		{Text: "When a glob or search comes back empty, treat that as the answer instead of loosening the pattern (empty-glob).", Kind: types.KindPattern, Good: true, Why: "repairs empty-glob-loop", From: "empty-glob-loop"},
		{Text: "This is a Go module: read go.mod, not package.json, to find build and dependency information (go-module).", Kind: types.KindFact, Good: true, Why: "repairs missing-package-json", From: "missing-package-json"},
		{Text: "Only report tests as passing from a run made after the last edit (fresh-evidence).", Kind: types.KindPattern, Good: true, Why: "repairs stale-test-evidence", From: "stale-test-evidence"},
		{Text: "The user declines package upgrades without a named package; ask which package first (ask-package).", Kind: types.KindPreference, Good: true, Why: "repairs declined-upgrade", From: "declined-upgrade"},
		{Text: "Before committing, review what is staged instead of running git add -A (review-staged).", Kind: types.KindPattern, Good: true, Why: "repairs undone-commit", From: "undone-commit"},
		{Text: "Edit files with the file tool's exact-snippet edit, not in-place sed, which reports success when it changes nothing (file-edit).", Kind: types.KindPattern, Good: true, Why: "repairs sed-noop", From: "sed-noop"},
	}
}

// WrongLessons are planted lessons a sound nutrient test must reject.
func WrongLessons() []LessonSpec {
	return []LessonSpec{
		{Text: "Prefer tabs over spaces when writing YAML files.", Kind: types.KindPreference, Why: "irrelevant: changes nothing", From: "edit-and-test"},
		{Text: "The user likes answers that start with a summary line.", Kind: types.KindPreference, Why: "irrelevant to every simulated failure", From: "answer"},
		{Text: "Re-run the full test suite after every step to be safe (always-rerun).", Kind: types.KindPattern, Why: "harmful: a failing extra step everywhere", From: "edit-and-test"},
		{Text: "Assume a Node project and read package.json first when building (node-first).", Kind: types.KindFact, Why: "plausible but wrong in a Go repository", From: "missing-package-json"},
		{Text: "Use in-place sed for quick renames; it is faster than the file tool (prefer-sed).", Kind: types.KindPattern, Why: "reintroduces the sed no-op failure", From: "sed-noop"},
	}
}

// Reflector proposes CorrectLessons for scenarios that keep failing, the
// way a perfect reflector would. It exists to test the pipeline around
// reflection, not reflection itself.
type Reflector struct{}

var _ ports.Reflector = Reflector{}

// Name implements ports.Reflector.
func (Reflector) Name() string { return "helixsim" }

// Reflect implements ports.Reflector.
func (Reflector) Reflect(_ context.Context, batch []types.Experience, _ []types.Lesson) ([]types.Lesson, error) {
	failures := map[string][]types.ID{}
	for _, x := range batch {
		if !x.Succeeded {
			sc := x.Episode.Attrs["scenario"]
			failures[sc] = append(failures[sc], x.Episode.ID)
		}
	}
	var out []types.Lesson
	for _, spec := range CorrectLessons() {
		for _, sc := range scenarios {
			if sc.fix == "" || !strings.Contains(spec.Text, "("+sc.fix+")") || len(failures[sc.name]) < 2 {
				continue
			}
			src := failures[sc.name]
			if len(src) > 5 {
				src = src[:5]
			}
			out = append(out, types.Lesson{
				Kind: spec.Kind, Scope: types.Scope{Level: types.ScopeGlobal}, Text: spec.Text,
				Sources: src, Rationale: fmt.Sprintf("%d %s episodes failed", len(failures[sc.name]), sc.name),
			})
		}
	}
	return out, nil
}

// fixedPlan is what a repaired scenario plans instead: the plan a capable
// assistant would make had it known the lesson. It must read as a sensible
// answer to the scenario's requests, because model judges read it; a
// placeholder like "edit: fixed" reads as an unrequested change and is
// rightly judged worse than the broken original.
type fixedPlan struct {
	steps []types.Step
	reply string
}

// fixedPlanFor answers the episode's actual request. The first live judge run
// caught a request for settings.py being "repaired" with a search for
// *config*.py; the judge was right to reject it.
func fixedPlanFor(scenario string, ep types.Episode) fixedPlan {
	if scenario == "empty-glob-loop" && strings.Contains(ep.Request.Text, "settings") {
		return fixedPlan{
			steps: []types.Step{{Tool: "file", Action: "glob", Subject: "**/settings*.py", OK: true}},
			reply: "There is no settings.py in this repository; the search for settings*.py found nothing.",
		}
	}
	return fixedPlans[scenario]
}

var fixedPlans = map[string]fixedPlan{
	"empty-glob-loop": {
		steps: []types.Step{{Tool: "file", Action: "glob", Subject: "**/*config*.py", OK: true}},
		reply: "There is no Python config loader in this repository; the search for *config*.py found nothing.",
	},
	"missing-package-json": {
		steps: []types.Step{
			{Tool: "file", Action: "read", Subject: "go.mod", OK: true},
			{Tool: "shell", Subject: "go build ./...", OK: true},
		},
		reply: "Built the Go module; it compiles cleanly.",
	},
	"stale-test-evidence": {
		steps: []types.Step{
			{Tool: "file", Action: "edit", Subject: "retry.go", OK: true},
			{Tool: "shell", Subject: "go test ./...", OK: true},
		},
		reply: "Made the change, then ran the tests after it: they pass.",
	},
	"declined-upgrade": {
		steps: []types.Step{{Tool: "response", OK: true}},
		reply: "Which package would you like me to upgrade? I can list the outdated ones first.",
	},
	"undone-commit": {
		steps: []types.Step{
			{Tool: "shell", Subject: "git status --short", OK: true},
			{Tool: "shell", Subject: "git diff --staged --stat", OK: true},
		},
		reply: "These files are staged. Should I commit exactly these, or add the others too?",
	},
	"sed-noop": {
		steps: []types.Step{
			{Tool: "file", Action: "edit", Subject: "main.go", OK: true},
			{Tool: "shell", Subject: "go build ./...", OK: true},
		},
		reply: "Renamed it with an exact edit; the build passes.",
	},
}

// Knows reports whether the simulator has a model of what a lesson does: it
// carries a fix or harm tag, or is one of the catalogued lessons. Anything
// else, such as a lesson a real model wrote in its own words, the simulator
// cannot replay meaningfully: treating it as "changes nothing" would fail
// good lessons for the simulator's ignorance.
func Knows(text string) bool {
	for _, sc := range scenarios {
		if sc.fix != "" && strings.Contains(text, sc.fix) {
			return true
		}
	}
	for _, h := range harms {
		if strings.Contains(text, h.tag) {
			return true
		}
	}
	if _, ok := poisonFor(text); ok {
		return true
	}
	for _, s := range append(CorrectLessons(), WrongLessons()...) {
		if s.Text == text {
			return true
		}
	}
	return false
}

// Live is a turn as it would have gone with lessons delivered into it, for
// testing the credit ledger: ep and outs are the turn without lessons. A
// lesson that repairs the scenario makes the turn the clean plan and a
// success; a harmful lesson that applies adds its failing step and makes the
// turn a failure, whatever else was delivered. Anything else changes
// nothing, so the turn keeps its original outcomes.
func Live(ep types.Episode, outs []types.Outcome, lessons []types.Lesson) (types.Episode, []types.Outcome, error) {
	sc, ok := lookup(ep.Attrs["scenario"])
	if !ok {
		return ep, outs, fmt.Errorf("helixsim: episode %s has unknown scenario %q", ep.ID, ep.Attrs["scenario"])
	}
	res, err := Host{}.Replay(context.Background(), ep, lessons)
	if err != nil {
		return ep, outs, err
	}
	harmed, fixed := false, false
	for _, l := range lessons {
		for _, h := range harms {
			harmed = harmed || (strings.Contains(l.Text, h.tag) && h.applies(sc.name))
		}
		fixed = fixed || (sc.fix != "" && strings.Contains(l.Text, sc.fix))
	}
	// Poisons: a failing step fails the turn; a silent harm or a false fix
	// changes what was done but not how the turn looks, so its outcomes stay
	// as they were.
	poisonFailed, poisonSilent := applyPoisons(&types.ReplayResult{Steps: slices.Clone(ep.Steps)}, sc, lessons, fixed)
	harmed = harmed || poisonFailed
	if !harmed && !fixed {
		if poisonSilent {
			ep.Steps, ep.Usage = res.Steps, res.Usage
		}
		return ep, outs, nil
	}
	ep.Steps, ep.Usage = res.Steps, res.Usage
	kind := types.OutcomeSuccess
	ep.End = types.RunDone
	if harmed {
		kind, ep.End = types.OutcomeFailure, types.RunFailed
	}
	// One host outcome replaces the old ones: a repaired turn collects no
	// late undo or repeat, and a harmed one has failed. The first outcome's
	// ID is kept so a simulation stays deterministic.
	id := types.NewID(ep.StartedAt)
	if len(outs) > 0 {
		id = outs[0].ID
	}
	return ep, []types.Outcome{{ID: id, EpisodeID: ep.ID, At: ep.StartedAt.Add(5 * time.Second),
		Kind: kind, Source: types.SourceHost}}, nil
}

// Drift is a change in the world (D-026): from At on, a lesson whose text
// carries Tag no longer holds. Its scenario's old failure is gone (the
// project changed, so a turn without the lesson now succeeds), and the
// lesson's advice, aimed at the old world, now makes the turn fail: advice
// for a build tool the project has left behind.
type Drift struct {
	Tag string
	At  time.Time
}

// LiveIn is Live in a world that may have changed: a delivered lesson whose
// drift has happened by the turn's start no longer repairs its scenario, and
// makes the turn fail there instead.
func LiveIn(drifts []Drift, ep types.Episode, outs []types.Outcome, lessons []types.Lesson) (types.Episode, []types.Outcome, error) {
	sc, ok := lookup(ep.Attrs["scenario"])
	if !ok {
		return ep, outs, fmt.Errorf("helixsim: episode %s has unknown scenario %q", ep.ID, ep.Attrs["scenario"])
	}
	var still []types.Lesson
	stale, changed := false, false
	for _, d := range drifts {
		if sc.fix != "" && strings.Contains(d.Tag, sc.fix) && !ep.StartedAt.Before(d.At) {
			changed = true
		}
	}
	for _, l := range lessons {
		drifted := false
		for _, d := range drifts {
			if strings.Contains(l.Text, d.Tag) && !ep.StartedAt.Before(d.At) {
				drifted = true
			}
		}
		if !drifted {
			still = append(still, l)
			continue
		}
		if sc.fix != "" && strings.Contains(l.Text, sc.fix) {
			stale = true
		}
	}
	live, liveOuts, err := Live(ep, outs, still)
	if err != nil || !changed {
		return live, liveOuts, err
	}
	id := types.NewID(ep.StartedAt)
	if len(liveOuts) > 0 {
		id = liveOuts[0].ID
	}
	if !stale {
		// The world changed: the scenario's old failure is gone.
		fp := fixedPlanFor(sc.name, ep)
		live.Steps, live.End = slices.Clone(fp.steps), types.RunDone
		return live, []types.Outcome{{ID: id, EpisodeID: ep.ID, At: ep.StartedAt.Add(5 * time.Second),
			Kind: types.OutcomeSuccess, Source: types.SourceHost}}, nil
	}
	live.Steps = append(slices.Clone(live.Steps), types.Step{Tool: "shell", Subject: "follow advice that no longer holds", OK: false, Err: "the project changed"})
	live.Usage.ModelCalls++
	live.End = types.RunFailed
	return live, []types.Outcome{{ID: id, EpisodeID: ep.ID, At: ep.StartedAt.Add(5 * time.Second),
		Kind: types.OutcomeFailure, Source: types.SourceHost}}, nil
}
