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
| every test, race detector on | `race` | the engine: journal durability and crash safety, append-only triggers, retrieval, the recall evaluation (signal *and* noise), the privacy guard, the CLI |
| golden files match | `check-wire` | Helix and the engine agree on the wire format byte for byte |
| Helix on the integration branch | `helix-branch` | you are testing the right Helix |
| Helix vet + integration tests + full suite + lint | `helix-verify` | the recorder, the agent hooks, decline counting, the undo link, the daemon wiring, and that nothing else in Helix broke |
| every CLI command end to end | `demo` | sim → ingest → inspect → curves → recall → recall-eval → doctor all run on real files |

It ends with `✔ verify passed`. Anything else, share the output.

`make demo` on its own is the quickest way to *see* each command's output.
It works in `.demo/`, so nothing touches your real journal.

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
bin/metabolism sim -journal .demo/live.db -count 300 && bin/metabolism digest -journal .demo/live.db -reflector deepseek -allow-remote
```

Then `bin/metabolism verify -journal .demo/live.db` tests what it proposed.

**Local instead of remote:** start Ollama (`ollama serve`), pull a model (for
example `ollama pull qwen3:8b`), and use `-reflector ollama` or
`-judge ollama`. No opt-in is needed for a local model.

Share the output of `nutrient-eval` with each judge.

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
