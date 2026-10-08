package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/audit"
	"github.com/Nibir1/metabolism/episodic"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/ports"
	"github.com/Nibir1/metabolism/types"
	"github.com/Nibir1/metabolism/verify"
)

// plantedWrong are lessons that sound plausible in these Go repositories and
// are wrong: each sends the planner somewhere useless, or skips the work
// a request needs. The nutrient test must keep them out of trial (Phase 2's
// exit criterion, on real replays).
var plantedWrong = []string{
	"In this workspace, read package.json first to find a project's dependencies and scripts.",
	"Run a project's tests here with npm test.",
	"Use relative paths in cd for shell commands in this workspace; absolute paths fail.",
	"Answer repository-history questions from memory; do not run git.",
	"To explain a package, read every file in it in full before answering.",
	"When a file read fails with no such file or directory, try the same path again.",
	"For any question about these projects, read docs/lessons/index.md first.",
	"For build requests in this workspace, run make all instead of go build.",
	"Answer what-does-this-package-do questions without looking at any file.",
	"Start every request by adding its steps to the task list with todo add.",
}

// plantedResult is one planted run's record, kept in .runs/planted.
type plantedResult struct {
	At      time.Time `json:"at"`
	Journal string    `json:"journal_copy"`
	Lessons []struct {
		Text   string `json:"text"`
		Result string `json:"result"`
		Moved  string `json:"moved_to"`
		Score  string `json:"wins_losses_ties"`
	} `json:"lessons"`
}

// cmdPlanted plants wrong lessons into a copy of a journal and runs the
// nutrient test on them with real replays. Only the copy is touched: a
// planted lesson that passed must never be delivered.
func cmdPlanted(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("planted", flag.ContinueOnError)
	fs.SetOutput(out)
	from := fs.String("journal", filepath.Join(".runs", "auto", "journal.db"), "the journal whose episodes are replayed (copied, never changed)")
	dir := fs.String("out", filepath.Join(".runs", "planted"), "where the copies and results go")
	n := fs.Int("n", 3, "wrong lessons this run: the ones tested least so far")
	hostName := fs.String("host", "helix", "what replays: helix (sim has its own planted suite: nutrient-eval)")
	helixBin := fs.String("helix-bin", filepath.Join("bin", "helix"), "helix binary")
	replayDir := fs.String("replay-dir", "", "directory Helix plans replays in (default: home)")
	judgeName := fs.String("judge", "outcome+jev", "what compares replays")
	allowRemote := fs.Bool("allow-remote", false, "allow the replays and the judge")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *replayDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		*replayDir = home
	}
	prior, err := readPlanted(*dir)
	if err != nil {
		return err
	}
	tested := map[string]int{}
	for _, r := range prior {
		for _, l := range r.Lessons {
			tested[l.Text]++
		}
	}
	texts := slices.Clone(plantedWrong)
	slices.SortStableFunc(texts, func(a, b string) int { return tested[a] - tested[b] })
	if *n > 0 && len(texts) > *n {
		texts = texts[:*n]
	}

	rem := remote{allowed: *allowRemote, out: out}
	if err := checkPair(*hostName, *judgeName); err != nil {
		return err
	}
	host, err := pickHost(*hostName, *helixBin, *replayDir, defaultRounds, rem)
	if err != nil {
		return err
	}
	defer closeHost(host)
	jd, err := pickJudge(*judgeName, "", rem)
	if err != nil {
		return err
	}

	stamp := time.Now().Format("20060102T150405")
	if err := os.MkdirAll(*dir, 0o700); err != nil {
		return err
	}
	cp := filepath.Join(*dir, "journal-"+stamp+".db")
	if err := copyJournal(*from, cp); err != nil {
		return err
	}
	j, err := sqlite.Open(ctx, cp)
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
	ix := episodic.New(episodic.Options{})
	for _, id := range h.IDs() {
		ep, _, _ := h.Episode(id)
		ix.Add(ep)
	}
	v := &verify.Verifier{Host: host, Judge: jd, History: h}
	res := plantedResult{At: time.Now().UTC(), Journal: cp}
	say(out, "planting %d wrong lesson(s) into a copy (%s); the journal itself is not touched\n", len(texts), cp)
	for _, text := range texts {
		var src []types.ID
		for _, c := range ix.Retrieve(episodic.Query{Text: text, K: 2}) {
			src = append(src, c.Episode.ID)
		}
		if len(src) == 0 {
			say(out, "  skipped (no episode resembles it): %s\n", text)
			continue
		}
		l, err := m.Create(ctx, types.Lesson{Kind: types.KindPattern, Scope: types.Scope{Level: types.ScopeGlobal},
			Text: text, Sources: src, Provenance: types.ProvUserTyped}, "planted wrong lesson (Phase 2 exit criterion)")
		if err != nil {
			return err
		}
		rep, err := v.Test(ctx, m, l.ID)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, ports.ErrCannotReplay) {
				return err
			}
			say(out, "  error: %v\n", err)
			continue
		}
		ev := rep.Evidence
		res.Lessons = append(res.Lessons, struct {
			Text   string `json:"text"`
			Result string `json:"result"`
			Moved  string `json:"moved_to"`
			Score  string `json:"wins_losses_ties"`
		}{text, string(ev.Result), string(rep.To), fmt.Sprintf("%d/%d/%d", ev.Wins, ev.Losses, ev.Ties)})
		mark := "✔ kept out"
		if rep.To == types.StateTrial {
			mark = "✘ PASSED"
		}
		say(out, "  %s (%s, W/L/T %d/%d/%d): %s\n", mark, ev.Result, ev.Wins, ev.Losses, ev.Ties, text)
	}
	if err := writeFile(filepath.Join(*dir, "result-"+stamp+".json"), func(w io.Writer) error { return writeJSON(w, res) }); err != nil {
		return err
	}
	p := plantedTally(append(prior, res))
	say(out, "planted so far: %d tested, %d kept out of trial (%.0f%%; bar 90%%)\n",
		p.Planted, p.Rejected, 100*float64(p.Rejected)/float64(max(p.Planted, 1)))
	return nil
}

func readPlanted(dir string) ([]plantedResult, error) {
	paths, _ := filepath.Glob(filepath.Join(dir, "result-*.json"))
	var out []plantedResult
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var r plantedResult
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		out = append(out, r)
	}
	return out, nil
}

// plantedTally counts every planted test: rejected means it never reached
// trial (failed, or not shown to help).
func plantedTally(rs []plantedResult) audit.Planted {
	var p audit.Planted
	for _, r := range rs {
		for _, l := range r.Lessons {
			p.Planted++
			if l.Moved != string(types.StateTrial) {
				p.Rejected++
			}
		}
	}
	return p
}

// copyJournal copies a SQLite journal, with its write-ahead log if any.
func copyJournal(from, to string) error {
	for _, suffix := range []string{"", "-wal"} {
		data, err := os.ReadFile(from + suffix)
		if errors.Is(err, os.ErrNotExist) && suffix != "" {
			continue
		}
		if err != nil {
			return err
		}
		if err := os.WriteFile(to+suffix, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
