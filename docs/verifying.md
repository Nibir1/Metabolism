# Verifying that it works

Two levels: the automated checks, which need nothing but this checkout and
`../Helix`, and a manual run with real Helix. Every command below is a
Makefile target; `make` alone lists them.

## 1. Automated: `make verify`

```bash
make verify
```

This runs, in order:

| Step | Target | Proves |
|---|---|---|
| gofmt, vet, golangci-lint | `lint` | the code is clean |
| every test, race detector on | `race` | the engine: journal durability and crash safety, append-only triggers, retrieval, the recall evaluation (signal *and* noise), the immune screen, digest validation, the nutrient test (including the 20-trial noisy-host test, D-018), the credit ledger (including the simulator acceptance test, D-020), versions, rollback and forget-with-proof (including the raw-bytes check, D-028), the judges and remote adapters against fakes, the privacy guard, the CLI and its opt-in rules |
| golden files match | `check-wire` | Helix and the engine agree on every contract (recording wire v1 to v5, replay v1 and v2, delivery v1) byte for byte |
| Helix on the integration branch | `helix-branch` | you are testing the right Helix |
| Helix vet + integration tests + full suite + lint | `helix-verify` | the recorder, the agent hooks, decline counting, refused steps, untrusted planner steps, exit codes on every turn, the undo link, the daemon wiring (pagers off, a fresh working directory per request), `helix replay`, lesson delivery and `/lessons`, and that nothing else in Helix broke |
| the CLI end to end | `demo` | sim → ingest → inspect → curves → recall → recall-eval → digest → verify → lessons → credit → export → doctor all run on real files |

It ends with `✔ verify passed`. Anything else, share the output.

`make demo` on its own is the quickest way to *see* each command's output.
It works in `.demo/cli/`, which it deletes and rebuilds each time, so nothing
touches your real journal. Keep live-run journals in `.runs/` (ignored by git,
never touched by a make target).

## 2. Manual: real Helix

```bash
make helix-howto
```

prints the steps below.

1. **Build Helix from the integration branch.**

   ```bash
   make helix-build
   ```

   This produces `bin/helix`. Your installed Helix, if any, is untouched.

2. **Start it:** `./bin/helix`. It uses your normal `~/.helix` configuration.
   On a machine where Helix has never run, it walks you through first-run
   setup (a provider and key).

3. **Turn recording on**, inside Helix:

   ```
   /metabolism on
   ```

   Expect `✔ metabolism  on — recording planner turns to ~/.helix/metabolism/episodes.ndjson`.

4. **Ask a few things that reach the planner.** Slash commands and commands
   you type directly as shell lines are deliberately not recorded. For example:
   - `what files are in this directory?`
   - `find the file that defines the main function`
   - `create a file notes.txt that says hello, then show it`: it will ask to
     confirm the write. **Decline once**, so a `declined` outcome is recorded.
   - Ask the same question twice within ten minutes, so a `repeated` outcome
     is recorded on the first.
   - Optional: in a scratch git repo, `commit my changes`, then `undo that`,
     so an `undone` outcome is recorded against the committing turn.

5. **Check status**, inside Helix:

   ```
   /metabolism
   ```

   Expect `RECORDS` to be above 0.

6. **Exit Helix**, then:

   ```bash
   make doctor
   ```

   Expect `✔ preference on`, `✔ episode file … N episodes`, and a prompt to
   ingest.

7. **Ingest and report:**

   ```bash
   make helix-report
   ```

   This ingests into `~/.metabolism/journal.db`, then prints the weekly
   curves, the recall evaluation and the last 10 episodes.

**Share the output of steps 5, 6 and 7.** From it we can check that:
- every recorded turn has an outcome (`labelled` 100%);
- the declined, repeated and undone outcomes landed on the right turns;
- the run ends match what Helix printed on screen;
- no secret you typed appears in the recorded requests or commands.

To look at one turn in full: `bin/metabolism inspect -id <ID>`, using an ID
from the `inspect` list.

A few turns aren't enough for `recall-eval`: it needs at least 20 earlier
episodes before it evaluates anything, and says so. That is expected until
you have used Helix for a while.

## 2b. Live models

Keys go in `.env` in the repository root (gitignored), never in a command
line or a chat:

```
DEEPSEEK_API_KEY=...
TYPESAFE_API_KEY=...
```

