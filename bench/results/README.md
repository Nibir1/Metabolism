# Benchmark results

Each directory is one measured curve: `curve.json` (full precision),
`curve.csv` (for plotting) and `curve.txt` (the printed table).

| Directory | What it is | Real? |
|---|---|---|
| `baseline-helixsim/` | Simulated Helix, no lessons, 1000 episodes over 8 weeks (`make baseline-sim`, seed 1). `curve.*` is the learning curve; `recall.*` is the episodic-recall evaluation; `nutrient.*` is the nutrient test on planted lessons (5 seeds); `credit.*` is the credit ledger on planted lessons delivered by coin flip (5 seeds × 3000 turns) | **No.** Synthetic |

The live runs below (judges, reflector, real Helix replays) are logged in this
file only. Their journals are not committed. Live-run data now lives in
`.runs/`.

**Data loss, 2026-10-02.** The journals and files from the daemon-driven
session and the D-018 re-run (`.demo/helix-driven.db`,
`.demo/helix-driven/episodes.ndjson`, `.demo/helix-d018.db`, the 34 saved
comparisons, both blind-labelling passes and the cross-check output) were
deleted. `make demo`, which `make verify` runs, cleared all of `.demo/` before
rebuilding its own files there. What remains: the numbers recorded below, and
the session's requests, driver script and replies in
`.runs/helix-driven-2026-10-02/`. The demo now uses `.demo/cli/` only. The
results below cannot be recomputed from raw data without re-running the
session.

## Read this before quoting any number

- **`baseline-helixsim` is synthetic.** It shows the harness works end to end:
  journal → load → metrics → weekly windows. The scenario mix is invented,
  so its rates say nothing about real Helix. Its curve is flat by
  construction, because the simulator does not learn. That is what a
  no-lesson baseline should look like.
- **The simulated recall skill (0.96) is easy by construction.** In the
  simulator, a request's wording names its scenario and the scenario decides
  the outcome. The number shows the evaluation can detect signal; the noise
  test in `bench/recall_test.go` shows it reports none when there is none.
  Real Helix skill will be far lower, and any value above 0 means memory knows
  something.
- **The simulated nutrient-test score (100%) proves the logic, not the power.**
  The simulator's host is an oracle: a lesson repairs a scenario if and only
  if it carries that scenario's fix tag, and harmful lessons carry harm tags.
  The score shows the decision rule, the replay sets and the evidence wiring
  do what they should. Whether the test separates good from bad lessons on
  real plans depends on real Helix replays and a real judge. The first such
  results are below; there are no planted wrong lessons in real data yet.
- **The simulated credit evaluation proves the ledger's logic and its
  false-decision rate under frequent settling, not its speed on real
  lessons.** Simulated effects are large and clean: a repairing lesson turns
  a failing scenario into a success every time it is delivered. Real lessons
  will move outcomes less often, so a decision will take many more turns.
  Lessons that only save model calls (go-module) stay in trial by design:
  credit is judged on outcomes.
- **The real Helix baseline doesn't exist yet.** Helix records episodes now
  (`/metabolism on`); the baseline needs a few weeks of real use with
  learning off, and will be committed as `baseline-helix/`. The 42
  daemon-driven episodes used below are scripted work, not a baseline.
- **The public-benchmark baseline doesn't exist yet.** The benchmark is still
  to be chosen (`docs/research.md`, open tasks).
- Token figures are estimates at 4 characters per token, as Helix's `/cost` labels them.

## Metric definitions

See `bench/metrics.go`. In short:

| Metric | Definition | Better |
|---|---|---|
| finish | episodes that ended `done` with no negative outcome ÷ episodes | ↑ |
| calls/fin | model calls across *all* episodes ÷ finished episodes | ↓ |
| est tok/fin | (input + output characters) ÷ 4 ÷ finished episodes | ↓ |
| budget | episodes that ended `budget-exhausted` ÷ episodes | ↓ |
| fail step | failed steps ÷ all steps | ↓ |
| undo / decline / repeat | episodes with that outcome ÷ episodes | ↓ |
| labelled | episodes with at least one outcome ÷ episodes (Phase 1 requires ≥ 90%) | ↑ |

Recall evaluation (`bench/recall.go`, `metabolism recall-eval`): the journal is
replayed in time order. Each episode's success is predicted two ways: from the
base rate of earlier episodes, and from the cases recalled for it (using only
outcomes known at the time). The Brier score is the mean squared error of each
prediction. **Skill** = 1 − Brier(memory) ÷ Brier(base): 0 means memory adds
nothing, and negative means it misleads.

