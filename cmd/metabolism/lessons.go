package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	helixhost "github.com/Nibir1/metabolism/adapters/remote/helix"
	"github.com/Nibir1/metabolism/adapters/remote/jev"
	"github.com/Nibir1/metabolism/adapters/remote/llm"
	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/digest"
	"github.com/Nibir1/metabolism/hosts/helixsim"
	"github.com/Nibir1/metabolism/judge"
	"github.com/Nibir1/metabolism/llmreflect"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
	"github.com/Nibir1/metabolism/verify"
)

// Reflectors, judges and the models behind them are chosen here, and this is
// the only place a remote one can be switched on. Local is the default:
// Ollama on this machine. DeepSeek and Jev send data off the machine, so they
// need -allow-remote, and say what they are about to send (D-008).

// remote is the opt-in a command was given.
type remote struct {
	allowed bool
	out     io.Writer
}

func (r remote) require(what, where string) error {
	if !r.allowed {
		return fmt.Errorf("%s sends data to %s; pass -allow-remote to opt in", what, where)
	}
	return nil
}

func (r remote) notice(format string, a ...any) {
	say(r.out, "opt-in: "+format+"\n", a...)
}

func ollamaURL() string {
	u := os.Getenv("OLLAMA_HOST")
	if u == "" {
		return llm.DefaultOllamaURL
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	return u
}

// pickModel returns a model by provider. Local is free; remote needs opt-in.
func pickModel(provider, model string, r remote) (ports.Model, error) {
	switch provider {
	case "ollama":
		u := ollamaURL()
		if !llm.IsLoopback(u) {
			if err := r.require("an Ollama server at "+u, u); err != nil {
				return nil, err
			}
		}
		if model == "" {
			model = "qwen3:8b"
		}
		return llm.Ollama{BaseURL: u, Model: model}, nil
	case "deepseek":
		if err := r.require("DeepSeek", "api.deepseek.com"); err != nil {
			return nil, err
		}
		key := os.Getenv("DEEPSEEK_API_KEY")
		if key == "" {
			return nil, fmt.Errorf("DEEPSEEK_API_KEY is not set")
		}
		return llm.DeepSeek(model, key), nil
	}
	return nil, fmt.Errorf("unknown model provider %q (have: ollama, deepseek)", provider)
}

func pickReflector(name, model string, r remote) (ports.Reflector, error) {
	switch name {
	case "sim", "helixsim":
		return helixsim.Reflector{}, nil
	case "ollama", "deepseek":
		m, err := pickModel(name, model, r)
		if err != nil {
			return nil, err
		}
		return &llmreflect.Reflector{Model: m}, nil
	}
	return nil, fmt.Errorf("-reflector %q is not available (have: sim, ollama, deepseek)", name)
}

// defaultRounds is a first plan plus the most follow-up rounds a live Helix
// turn gets for a file lookup: 1 + 6 (D-021, D-022).
const defaultRounds = 7

// replayNotice says what a Helix replay sends, for the opt-in notice.
func replayNotice(rounds int) string {
	if rounds > 1 {
		return fmt.Sprintf("each replay sends a past request (and the lesson) to Helix's configured model provider, "+
			"for up to %d rounds, with what read-only file steps read under the replay directory", rounds)
	}
	return "each replay sends a past request (and the lesson) to Helix's configured model provider"
}

func pickHost(name, bin, dir string, rounds int, r remote) (ports.Host, error) {
	switch name {
	case "sim", "helixsim":
		return helixsim.Host{}, nil
	case "helix":
		what := "Helix's configured model provider (past requests are planned again)"
		if rounds > 1 {
			what = "Helix's configured model provider (past requests are planned again, with what read-only file steps " +
				"read under the replay directory between rounds)"
		}
		if err := r.require("replaying through Helix", what); err != nil {
			return nil, err
		}
		return &helixhost.Host{Command: bin, Dir: dir, Rounds: rounds}, nil
	}
	return nil, fmt.Errorf("-host %q is not available (have: sim, helix)", name)
}

// checkPair refuses combinations that cannot mean anything: the heuristic
// judge counts failed steps, and a real replay only plans, so nothing failed.
func checkPair(host, judgeName string) error {
	if host == "helix" && judgeName == "heuristic" {
		return fmt.Errorf("the heuristic judge cannot read real plans (nothing ran, so nothing failed); use -judge jev, deepseek or ollama")
	}
	return nil
}

func closeHost(h ports.Host) {
	if c, ok := h.(io.Closer); ok {
		_ = c.Close()
	}
}

func pickJudge(name, model string, r remote) (ports.Judge, error) {
	switch name {
	case "heuristic":
		return judge.Heuristic{}, nil
	case "ollama", "deepseek":
		m, err := pickModel(name, model, r)
		if err != nil {
			return nil, err
		}
		return judge.LLM{Model: m}, nil
	case "jev":
		if err := r.require("the Jev judge", "TypeSafe's API"); err != nil {
			return nil, err
		}
		return jev.New(model)
	}
	return nil, fmt.Errorf("-judge %q is not available (have: heuristic, ollama, deepseek, jev)", name)
}

func isRemote(name string) bool { return name == "deepseek" || name == "jev" }

func parseDay(flagName, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("-%s %q: want YYYY-MM-DD", flagName, s)
	}
	return t, nil
}

