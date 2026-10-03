package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	helixhost "github.com/Nibir1/metabolism/adapters/remote/helix"
	"github.com/Nibir1/metabolism/bench"
	"github.com/Nibir1/metabolism/hosts/helixuser"
	"github.com/Nibir1/metabolism/types"
)

// cmdAuto runs one automated day (D-023): a synthetic user's requests go
// through a real Helix daemon, recorded as usual, then the engine ingests,
// reflects, tests and credits, and appends to a report.
//
// It changes the user's Helix setup only for the length of the run: the
// config and the delivery file are backed up byte for byte first and
// restored on every exit, and a run that was killed before it could restore
// is recovered by the next one before anything else happens.
func cmdAuto(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("auto", flag.ContinueOnError)
	fs.SetOutput(out)
	dir := fs.String("dir", filepath.Join(".runs", "auto"), "where the run keeps its journal, workspace, days and report")
	helixBin := fs.String("helix-bin", filepath.Join("bin", "helix"), "helix binary (make helix-build)")
	projects := fs.String("projects", "Development/Personal/Metabolism,Development/Personal/Helix",
		"repositories to work in, relative to home; each is cloned into the run's workspace, never used in place")
	requests := fs.Int("requests", 100, "requests the synthetic user sends")
	baseline := fs.Int("baseline", 300, "recorded episodes before lessons are delivered (the lessons-off baseline)")
	reflector := fs.String("reflector", "deepseek", "what proposes lessons: deepseek, ollama")
	judgeName := fs.String("judge", "jev", "what compares replays: jev, deepseek, ollama")
	verifyMax := fs.Int("verify-max", 5, "most lessons tested per run: never tested first, then the longest since a test")
	verifyAfter := fs.Int("verify-after", 100, "skip the nutrient test until the journal holds this many episodes "+
		"(on fewer, replay noise swamps any effect: day one had 10 of 14 baseline pairs decisive)")
	timeout := fs.Duration("timeout", 3*time.Minute, "longest wait for one request's reply")
	helixHome := fs.String("helix-home", "", "Helix's state directory (default ~/.helix)")
	planOnly := fs.Bool("plan-only", false, "write today's requests and stop: no Helix, no engine steps")
	allowRemote := fs.Bool("allow-remote", false, "allow the run's remote calls (see the notices)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rem := remote{allowed: *allowRemote, out: out}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if *helixHome == "" {
		*helixHome = filepath.Join(home, ".helix")
	}
	absDir, err := filepath.Abs(*dir)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	setup := &helixSetup{home: *helixHome, state: filepath.Join(absDir, "state")}
	if recovered, err := setup.recover(); err != nil {
		return fmt.Errorf("recovering the Helix setup from an interrupted run: %w", err)
	} else if recovered {
		say(out, "recovered: restored the Helix config and delivery file an interrupted run had changed\n")
	}

	model, err := pickModel(*reflector, "", rem)
	if err != nil {
		return err
	}
	rem.notice("the synthetic user sends the projects' file outlines and README openings to %s to write requests", model.Name())
	if !*planOnly {
		rem.notice("%s", "each request is a Helix turn planned by Helix's configured model provider; each replay as in verify")
	}

	day := time.Now()
	dayDir := filepath.Join(absDir, "days", day.Format("2006-01-02T150405"))
	if err := os.MkdirAll(dayDir, 0o700); err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(dayDir, "log.txt"))
	if err != nil {
		return err
	}
	defer func() { _ = log.Close() }()
	out = io.MultiWriter(out, log)

	// The workspace: fresh clones, so the synthetic user never works in the
	// real checkouts and yesterday's edits are gone.
	ws := filepath.Join(absDir, "workspace")
	var projs []helixuser.Project
	for _, p := range strings.Split(*projects, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		rel, err := cloneProject(ctx, home, p, ws)
		if err != nil {
			return err
		}
		proj, err := helixuser.BuildProject(home, rel)
		if err != nil {
			return err
		}
		projs = append(projs, proj)
	}

	journal := filepath.Join(absDir, "journal.db")
	recorded, err := countHelixEpisodes(ctx, journal)
	if err != nil {
		return err
	}
	lessonsOn := recorded >= *baseline
	phase := "baseline (lessons off)"
	if lessonsOn {
		phase = "lessons (delivered by coin flip)"
	}
	say(out, "auto run %s · %s · %d episodes recorded so far (baseline: %d)\n", day.Format(time.DateTime), phase, recorded, *baseline)

	history := filepath.Join(absDir, "requests-history.txt")
	earlier := readLines(history)
	gen := helixuser.Generator{Model: model}
	reqs, err := gen.Generate(ctx, projs, *requests, uint64(day.UnixNano()), earlier)
	if err != nil {
		return err
	}
	if err := writeJSONFile(filepath.Join(dayDir, "requests.json"), reqs); err != nil {
		return err
	}
	say(out, "wrote %d requests (%d repeated from earlier days) to %s\n", len(reqs), countRepeats(reqs), filepath.Join(dayDir, "requests.json"))
	if *planOnly {
		return nil
	}

	if err := preflight(ctx, *helixHome, *helixBin); err != nil {
		return err
	}
	if lessonsOn {
		if err := run(ctx, []string{"export", "-journal", journal, "-to", filepath.Join(dayDir, "lessons.json")}, out); err != nil {
			return err
		}
	}
	if err := setup.apply(lessonsOn, filepath.Join(dayDir, "lessons.json")); err != nil {
		_, _ = setup.recover()
		return err
	}
	restored := false
	defer func() { // backstop: a panic between apply and restore still restores
		if !restored {
			_ = setup.restore()
		}
	}()
	drove, driveErr := driveDay(ctx, *helixBin, reqs, *timeout, dayDir, out)
	restored = true
	if err := setup.restore(); err != nil {
		return fmt.Errorf("RESTORING THE HELIX SETUP FAILED (backup kept in %s): %w", setup.state, err)
	}
	say(out, "restored the Helix config and delivery file\n")
	if driveErr != nil {
		return driveErr
	}

	recording := filepath.Join(dayDir, "recording")
	if err := moveRecording(filepath.Join(*helixHome, "metabolism"), recording); err != nil {
		return err
	}
	for _, q := range reqs {
		if !q.Repeat {
			earlier = append(earlier, q.Text)
		}
	}
	if err := os.WriteFile(history, []byte(strings.Join(earlier, "\n")+"\n"), 0o600); err != nil {
		return err
	}

	steps := [][]string{
		{"ingest", "-journal", journal, "-from", filepath.Join(recording, "episodes.ndjson")},
		{"digest", "-journal", journal, "-reflector", *reflector, "-allow-remote", "-since", day.Format("2006-01-02")},
		{"verify", "-journal", journal, "-host", "helix", "-helix-bin", *helixBin, "-replay-dir", home,
			"-judge", *judgeName, "-allow-remote", "-max", fmt.Sprint(*verifyMax), "-save", filepath.Join(dayDir, "comparisons.ndjson")},
		{"credit", "-journal", journal, "-apply"},
	}
	for _, s := range steps {
		if s[0] == "verify" {
			n, err := countHelixEpisodes(ctx, journal)
			if err != nil {
				return err
			}
			if n < *verifyAfter {
				say(out, "\n━━ metabolism verify: skipped, %d episodes recorded (needs %d, -verify-after)\n", n, *verifyAfter)
				continue
			}
		}
		say(out, "\n━━ metabolism %s\n", s[0])
		if err := run(ctx, s, out); err != nil {
			say(out, "%s failed: %v (the day's recording is kept; later steps skipped)\n", s[0], err)
			break
		}
	}
	return appendReport(ctx, absDir, journal, day, phase, drove)
}

