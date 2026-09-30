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
	"path/filepath"
	"strings"
	"time"

	"github.com/Nibir1/metabolism/adapters/journal/sqlite"
	"github.com/Nibir1/metabolism/adapters/ndjson"
	"github.com/Nibir1/metabolism/types"
)

// cmdDoctor reports what the Helix → engine pipeline looks like right now, and
// what to do next. It only reads: it never creates a journal or a file, and it
// always exits 0, because a missing piece is a finding, not a failure.
func cmdDoctor(ctx context.Context, args []string, out io.Writer) error {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(out)
	path := flags.String("journal", defaultJournal(), "journal file")
	from := flags.String("from", defaultHelixFile(), "Helix's recorded episode file")
	helixCfg := flags.String("helix-config", defaultHelixConfig(), "Helix's config.json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	d := &doctor{out: out}

	d.section("Helix recording")
	pref, prefErr := helixPreference(*helixCfg)
	switch {
	case errors.Is(prefErr, fs.ErrNotExist):
		d.idle("preference", "no Helix config at "+*helixCfg)
		d.next("run Helix once (make helix-build && ./bin/helix), then /metabolism on")
	case prefErr != nil:
		d.bad("preference", fmt.Sprintf("cannot read %s: %v", *helixCfg, prefErr))
	case pref:
		d.good("preference", "metabolism_record is on")
	default:
		d.idle("preference", "off: nothing is being recorded")
		d.next("in Helix, run: /metabolism on")
	}

	fileIDs, fst := scanHostFile(*from)
	switch {
	case len(fst.files) == 0 && pref:
		d.idle("episode file", "none yet at "+*from)
		d.next("use Helix: the file appears after the first turn that reaches the planner")
	case len(fst.files) == 0:
		d.idle("episode file", "none at "+*from)
	default:
		d.good("episode file", fmt.Sprintf("%d file(s), %d records (%d episodes, %d outcomes), %s",
			len(fst.files), fst.lines, fst.episodes, fst.outcomes, bytesString(fst.bytes)))
		if !fst.last.IsZero() {
			d.info("last record", fst.last.Local().Format(time.DateTime)+" ("+ago(fst.last)+")")
		}
		if fst.badLines > 0 {
			d.warn("unreadable", fmt.Sprintf("%d line(s) are not wire-v%d records; ingest will skip and report them",
				fst.badLines, ndjson.WireVersion))
		}
	}

	d.section("Engine journal")
	if _, err := os.Stat(*path); err != nil {
		d.idle("journal", "none yet at "+*path)
		if len(fileIDs) > 0 {
			d.next("make helix-ingest")
		}
		return nil
	}
	j, err := sqlite.Open(ctx, *path)
	if err != nil {
		d.bad("journal", err.Error())
		return nil
	}
	defer func() { _ = j.Close() }()
	if err := j.IntegrityCheck(ctx); err != nil {
		d.bad("integrity", err.Error())
	}

	byHost := map[string]int{}
	outcomes, labelled := 0, map[types.ID]bool{}
	have := map[types.ID]bool{}
	for r, err := range j.Scan(ctx, types.JournalQuery{Kinds: []types.RecordKind{types.RecordEpisode, types.RecordOutcome}}) {
		if err != nil {
			d.bad("scan", err.Error())
			return nil
		}
		if r.Episode != nil {
			byHost[r.Episode.Host]++
			have[r.Episode.ID] = true
		} else {
			outcomes++
			labelled[r.Outcome.EpisodeID] = true
		}
	}
	total := len(have)
	var hosts []string
	for h, n := range byHost {
		hosts = append(hosts, fmt.Sprintf("%s %d", h, n))
	}
	d.good("journal", fmt.Sprintf("%s · %d episodes (%s) · %d outcomes", *path, total, strings.Join(hosts, ", "), outcomes))
	if total > 0 {
		d.info("labelled", fmt.Sprintf("%.1f%% of episodes have an outcome (Phase 1 needs ≥ 90%%)",
			100*float64(len(labelled))/float64(total)))
	}

	if len(fileIDs) > 0 {
		in := 0
		for id := range fileIDs {
			if have[id] {
				in++
			}
		}
		if in == len(fileIDs) {
			d.good("ingested", fmt.Sprintf("all %d recorded episodes are in the journal", in))
		} else {
			d.warn("ingested", fmt.Sprintf("%d of %d recorded episodes are in the journal", in, len(fileIDs)))
			d.next("make helix-ingest")
		}
	}
	if byHost["helix"] > 0 {
		d.next("make helix-report   (curves, recall skill, recent episodes)")
	}
	return nil
}

func defaultHelixConfig() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.json"
	}
	return filepath.Join(home, ".helix", "config.json")
}