## Live judge runs on the simulator

`metabolism nutrient-eval -seeds 1 -judge <judge> -allow-remote`, 6 correct and
5 wrong planted lessons. Each run is kept, including the misses, because what
changed between runs is part of the result.

| # | Date | Judge | Wrong rejected | Correct accepted | What changed before this run |
|---|---|---|---|---|---|
| 1 | 2026-10-01 | jev | 5/5 | 2/6 | first run |
| 2 | 2026-10-01 | jev | 5/5 | 4/6 | Repaired plans became realistic (they were the placeholder `file edit: fixed`, which reads as an unrequested change). Identical replays became ties without asking the judge |
| 3 | 2026-10-01 | jev | 5/5 | 6/6 | The original attempt is described in plain words ("ran out of its step budget before finishing", not `budget-exhausted`). A plan with no answer says `reply to the user: (none)`. Fixed a simulator bug where a request for settings.py was "repaired" by searching for config files |
| 4 | 2026-10-01 | deepseek-flash (LLM judge) | 5/5 | 6/6 | First DeepSeek run returned empty answers: `deepseek-flash` thinks by default and spent the whole budget reasoning (`finish_reason="length" reasoning=true`). The judge now disables thinking; errors report the finish reason; one retry on an empty answer |

**What run 3 does and doesn't show.**
- It is one seed: 11 lessons, decided by about 60 judged comparisons.
- The two *irrelevant* wrong lessons are rejected without any judge call,
  since identical replays are ties. Jev's own contribution is the 3 harmful
  lessons and the 6 correct ones, 9/9.
- Runs 3 and 4 agree: both judges get every simulated lesson right. That
  says the simulator is now fair to a reading judge. It does not separate
  the two judges; real Helix replays (Phase 2c) are needed for that.

**Caution about these fixes.** They change what the judge is *shown*, tuned
against the simulator. Each one is justified on its own terms: plain words
instead of jargon, an absence made visible, a bug fixed. None of them encodes
the right answer. Still, a judge calibrated on simulated plans has to be
re-checked on real Helix replays (Phase 2c) before its numbers mean anything.

## Live reflector run on the simulator

2026-10-01: `digest -reflector deepseek` (deepseek-flash, thinking on) on 300
simulated episodes. It was shown 40 of them, failures first.