// helixSetup changes and restores the user's Helix config and delivery file.
type helixSetup struct {
	home  string // ~/.helix
	state string // the run's state directory: backups live here while a run is on
}

func (h *helixSetup) configPath() string  { return filepath.Join(h.home, "config.json") }
func (h *helixSetup) lessonsPath() string { return filepath.Join(h.home, "metabolism", "lessons.json") }
func (h *helixSetup) backup(name string) string {
	return filepath.Join(h.state, name+".backup")
}

// apply backs up the config and delivery file, then turns recording on, wake
// listening off, and lessons on or off, installing delivery when lessons are
// on.
func (h *helixSetup) apply(lessonsOn bool, delivery string) error {
	if err := os.MkdirAll(h.state, 0o700); err != nil {
		return err
	}
	cfg, err := os.ReadFile(h.configPath())
	if err != nil {
		return fmt.Errorf("read the Helix config: %w (run Helix once first)", err)
	}
	if err := os.WriteFile(h.backup("config"), cfg, 0o600); err != nil {
		return err
	}
	switch l, err := os.ReadFile(h.lessonsPath()); {
	case err == nil:
		if err := os.WriteFile(h.backup("lessons"), l, 0o600); err != nil {
			return err
		}
	case errors.Is(err, fs.ErrNotExist):
		if err := os.WriteFile(h.backup("lessons-absent"), nil, 0o600); err != nil {
			return err
		}
	default:
		return err
	}

	var doc map[string]any
	if err := json.Unmarshal(cfg, &doc); err != nil {
		return fmt.Errorf("the Helix config is not JSON: %w", err)
	}
	prefs, _ := doc["user_preferences"].(map[string]any)
	if prefs == nil {
		prefs = map[string]any{}
	}
	prefs["metabolism_record"] = true
	prefs["metabolism_lessons"] = lessonsOn
	doc["user_preferences"] = prefs
	speech, _ := doc["speech"].(map[string]any)
	if speech == nil {
		speech = map[string]any{}
	}
	wake, _ := speech["wake_word"].(map[string]any)
	if wake == nil {
		wake = map[string]any{}
	}
	wake["enabled"] = false // nobody is there to speak to it
	speech["wake_word"] = wake
	doc["speech"] = speech
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(h.configPath(), append(data, '\n'), 0o600); err != nil {
		return err
	}
	if lessonsOn {
		l, err := os.ReadFile(delivery)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(h.lessonsPath()), 0o700); err != nil {
			return err
		}
		return os.WriteFile(h.lessonsPath(), l, 0o600)
	}
	return nil
}

