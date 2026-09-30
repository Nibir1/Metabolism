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

// Replay re-plans ep with lessons in context.
func (Host) Replay(ctx context.Context, ep types.Episode, lessons []types.Lesson) (types.ReplayResult, error) {
	if err := ctx.Err(); err != nil {
		return types.ReplayResult{}, err
	}
	res := types.ReplayResult{EpisodeID: ep.ID, Steps: ep.Steps, Usage: ep.Usage}
	sc, ok := lookup(ep.Attrs["scenario"])
	if !ok {
		return types.ReplayResult{}, fmt.Errorf("helixsim: episode %s has unknown scenario %q", ep.ID, ep.Attrs["scenario"])
	}
	if sc.fix == "" {
		return res, nil
	}
	for _, l := range lessons {
		if strings.Contains(l.Text, sc.fix) {
			res.Steps = []types.Step{{Tool: "file", Action: "edit", Subject: "fixed", OK: true}}
			res.Usage = types.Usage{ModelCalls: 1, InputChars: ep.Usage.InputChars / max(ep.Usage.ModelCalls, 1),
				OutputChars: ep.Usage.OutputChars / max(ep.Usage.ModelCalls, 1)}
			res.Text = "applied lesson " + string(l.ID)
			return res, nil
		}
	}
	return res, nil
}

// Judge prefers the replay with fewer failed steps, then fewer model calls.
type Judge struct{}

var _ ports.Judge = Judge{}

// Compare implements ports.Judge.
func (Judge) Compare(_ context.Context, _ types.Episode, _ []types.Outcome, with, without types.ReplayResult) (types.Verdict, error) {
	fw, fo := failed(with.Steps), failed(without.Steps)
	switch {
	case fw < fo:
		return types.Verdict{Prefer: types.PreferWith, Confidence: 0.9, Reason: "fewer failed steps"}, nil
	case fw > fo:
		return types.Verdict{Prefer: types.PreferWithout, Confidence: 0.9, Reason: "more failed steps"}, nil
	case with.Usage.ModelCalls < without.Usage.ModelCalls:
		return types.Verdict{Prefer: types.PreferWith, Confidence: 0.7, Reason: "fewer model calls"}, nil
	case with.Usage.ModelCalls > without.Usage.ModelCalls:
		return types.Verdict{Prefer: types.PreferWithout, Confidence: 0.7, Reason: "more model calls"}, nil
	}
	return types.Verdict{Prefer: types.PreferNeither, Confidence: 0.5, Reason: "equivalent"}, nil
}

func failed(steps []types.Step) int {
	n := 0
	for _, s := range steps {
		if !s.OK {
			n++
		}
	}
	return n
}