func cmdDigest(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("digest", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	refl := fs.String("reflector", "ollama", "what proposes lessons: ollama (local), deepseek (remote), sim")
	model := fs.String("model", "", "model name (ollama default qwen3:8b, deepseek default deepseek-flash)")
	allowRemote := fs.Bool("allow-remote", false, "allow a remote reflector to receive episodes")
	since := fs.String("since", "", "only episodes started on or after this day (YYYY-MM-DD)")
	until := fs.String("until", "", "only episodes started before this day (YYYY-MM-DD)")
	maxLessons := fs.Int("max", 10, "most lessons one run may record")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rem := remote{allowed: *allowRemote, out: out}
	r, err := pickReflector(*refl, *model, rem)
	if err != nil {
		return err
	}
	from, err := parseDay("since", *since)
	if err != nil {
		return err
	}
	to, err := parseDay("until", *until)
	if err != nil {
		return err
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	batch, err := digest.Batch(ctx, j, from, to)
	if err != nil {
		return err
	}
	if isRemote(*refl) {
		rem.notice("up to 40 of %d episodes (requests and steps, already masked by the host) go to %s", len(batch), r.Name())
	}
	res, err := digest.Run(ctx, m, r, batch, digest.Config{MaxLessons: *maxLessons})
	if err != nil {
		return err
	}
	say(out, "%s reflected on %d episodes: %d proposed, %d recorded\n\n", res.Reflector, res.Batch, res.Proposed, res.Recorded)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, d := range res.Decisions {
		note := d.Reason
		if d.Lesson != "" {
			note = string(d.Lesson)
		}
		say(tw, "  %s\t%s\t%s\n", d.State, truncate(d.Text, 70), note)
	}
	_ = tw.Flush()
	if res.Recorded > 0 {
		say(out, "\nnext: metabolism verify   (the nutrient test)\n")
	}
	return nil
}

func cmdVerify(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	hostName := fs.String("host", "sim", "what replays episodes without side effects: sim, helix")
	helixBin := fs.String("helix-bin", "helix", "helix binary for -host helix (e.g. bin/helix from make helix-build)")
	replayDir := fs.String("replay-dir", "", "directory Helix plans replays in (default: this one); episodes record only a hashed project key")
	rounds := fs.Int("rounds", defaultRounds, "planner rounds per Helix replay; between rounds only read-only file steps run (1: first plan only)")
	save := fs.String("save", "", "append every judged comparison to this NDJSON file, for metabolism label")
	judgeName := fs.String("judge", "heuristic", "what compares replays: heuristic, ollama, deepseek, jev")
	judgeModel := fs.String("judge-model", "", "model for an ollama, deepseek or jev judge")
	allowRemote := fs.Bool("allow-remote", false, "allow a remote judge to receive replayed plans")
	only := fs.String("lesson", "", "test one lesson (default: every candidate and quarantined lesson)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rem := remote{allowed: *allowRemote, out: out}
	if err := checkPair(*hostName, *judgeName); err != nil {
		return err
	}
	host, err := pickHost(*hostName, *helixBin, *replayDir, *rounds, rem)
	if err != nil {
		return err
	}
	defer closeHost(host)
	if *hostName == "helix" {
		rem.notice("%s", replayNotice(*rounds))
	}
	jd, err := pickJudge(*judgeName, *judgeModel, rem)
	if err != nil {
		return err
	}
	if isRemote(*judgeName) {
		rem.notice("each comparison (the request, how it went, and both plans) goes to %s", jd.Name())
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	h, err := verify.LoadHistory(ctx, j)
	if err != nil {
		return err
	}
	var ids []types.ID
	if *only != "" {
		ids = []types.ID{types.ID(*only)}
	} else {
		for _, l := range m.Lessons(types.StateCandidate, types.StateQuarantined) {
			ids = append(ids, l.ID)
		}
	}
	if len(ids) == 0 {
		say(out, "nothing to test: no candidate or quarantined lessons (run metabolism digest first)\n")
		return nil
	}
	v := &verify.Verifier{Host: host, Judge: jd, History: h}
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	say(tw, "  result\tmove\tW/L/T\tworse/better\tnoise\tlesson\n")
	skipped := 0
	for _, id := range ids {
		rep, err := v.Test(ctx, m, id)
		if errors.Is(err, ports.ErrCannotReplay) {
			e, _ := m.Get(id)
			say(tw, "  can't judge\t%s (unchanged)\t-\t-\t%s\n", e.Lesson.State, truncate(e.Lesson.Text, 60))
			skipped++
			continue
		}
		if err != nil {
			return err
		}
		e, _ := m.Get(id)
		if *save != "" {
			if err := saveComparisons(*save, e.Lesson, rep, h, jd.Name()); err != nil {
				return err
			}
		}
		ev := rep.Evidence
		say(tw, "  %s\t%s → %s\t%d/%d/%d\t%d/%d\t%d/%d\t%s\n", ev.Result, rep.From, rep.To,
			ev.Wins, ev.Losses, ev.Ties, ev.Regressions, ev.RegressionWins, ev.NoiseDecisive, ev.NoiseCompared,
			truncate(e.Lesson.Text, 60))
	}
	_ = tw.Flush()
	say(out, "\njudge: %s · W/L/T on the episodes a lesson is about; worse/better on past successes; noise = decisive baseline-vs-baseline pairs (D-018)\n", jd.Name())
	say(out, "details: metabolism lessons -id <ID>\n")
	if skipped > 0 {
		say(out, "%d lesson(s) not tested: host %q cannot replay them meaningfully (the simulator only models its own catalogue). Nothing was recorded for them.\n", skipped, *hostName)
	}
	return nil
}

func cmdLessons(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("lessons", flag.ContinueOnError)
	fs.SetOutput(out)
	path := fs.String("journal", defaultJournal(), "journal file")
	state := fs.String("state", "", "only lessons in this state")
	id := fs.String("id", "", "show one lesson: its sources, history and evidence")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *state != "" && !types.LessonState(*state).Valid() {
		return fmt.Errorf("-state %q is not a lesson state", *state)
	}
	j, err := openExisting(ctx, *path)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	m, err := metabolome.Load(ctx, j)
	if err != nil {
		return err
	}
	if *id != "" {
		return lessonWhy(m, types.ID(*id), out)
	}
	var ls []types.Lesson
	if *state != "" {
		ls = m.Lessons(types.LessonState(*state))
	} else {
		ls = m.Lessons()
	}
	counts := map[types.LessonState]int{}
	for _, l := range m.Lessons() {
		counts[l.State]++
	}
	var parts []string
	for _, s := range []types.LessonState{types.StateCandidate, types.StateQuarantined, types.StateTrial,
		types.StateAccepted, types.StateSuperseded, types.StateEliminated} {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", s, counts[s]))
		}
	}
	if len(parts) == 0 {
		say(out, "no lessons yet (metabolism digest proposes them)\n")
		return nil
	}
	say(out, "%s\n\n", strings.Join(parts, " · "))
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	for _, l := range ls {
		say(tw, "  %s\t%s\t%s\t%s\n", l.ID, l.State, l.Kind, truncate(l.Text, 70))
	}
	return tw.Flush()
}

func lessonWhy(m *metabolome.Metabolome, id types.ID, out io.Writer) error {
	e, ok := m.Get(id)
	if !ok {
		return fmt.Errorf("no lesson %s", id)
	}
	l := e.Lesson
	say(out, "lesson %s · %s · %s · %s\n  %s\n", l.ID, l.State, l.Kind, scopeString(l.Scope), l.Text)
	if l.Rationale != "" {
		say(out, "  why proposed: %s\n", l.Rationale)
	}
	say(out, "  learned from %d episode(s), provenance %s, origin %s\n", len(l.Sources), l.Provenance, orDash(l.Origin))
	say(out, "\nhistory:\n")
	for _, ev := range e.Events {
		from := string(ev.From)
		if from == "" {
			from = "∅"
		}
		say(out, "  %s  %s → %s  %s\n", ev.At.UTC().Format(time.DateTime), from, ev.To, ev.Reason)
	}
	if len(e.Evidence) > 0 {
		say(out, "\nevidence:\n")
		for _, ev := range e.Evidence {
			say(out, "  %s  %s %s by %s over %d episodes\n", ev.At.UTC().Format(time.DateTime), ev.Kind, ev.Result,
				orDash(ev.Judge), len(ev.Episodes))
			say(out, "    relevant: %d wins, %d losses, %d ties (p_help %.3f)\n", ev.Wins, ev.Losses, ev.Ties, ev.PHelp)
			say(out, "    past successes: %d worse, %d better (p_harm %.3f)\n", ev.Regressions, ev.RegressionWins, ev.PHarm)
			if ev.NoiseCompared > 0 {
				say(out, "    noise floor: %d of %d baseline-vs-baseline pairs judged decisively\n", ev.NoiseDecisive, ev.NoiseCompared)
			}
		}
	}
	return nil
}

func cmdNutrientEval(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("nutrient-eval", flag.ContinueOnError)
	fs.SetOutput(out)
	seeds := fs.Int("seeds", 5, "simulated histories to plant lessons in")
	count := fs.Int("count", 400, "episodes per history")
	outDir := fs.String("out", "", "directory to write nutrient.json (optional)")
	judgeName := fs.String("judge", "heuristic", "judge to evaluate: heuristic, ollama, deepseek, jev")
	judgeModel := fs.String("judge-model", "", "model for an ollama, deepseek or jev judge")
	allowRemote := fs.Bool("allow-remote", false, "allow a remote judge to receive simulated plans")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rem := remote{allowed: *allowRemote, out: out}
	jd, err := pickJudge(*judgeName, *judgeModel, rem)
	if err != nil {
		return err
	}
	if isRemote(*judgeName) {
		rem.notice("simulated comparisons (no real data) go to %s", jd.Name())
	}
	var ss []uint64
	for i := 1; i <= *seeds; i++ {
		ss = append(ss, uint64(i))
	}
	r, err := bench.EvaluateNutrient(ctx, ss, *count, verify.Config{}, jd)
	if err != nil {
		return err
	}
	say(out, "nutrient test on simulated Helix · %d histories × %d episodes · judge %s\n\n", r.Seeds, r.Count, r.Judge)
	say(out, "  wrong lessons rejected    %d / %d  (%s)   exit bar: ≥ 90%%\n", r.WrongRejected, r.WrongTotal, pct(r.WrongRejectRate))
	say(out, "  correct lessons accepted  %d / %d  (%s)\n", r.CorrectAccepted, r.CorrectTotal, pct(r.CorrectAcceptRate))
	say(out, "  inconclusive              %d\n\n", r.Inconclusive)
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	seen := map[string]bool{}
	for _, row := range r.Rows {
		if seen[row.Text] {
			continue
		}
		seen[row.Text] = true
		agree := 0
		total := 0
		for _, x := range r.Rows {
			if x.Text == row.Text {
				total++
				if (x.Good && x.Result == types.ResultPass) || (!x.Good && x.Result == types.ResultFail) {
					agree++
				}
			}
		}
		kind := "wrong"
		if row.Good {
			kind = "good"
		}
		say(tw, "  %s\t%d/%d right\t%s\t%s\n", kind, agree, total, truncate(row.Text, 60), row.Why)
		for _, x := range r.Rows {
			wrong := (x.Good && x.Result != types.ResultPass) || (!x.Good && x.Result != types.ResultFail)
			if x.Text == row.Text && wrong {
				say(tw, "  \t\t  ↳ seed %d: %s\t\n", x.Seed, truncate(x.Summary, 110))
			}
		}
	}
	_ = tw.Flush()
	if *outDir != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			return err
		}
		if err := writeFile(filepath.Join(*outDir, "nutrient.json"), func(w io.Writer) error { return writeJSON(w, r) }); err != nil {
			return err
		}
		say(out, "\nwrote %s/nutrient.json\n", *outDir)
	}
	return nil
}

func say(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