// restore puts back exactly what apply backed up, and removes the backups.
func (h *helixSetup) restore() error {
	cfg, err := os.ReadFile(h.backup("config"))
	if err != nil {
		return err
	}
	if err := os.WriteFile(h.configPath(), cfg, 0o600); err != nil {
		return err
	}
	if l, err := os.ReadFile(h.backup("lessons")); err == nil {
		if err := os.WriteFile(h.lessonsPath(), l, 0o600); err != nil {
			return err
		}
	} else if _, err := os.Stat(h.backup("lessons-absent")); err == nil {
		if err := os.Remove(h.lessonsPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	for _, n := range []string{"config", "lessons", "lessons-absent"} {
		_ = os.Remove(h.backup(n))
	}
	return nil
}

// recover restores a setup an earlier run changed and never put back.
func (h *helixSetup) recover() (bool, error) {
	if _, err := os.Stat(h.backup("config")); err != nil {
		return false, nil
	}
	return true, h.restore()
}

// preflight refuses to run over anything the user is doing with Helix.
func preflight(ctx context.Context, helixHome, helixBin string) error {
	bin, err := filepath.Abs(helixBin)
	if err != nil {
		return err
	}
	if helixhost.Status(ctx, bin) == nil {
		return errors.New("a Helix daemon is running; stop it first (helix remote stop): a driven run needs its own")
	}
	if data, err := os.ReadFile(filepath.Join(helixHome, "active.lock")); err == nil {
		var lock struct {
			TS int64 `json:"ts"`
		}
		if json.Unmarshal(data, &lock) == nil && time.Since(time.Unix(lock.TS, 0)) < 5*time.Minute {
			return errors.New("an interactive Helix session is active (or exited under 5 minutes ago); try again later")
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(helixHome, "metabolism", "episodes.ndjson*")); len(matches) > 0 {
		return fmt.Errorf("%s holds a recording that is not this run's (your own /metabolism use?); ingest or move it first",
			filepath.Join(helixHome, "metabolism"))
	}
	return nil
}

// cloneProject clones home/p into ws, replacing yesterday's clone, and
// returns the clone's path relative to home.
func cloneProject(ctx context.Context, home, p, ws string) (string, error) {
	src := filepath.Join(home, p)
	dst := filepath.Join(ws, filepath.Base(src))
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	if err := os.MkdirAll(ws, 0o700); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "git", "clone", "--quiet", "--local", src, dst) //nolint:gosec // local clone
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("clone %s: %v: %s", src, err, strings.TrimSpace(string(b)))
	}
	rel, err := filepath.Rel(home, dst)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("the workspace %s must be under the home directory, where the Helix daemon runs", ws)
	}
	return filepath.ToSlash(rel), nil
}

