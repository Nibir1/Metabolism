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
| every test, race detector on | `race` | the engine: journal durability and crash safety, append-only triggers, retrieval, the recall evaluation (signal *and* noise), the immune screen, digest validation, the nutrient test (including the 20-trial noisy-host test, D-018), the credit ledger (including the simulator acceptance test, D-020), the judges and remote adapters against fakes, the privacy guard, the CLI and its opt-in rules |
| golden files match | `check-wire` | Helix and the engine agree on every contract (recording v1 and v2, replay, delivery) byte for byte |
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

Keys go in your environment, never in a command line or a chat:

```bash
export DEEPSEEK_API_KEY=...   # in your shell profile or a secrets manager
export TYPESAFE_API_KEY=...
```

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

`bin/metabolism doctor` shows the delivery file and both preferences.

Share the output of `credit-eval`, `/lessons`, and `credit`.

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