// helixPreference reads only user_preferences.metabolism_record from Helix's
// config; nothing else in that file is looked at.
func helixPreference(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var cfg struct {
		UserPreferences struct {
			MetabolismRecord bool `json:"metabolism_record"`
		} `json:"user_preferences"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false, err
	}
	return cfg.UserPreferences.MetabolismRecord, nil
}

type fileStats struct {
	files                     []string
	bytes                     int64
	lines, episodes, outcomes int
	badLines                  int
	last                      time.Time
}

// scanHostFile reads the recorded file and its generations, returning the
// episode IDs it holds.
func scanHostFile(path string) (map[types.ID]bool, fileStats) {
	var st fileStats
	ids := map[types.ID]bool{}
	files, _ := ndjson.Generations(path)
	st.files = files
	for _, f := range files {
		fh, err := os.Open(f)
		if err != nil {
			continue
		}
		if info, err := fh.Stat(); err == nil {
			st.bytes += info.Size()
		}
		sc := bufio.NewScanner(fh)
		sc.Buffer(make([]byte, 64<<10), 4<<20)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			st.lines++
			var rec struct {
				V       int    `json:"v"`
				Kind    string `json:"kind"`
				Episode *struct {
					ID        types.ID  `json:"id"`
					StartedAt time.Time `json:"started_at"`
				} `json:"episode"`
				Outcome *struct {
					At time.Time `json:"at"`
				} `json:"outcome"`
			}
			if err := json.Unmarshal([]byte(line), &rec); err != nil || rec.V != ndjson.WireVersion {
				st.badLines++
				continue
			}
			switch {
			case rec.Kind == "episode" && rec.Episode != nil:
				st.episodes++
				ids[rec.Episode.ID] = true
				st.last = later(st.last, rec.Episode.StartedAt)
			case rec.Kind == "outcome" && rec.Outcome != nil:
				st.outcomes++
				st.last = later(st.last, rec.Outcome.At)
			default:
				st.badLines++
			}
		}
		_ = fh.Close()
	}
	return ids, st
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func ago(t time.Time) string {
	d := time.Since(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%.0f h ago", d.Hours())
	}
	return fmt.Sprintf("%.0f days ago", d.Hours()/24)
}

func bytesString(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// doctor prints aligned status lines.
type doctor struct{ out io.Writer }

func (d *doctor) section(name string) { _, _ = fmt.Fprintf(d.out, "\n%s\n", name) }
func (d *doctor) line(mark, k, v string) {
	_, _ = fmt.Fprintf(d.out, "  %s %-13s %s\n", mark, k, v)
}
func (d *doctor) good(k, v string) { d.line("✔", k, v) }
func (d *doctor) warn(k, v string) { d.line("!", k, v) }
func (d *doctor) bad(k, v string)  { d.line("✘", k, v) }
func (d *doctor) idle(k, v string) { d.line("•", k, v) }
func (d *doctor) info(k, v string) { d.line(" ", k, v) }
func (d *doctor) next(v string)    { _, _ = fmt.Fprintf(d.out, "    → %s\n", v) }