// driveDay starts the daemon, submits every request, and stops it.
func driveDay(ctx context.Context, bin string, reqs []helixuser.Request, timeout time.Duration, dayDir string, out io.Writer) (int, error) {
	dlog, err := os.Create(filepath.Join(dayDir, "daemon.log"))
	if err != nil {
		return 0, err
	}
	defer func() { _ = dlog.Close() }()
	d := &helixhost.Daemon{Command: bin, Log: dlog}
	if err := d.Start(ctx, 60*time.Second); err != nil {
		return 0, err
	}
	defer func() { _ = d.Stop(context.Background()) }()

	f, err := os.Create(filepath.Join(dayDir, "drive.ndjson"))
	if err != nil {
		return 0, err
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	sent := 0
	for i, q := range reqs {
		if ctx.Err() != nil {
			say(out, "interrupted after %d requests\n", sent)
			return sent, ctx.Err()
		}
		start := time.Now()
		reply, err := d.Submit(ctx, q.Text, timeout)
		rec := map[string]any{"n": i + 1, "kind": q.Kind, "repeat": q.Repeat, "request": q.Text,
			"secs": int(time.Since(start).Seconds()), "reply": clipReply(reply)}
		if err != nil {
			rec["error"] = err.Error()
		}
		_ = enc.Encode(rec)
		sent++
		if (i+1)%10 == 0 || i+1 == len(reqs) {
			say(out, "  sent %d/%d\n", i+1, len(reqs))
		}
	}
	return sent, d.Stop(ctx)
}

func clipReply(s string) string {
	if r := []rune(s); len(r) > 400 {
		return string(r[:399]) + "…"
	}
	return s
}

// moveRecording moves Helix's episode file (and rotated generations) into
// the day's directory, leaving Helix's own directory as it was.
func moveRecording(from, to string) error {
	matches, _ := filepath.Glob(filepath.Join(from, "episodes.ndjson*"))
	if len(matches) == 0 {
		return fmt.Errorf("no recording in %s: did any request reach Helix's planner?", from)
	}
	if err := os.MkdirAll(to, 0o700); err != nil {
		return err
	}
	for _, m := range matches {
		if err := os.Rename(m, filepath.Join(to, filepath.Base(m))); err != nil {
			return err
		}
	}
	return nil
}

func countHelixEpisodes(ctx context.Context, path string) (int, error) {
	if _, err := os.Stat(path); err != nil {
		return 0, nil
	}
	j, err := sqlite.Open(ctx, path)
	if err != nil {
		return 0, err
	}
	defer func() { _ = j.Close() }()
	n := 0
	for r, err := range j.Scan(ctx, types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode}}) {
		if err != nil {
			return 0, err
		}
		if r.Episode.Host == helixhost.HostName {
			n++
		}
	}
	return n, nil
}

// appendReport adds the day's line to the run's report.
func appendReport(ctx context.Context, dir, journal string, day time.Time, phase string, drove int) error {
	j, err := sqlite.Open(ctx, journal)
	if err != nil {
		return err
	}
	defer func() { _ = j.Close() }()
	ds, err := bench.Load(ctx, j)
	if err != nil {
		return err
	}
	var today []types.Episode
	for _, ep := range ds.Episodes {
		if !ep.StartedAt.Before(day) {
			today = append(today, ep)
		}
	}
	m := bench.Compute(today, ds.Outcomes)
	path := filepath.Join(dir, "report.md")
	_, statErr := os.Stat(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w := bufio.NewWriter(f)
	if statErr != nil {
		_, _ = fmt.Fprint(w, "# Automated Helix runs (synthetic user, D-023)\n\n"+
			"Synthetic evidence: a model writes the requests. Not a measure of real use.\n\n"+
			"| run | phase | sent | episodes | finished | budget-exhausted | failed steps | calls/finished |\n"+
			"|---|---|---|---|---|---|---|---|\n")
	}
	_, _ = fmt.Fprintf(w, "| %s | %s | %d | %d | %.1f%% | %.1f%% | %.1f%% | %.2f |\n",
		day.Format("2006-01-02 15:04"), phase, drove, m.Episodes, 100*m.FinishRate, 100*m.BudgetExhaustionRate,
		100*m.FailedStepRate, m.ModelCallsPerFinished)
	return w.Flush()
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(data), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func countRepeats(reqs []helixuser.Request) int {
	n := 0
	for _, q := range reqs {
		if q.Repeat {
			n++
		}
	}
	return n
}

func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