`bin/metabolism` reads it at startup, only if it is owner-only
(`chmod 600 .env`), and only these variables (plus `OLLAMA_HOST` and
`METABOLISM_JOURNAL`). A variable already exported in your shell wins.

**Check a judge against ground truth.** On the simulator the right answer for
every planted lesson is known, so this measures how often a real judge's
verdicts lead the nutrient test to the right decision. It sends only simulated
data. About 150 comparisons per seed:

```bash
make build && bin/metabolism nutrient-eval -seeds 1 -judge jev -allow-remote
```

```bash
bin/metabolism nutrient-eval -seeds 1 -judge deepseek -allow-remote
```

The heuristic judge scores 100% here by construction. A model judge scores
lower; anything at or above 90% for wrong-lesson rejection meets the Phase 2
bar on the simulator.

**Reflect with a real model on simulated episodes:**

```bash
bin/metabolism sim -journal .runs/live.db -count 300 && bin/metabolism digest -journal .runs/live.db -reflector deepseek -allow-remote
```

Then `bin/metabolism verify -journal .runs/live.db` tests what it proposed.

**Local instead of remote:** start Ollama (`ollama serve`), pull a model (for
example `ollama pull qwen3:8b`), and use `-reflector ollama` or
`-judge ollama`. No opt-in is needed for a local model.

Share the output of `nutrient-eval` with each judge.

## 2c. Real Helix replay

Build Helix from the integration branch first: `make helix-build` (it puts
`bin/helix` here). Every replay below is a planner call to Helix's configured
provider, so `-allow-remote` is required.

**See one replay.** Pick an episode from `bin/metabolism inspect -journal …`
and a lesson you write yourself:

```bash
bin/metabolism replay -journal ~/.metabolism/journal.db -episode <ID> -lesson "When a search finds nothing, say so instead of repeating it." -host helix -helix-bin bin/helix -replay-dir ~ -judge jev -allow-remote
```

It prints the plan without the lesson, the plan with it, and the judge's
verdict. Nothing is recorded.

By default a replay plays up to 7 rounds (`-rounds 7`): the first plan, then
the follow-up rounds a live turn would play (3 after a file lookup, up to 6
when each round reads new parts of files; D-022). Between rounds it runs only
read-only file steps (`list`, `glob`, `grep`, `read`). The printed plans show
the round of each step, which reads ran, and how the replay ended.
`-rounds 1` replays the first plan only and runs nothing. More rounds mean
more planner calls (up to four per replay), and excerpts of files under
`-replay-dir` go to the provider, as they did in the original turn.

`-replay-dir` is where Helix plans the replay. Use the directory the episode
happened in: `~` for daemon episodes, the repository for interactive ones.
Episodes keep only a hashed project key, so the engine cannot know it.

**The full loop on your recorded episodes.** After a while with
`/metabolism on`:

```bash
make helix-ingest && bin/metabolism digest -reflector deepseek -allow-remote
```

```bash
bin/metabolism verify -host helix -helix-bin bin/helix -replay-dir ~ -judge jev -allow-remote -save comparisons.ndjson
```

```bash
bin/metabolism label -from comparisons.ndjson
```

```bash
bin/metabolism label -from comparisons.ndjson -score
```

`label` shows each judged comparison **blind**: the request, how it went, and
plans A and B, never which one had the lesson or what the judge said. It saves
as you go; `q` quits and the same command resumes. `-redo` starts a fresh pass
and keeps the earlier labels. `-score` reports agreement and Cohen's κ, and
refuses a sample where at least 90% of the labels are one letter. The Phase 2
bar is ≥ 80% agreement, and fewer than 30 labels is an early reading.

**Cost of a run.** Each replay is one planner call or more. Under D-018 every
episode's baseline is replayed twice (once per run, shared by all lessons) for
the A/A noise floor. Each lesson then adds one replay per episode it is tested
on (up to 8 relevant and 6 past successes) and one judge call per pair whose
plans differ.

**A second judge on the same comparisons** (no replays; the saved view is
sent to the second judge):

```bash
bin/metabolism crosscheck -from comparisons.ndjson -judge deepseek -allow-remote
```

It reports agreement and κ overall, on relevant episodes, on past successes
and per lesson. Two models agreeing is weaker evidence than human labels.

## 2d. Delivering lessons (Phase 3)