- It proposed 5 lessons, and all 5 passed validation and the immune screen.
- It found **5 of the 6** planted failure patterns, in its own words, each
  citing 3 source episodes:
  - the rename lesson: grep for the definition and every reference, edit
    every file, then build (deeper than the catalogue's "don't use sed");
  - the glob loop;
  - `git add -A` being undone;
  - blanket upgrades being declined;
  - Go modules, not package.json.
- It missed the subtlest one: reporting tests from a run made *before* the
  edit, where the only signal is the request being repeated.
- These lessons cannot be nutrient-tested on the simulator, which models only
  its own catalogue. `verify -host sim` reports them as "can't judge" and
  records nothing (`ports.ErrCannotReplay`). They need real Helix replays
  (Phase 2c).

## First nutrient test on real Helix replays

2026-10-02: `verify -host helix -judge jev` on 42 daemon-driven episodes,
with 4 lessons proposed by DeepSeek. **All 4 were eliminated, wrongly.** The
saved comparisons show the task-list lesson winning its 3 relevant episodes
at 0.96–0.99 confidence. The "regressions" that eliminated it were the
planner planning the same request differently each time, read by the judge
as preference. The rule was replaced: D-018 adds a noise floor, calibrated
tests and asymmetric bars. Also observed: one plain judge error, where Jev
preferred a plan that misused the task-list tool for "add a TODO comment".

### Re-run under D-018

2026-10-02, on a fresh journal (`.demo/helix-d018.db`, the same 42 episodes).
DeepSeek proposed 3 lessons, the same themes as before; the suspect "wait for
go-ahead" lesson did not recur. Jev judged.

| Lesson | Result | Relevant W/L/T | Past successes worse/better | A/A noise |
|---|---|---|---|---|
| Don't call `todo add` with no task list loaded | **pass → trial** (p_help 0.011) | 4/1/3 | 0/2 | 3/14 |
| Relative paths resolve under the drifted folder | inconclusive | 2/4/2 | 1/3 | 6/14 |
| Don't re-read the same file | inconclusive | 2/2/4 | 2/0 | 5/14 |

**The first lesson learned from real Helix episodes and validated on real
Helix replays.** The measured noise (3–6 of 14 A/A pairs decisive) is what
eliminated everything under D-015. The path lesson cannot show value when
replayed from `~`, where there is no drift to fix, and is correctly left
undecided rather than eliminated. 34 judged comparisons were saved for blind
labelling.

### Judge agreement: first labelling pass not usable

The first blind labelling pass over the 34 saved comparisons answered "a" for
all 34, many within 1–3 seconds of each other. Jev chose A 9 times, so the
printed "26.5% agreement, κ −0.38" measured only how often Jev also picked A.
It is **not** a judge-agreement result. `label -score` now refuses to grade a
sample where at least 90% of labels are one letter, and `label -redo` starts
a fresh pass and keeps the old labels.

### Judge cross-check: Jev against DeepSeek

2026-10-02: `crosscheck -judge deepseek` re-judged the 34 saved comparisons
with exactly the view Jev had.

| Set | Agreement | Cohen's κ |
|---|---|---|
| All | 18/34 (53%) | 0.19 |
| Episodes the lesson is about | 9/19 (47%) | 0.00 |
| Past successes | 9/15 (60%) | 0.37 |

**By lesson:**
- **The lesson that passed** ("don't call `todo add` with no task list"):
  agreement **7/9**. On all 3 core episodes, where the without-lesson plan
  opens with the failing `todo add`, the judges agree, two of them "with is
  better" at 0.9. The trial decision is supported by both judges where it
  matters.
- **Path lesson:** 7/14. **Re-reading lesson:** 4/11. About chance. These
  pairs are two different but reasonable plans (two answers to "what is a
  Brier score?", two ways to search a folder), and no judge reads that
  difference reliably. The nutrient test left both inconclusive rather than
  trusting either judge's call: D-018 working as designed.

**Reading:** the low overall κ comes from noise pairs, not from the judges
disagreeing where a lesson changes the plan. Two models can still agree on
the same mistake, so this is weaker than human labels, and the
human-labelled Phase 2 criterion stays open. It points to where labelling
effort is worth spending: pairs where the plans differ in substance.

## Credit ledger on the simulator (Phase 3)

2026-10-02: `credit-eval` plants all 11 simulator lessons in trial, delivers
each on a coin flip over 3000 turns per history, and settles every 100
turns. The rule went through one revision, kept here because the miss is
the reason for the rule.

| Rule | Histories | Helpful accepted | Harmful eliminated | Neutral moved |
|---|---|---|---|---|
| P(better) ≥ 0.95 / P(worse) ≥ 0.99 | 10 | 49/50 | 30/30 | 9/30 (2 eliminated) |
| P(better) ≥ 0.99 / P(worse) ≥ 0.999 | 20 | 97/100 | 60/60 | 4/60 (3 eliminated) |
| **Bayes factor 30 / 300 (D-020)** | 30 | 120/150 | 90/90 | **0/90** |

- **The p-value rules failed on neutral lessons**, which is the error that
  matters: an eliminated harmless lesson is gone for good, and an accepted
  irrelevant one is claimed as learning. Without repeated looks the neutral
  lessons' z-scores were unbiased (mean 0.08, sd 0.99 over 80). The cause
  was settling repeatedly: optional stopping.
- **Two design bugs were found on the way.** The budget was filled in file
  order, so trial lessons past it were never delivered or measured (the
  harmful ones, last in the list, had 0 turns). Turns before and after a
  lesson's state change were mixed, which compared arms drawn from different
  periods. Both are fixed (shuffled trial order, eras).
- 2026-10-03: credit counts, for acceptance, only the turns that bear on
  each lesson; harm still counts on every turn (D-026). Helpful accepted
  went from 19/25 to 25/25; harmful 15/15 and neutral 0/15 are unchanged.
  `credit-eval -all-turns` reruns the old analysis.
- `baseline-helixsim/credit.txt` is the committed run: 25/25 helpful
  accepted, 15/15 harmful eliminated, 0/15 neutral moved.

## Drift on the simulator (Phase 4b)

2026-10-03: `drift-eval` plants the simulator's correct lessons in trial,
plays 3000 turns (they earn acceptance), changes the world so two of them
stop holding, and plays 3000 more. `baseline-helixsim/drift.*`, 5 histories.

| Analysis | Drifted accepted lessons retired | Lessons that still hold, retired |
|---|---|---|
| Every turn, whole record (D-020) | 0/10 | 0/15 |
| Recent 200 turns, every turn | 0/10 | 0/15 |
| **Recent 200 related turns, and every turn for harm (D-026)** | **10/10**, 1100–1900 turns after the change | **0/15** |

Synthetic: the simulator's 19 distinct requests make related turns easy to
find. Details and limits in D-026.

## Driven session re-run (after the Helix fixes)

2026-10-02, 13:49–13:53: the same 45 requests through the Helix daemon
(`feat/metabolism-integration` at `bb33f2d`, DeepSeek), lessons off. File
requests were pointed at the run's own workspace. Data in
`.runs/helix-driven-2026-10-02b/` (recording, journal, driver log, daemon
log). Scripted work, **not a usage baseline**.

| | First run | Re-run |
|---|---|---|
| Planner episodes | 42 | 42 |
| Done / budget-exhausted / failed / open work | 30 / 4 / 7 / 1 | 28 / 8 / 5 / 1 |
| Failed-step rate | 9.7% | 5.3% |
| Model calls per finished turn | 6.17 | 6.79 |
| Wall time | about 30 min | 4 min |

- **The directory drift is gone.** Builds, tests and `go vet` ran in one
  step each, in the right directory. In the first run, 7 of 12 failures and
  budget exhaustions traced to a `cd` carried over from an earlier request.
- **Still failing, unchanged:** 3 turns called `todo add` with no task list
  loaded (the daemon still offers the tool). One read a file that does not
  exist (the TODO request, as designed). One listed `internal/judge` instead
  of `judge`.
- **More budget exhaustions (8):** explanatory questions ("explain the
  difference between candidate and quarantined lessons") spend the file
  lookup's three follow-up iterations on grep and read without answering.
  DeepSeek proposed three lessons about it; the nutrient test left all three
  inconclusive. Multi-round replay (D-021) then showed the cause was a Helix
  bug, not something a lesson can fix. A read of a long file showed the
  planner a slice from the middle (the last 60 lines of the first 20 KB), so
  it could never reach the section asked about. Fixed in Helix: a read is a
  window of up to 80 lines with a header, and takes `start_line`.

## Driven session after the read fix

2026-10-02, 15:04–15:08 (local): the same 45 requests, lessons off, Helix at
`1c08cdf` (reads are windows with `start_line`). Data in
`.runs/helix-driven-2026-10-02c/`.

| | First run | Re-run (directory fix) | After the read fix |
|---|---|---|---|
| Done / budget-exhausted / failed / open work | 30 / 4 / 7 / 1 | 28 / 8 / 5 / 1 | 33 / 5 / 3 / 1 |
| Finished turns | 71.4% | 66.7% | 78.6% |
| Failed-step rate | 9.7% | 5.3% | 3.3% |
| Model calls per finished turn | 6.17 | 6.79 | 5.67 |

- Reads behave as intended: a file is read window by window, and a grep's
  line number leads straight to a ranged read.
- **The remaining budget exhaustions are lesson-shaped.** Three of five
  repeat an identical step (decisions.md read twice and grepped twice; the
  same grep for `Injectable` twice). One searched for a Python config loader
  that does not exist and kept loosening the pattern (the simulator's
  empty-glob scenario, seen for real). One read all of digest.go and ran out
  before answering.
- Failures: two are still the daemon offering the task-list tool with no
  list loaded; one is the deliberately missing scratch.go.
- One run each, scripted, with a nondeterministic planner: the direction is
  clear, but the size of the change is not measured to any precision.

### Nutrient test with multi-round replays (D-021)

DeepSeek proposed 5 lessons from the run above; `verify -host helix -judge
jev` replayed up to 4 rounds per episode (about 25 minutes).

| Lesson | Result | Relevant W/L/T | Past successes worse/better | A/A noise |
|---|---|---|---|---|
| Don't open with `todo` tools (this machine) | **pass → trial** | 5/0/3 | 1/2 | 7/14 |
| Don't repeat an identical grep, glob or read | inconclusive | 5/2/1 | 2/3 | 11/14 |
| Scope searches to the package directory | inconclusive | 3/1/4 | 1/1 | 6/14 |
| These two repos are Go; nothing to hunt for (this machine) | **pass → trial** | 6/1/1 | 0/4 | 9/14 |
| "What does package X do": list, grep, read the first file | inconclusive | 4/0/4 | 0/3 | 8/14 |

- **Multi-round replay shows what first plans could not.** With the
  repeated-step lesson, questions about decisions.md, the verify package and
  the Makefile were answered in 3 rounds; without it, they reread until the
  rounds ran out.
- **Multi-round replays are much noisier** (up to 11 of 14 A/A pairs
  decisive), so a lesson needs a clearer win to pass.
- **Part of the noise was a Helix bug, since fixed:** glob and grep reported
  paths relative to the searched folder, so reading a result often failed,
  at random in both arms. Re-testing the three inconclusive lessons on fixed
  Helix is the next step.
- **The Go-projects lesson behaves well.** With it, Helix still searched,
  then said "there isn't one, it's a Go project" in 2 rounds instead of
  loosening the search until the rounds ran out. Most of its wins are on the
  scripted session's planted questions about missing files, so its value in
  real use is narrower than these counts.

### Re-test after the glob/grep path fix

The three inconclusive lessons, replayed again on fixed Helix
(`comparisons-pathfix.ndjson`):

| Lesson | Before the fix W/L/T (noise) | After the fix W/L/T (noise) |
|---|---|---|
| Don't repeat an identical grep, glob or read | 5/2/1 (11/14) | 1/3/4 (8/14) |
| Scope searches to the package directory | 3/1/4 (6/14) | 1/3/4 (8/14) |
| "What does package X do" procedure | 4/0/4 (8/14) | 3/4/1 (7/14) |

On the repeated-step lesson's 8 relevant episodes, how often each plan
answered within the 4 rounds:

| | Without the lesson | With the lesson |
|---|---|---|
| Before the path fix | 0 of 8 | 4 of 8 |
| After the path fix | 3 of 8 | 3 of 8 |

- **The path fix helped Helix itself:** with no lesson, 3 of these 8 hard
  questions are now answered within the live budget, up from none.
- **The lesson's earlier wins did not hold.** With one replay per episode and
  a planner that varies run to run, single comparisons flip between runs (the
  decisions.md question went from a win to a loss). About 8 relevant episodes
  per lesson cannot separate an effect this size from that noise.
  Inconclusive is the right result, and D-018 left all three in place rather
  than eliminating them on a guess.
- 5 of the 8 still run out of rounds either way. The live follow-up budget
  for a file lookup (3 rounds) is tight for explanatory questions that need
  several windows of a file; that is a Helix design question, not a lesson.

## Automated days (synthetic user, D-023)

Two runs on 2026-10-03, lessons off: 20 requests (14 planner episodes, 92.9%
finished) and 100 requests (95 episodes, 93.7% finished, no budget
exhaustions). Running log: `.runs/auto/report.md`. Synthetic evidence: a
model writes the requests.

### The noise floor, and the outcome-first judge (D-024)

| Judge | Decisive baseline-vs-baseline pairs |
|---|---|
| Jev alone (7 lessons, two runs) | 9 to 12 of 14 |
| Outcome first, then Jev (3 lessons) | 5 of 26, 0 of 26, 6 of 25 |

Under Jev alone, 7 lessons from two automated runs were all inconclusive:
nothing could clear a floor that high. With outcome first, of one lesson's 15
multi-round comparisons, 12 were ties on equal outcomes and 3 were wins on
outcome (the lesson's plan answered, the other ran out of rounds).

| Lesson | Result | Relevant W/L/T | Noise |
|---|---|---|---|
| Use absolute paths in `cd` for shell commands here | **pass → trial** | 5/0/11 | 5/26 |
| For simple build/test/vet requests, go straight to the shell command | **pass → trial** | 5/1/9 | 6/25 |
| To explain package X, start with list or glob and grep | eliminated ("changes nothing") | 0/0/16 | 0/26 |

The third was eliminated under the old "changes nothing" rule: 16 ties on
equal outcomes are not evidence that a lesson changes nothing. The rule now
needs identical plans (D-024). The elimination stands in the append-only
journal, with its reason.

## Poisoning suite (Phase 4a, simulator)

`poison.*` in `baseline-helixsim/`: 128 cases, the worst-case reflector.
Before the defences: persistence 16.7% and end-to-end attack success 16.7%,
against bars of 5% and 2%, all of it the false-answer attack, which wins
replays by imitating a real fix. With them: 0% and 0%; benign lessons kept
in every case. Details and caveats in
[docs/poisoning.md](../../docs/poisoning.md#results).