**Check the ledger on the simulator first.** It plants every lesson the
simulator knows the truth about, delivers them by coin flip over thousands of
turns, and settles every 100 turns:

```bash
bin/metabolism credit-eval
```

Expect every harmful lesson eliminated, no lesson that changes no outcome
moved, and most helpful lessons accepted. It sends nothing anywhere.

**Deliver to real Helix.** Once a lesson has passed `verify` (it is in trial):

```bash
bin/metabolism export
```

This writes `~/.helix/metabolism/lessons.json`. Then, inside Helix:

```
/lessons on
/lessons
```

`/lessons on` refuses while recording is off: a lesson delivered on an
unrecorded turn could never be credited. `/lessons` lists what is delivered
and what applies in the current directory. Ask a few planner questions. With
`/debug` on, each turn prints how many lessons it delivered and withheld.
Then:

```bash
make helix-ingest && bin/metabolism credit
```

Expect each trial lesson's delivered and withheld turn counts to grow,
about half each. A decision needs at least 20 delivered and 10 withheld
turns that are an hour old, and a Bayes factor of 30 (accept) or 300
(eliminate), so it takes many turns. `credit -apply` records a decision;
`export` again so Helix delivers the new state.

To try a forget: `/lessons forget <id> <reason>` in Helix, then
`make helix-ingest`. Expect `feedback: 1 lesson(s) forgotten in Helix
eliminated`, and the reason in `bin/metabolism lessons -id <id>`.

`/lessons forget --hard <id> <reason>` deletes instead (D-028). The lesson
leaves `lessons.json` at once; after `make helix-ingest`, expect `1 deleted
with proof`, `bin/metabolism versions` lists the forget record (`by helix`),
and `bin/metabolism forget -verify` reports nothing left.

Versions (D-028), on any journal:

- `bin/metabolism snapshot -note "why" NAME` names the current version.
- `bin/metabolism versions` lists the named versions and the forget records.
- `bin/metabolism diff NAME` shows which lessons changed state since.
- `bin/metabolism rollback NAME` shows what a rollback would do, and
  `-apply` records it. Lessons come back to trial, never straight to
  accepted. A rollback is undone by rolling back to the `before-rollback-…`
  version it names.
- `bin/metabolism forget -match TEXT` lists what would be deleted. Add
  `-apply -reason "..."` to delete it from the journal, the recording and
  the delivery file.

`bin/metabolism doctor` shows the delivery file and both preferences.

Share the output of `credit-eval`, `/lessons`, and `credit`.

## 2e. Automated days (D-023)

Build first (`make helix-build`), and make sure no Helix daemon or
interactive session is running. Try it with a small day:

```bash
bin/metabolism auto -allow-remote -plan-only -requests 20
```

`-plan-only` only writes the day's requests
(`.runs/auto/days/<time>/requests.json`) so you can read what the synthetic
user would ask. Then a real day:

```bash
bin/metabolism auto -allow-remote -requests 20
```

It clones the projects into `.runs/auto/workspace`, turns Helix recording on
(and wake listening off) for the run, starts the daemon, sends the requests,
stops it, restores your config byte for byte, and runs ingest, digest,
verify and credit on `.runs/auto/journal.db`. Each run adds a line to
`.runs/auto/report.md`; everything else is in the run's `days/` folder.
Lessons are delivered only after `-baseline` episodes (default 300) are
recorded. The nutrient test is skipped until the journal holds
`-verify-after` episodes (default 100): on fewer, replay noise swamps any
effect. After that each run tests up to `-verify-max` lessons (default 5),
never-tested ones first. A full day is `-requests 100`.

Days run off-peak only. DeepSeek bills double from 01:00 to 04:00 and from
06:00 to 10:00 UTC on weekdays (03:00–06:00 and 08:00–12:00 CEST), and a day
spends most of its money there. `auto` (and `-resume`) refuses to start
when peak applies now or within 2 hours, and names the next safe start;
`-allow-peak` pays the peak price instead. `-plan-only` is exempt.

Phase 5a (D-031): once days are recorded with Helix's wire v3,
`bin/metabolism decisions -journal .runs/auto/journal.db` shows where the
model calls went (by kind, and the planner's by cause) and each candidate
decision's share against the 15% stop bar.

Planning mode (D-031): `bin/metabolism auto -allow-remote -thinking-flip`
plans about half the turns with DeepSeek's thinking off, by coin flip per
turn. The same `decisions` command then ends with the planning-mode section:
reasoning's share of planner cost, and the two arms' success and cost per
turn. It needs a Helix built from `feat/metabolism-integration` with wire v4
(`make helix-build`). Measured over 683 turns and judged negative on
2026-10-11 (habits.md), so the flip is off by default in `make collect`.

**The automated days have done their job (2026-10-11).** Everything they
could measure is measured; what is open needs real use, which a synthetic
user cannot stand in for (the rubric's C1, C8 and C9 ask for real use).
`DAYS=n` stays available.

The synthetic user never starts a request with a command name: Helix runs
such a line as a shell command and it never reaches the planner (6 of 20
requests on the first day).

What it sends: the projects' file outlines and README openings to DeepSeek
(the request writer); each request through Helix to its provider; lessons to
DeepSeek (digest) and comparisons to Jev (verify).

## 2f. Gathering everything: `make collect` (D-032)

One target gathers the evidence for every open exit criterion and ends with
a scorecard. Run it yourself, when you are at the machine; it never
schedules anything.

```bash
make collect                # gather and measure only: no API cost
make collect DAYS=1         # plus one automated day
make collect DAYS=1 FLIP=1  # with the planning-mode coin flip
```

In order, `scripts/collect.sh`:

1. builds Helix (`feat/metabolism-integration`) and Metabolism;
2. ingests your own Helix recording into the real journal
   (`~/.metabolism/journal.db`), apart from the automated one. Once the
   lessons-off baseline is in (≥ 300 episodes over ≥ 21 days), it also
   digests, verifies, credits and exports lessons from your own use (paid,
   off-peak only; `LEARN=0` skips it), and you turn them on with
   `/lessons on`;
3. runs `DAYS` automated days. Your own recording is set aside for each
   run and put back after it (`auto -keep-own-recording`), even if the run
   is interrupted;
4. writes every measurement to `.runs/collect/<time>/`;
5. adds each day's judged comparisons to the labelling pool
   (`.runs/labels/`, shuffled) and, while fewer than 50 are labelled,
   offers the blind labelling session (`LABEL=0` skips it); when the labels
   changed, it measures the judge in use against them (D-033, D-035);
6. writes `summary.md` beside them: each criterion met, short, waiting,
   manual or not built, with its bar, the evidence and what moves it.

Share `summary.md` in your review session (with a person or your coding
assistant); code changes are made from it.

The paid or long steps are opt-in per run (all off-peak only):

| Setting | What it gathers | Once first |
|---|---|---|
| `DAYS=n` | automated Helix days (planning-mode flip with `FLIP=1`) | |
| `BENCH=n` | the second host on n benchmark exercises with a local model: the lessons-off baseline, then learning (Phase 0, 8). Stopped since 2026-10-13: no model lands between the floor and the ceiling (D-037) | `make bench-setup`, `make trainer-setup` |
| `PLANTED=n` | n planted wrong lessons, tested in a copy of the automated journal (Phase 2) | |
| `TRANSFER=n` | n tested lessons measured on projects they were not learned in (Phase 6) | |
| `TRAIN=1` | consolidate the second host's credited lessons into a small local model, about an hour (Phase 7) | `make trainer-setup` |

`make bench-setup` and `make trainer-setup` download the benchmark (git)
and MLX's trainer (pip); run them yourself, once. The trainer environment
and the local models were deleted on 2026-10-13 to free space;
`make trainer-setup` puts the environment back, and the first local run
downloads its model again.

## 3. Once there are weeks of data

```bash
make helix-baseline
```

writes the real baseline to `bench/results/baseline-helix/` (curves and
recall skill) for committing. That is the Phase 0 and Phase 1 exit evidence.

## The cgo note

On this machine, Go 1.27's external linker can't read the installed Command
Line Tools SDK (`tapi error: malformed file … unknown architecture
arm64e.x1-macos`). That affects anything built with cgo, including plain
`go build` of Helix on `main`. It is not caused by the integration.

The Makefile therefore builds and tests Helix with `CGO_ENABLED=0` (override
with `HELIX_CGO=1`). Race tests need cgo, so Helix's race tests run in its CI,
not locally. Updating the Command Line Tools usually fixes it:

```bash
sudo rm -rf /Library/Developer/CommandLineTools && xcode-select --install
```
