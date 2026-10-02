# Benchmark results

Each directory is one measured curve: `curve.json` (full precision),
`curve.csv` (for plotting) and `curve.txt` (the printed table).

| Directory | What it is | Real? |
|---|---|---|
| `baseline-helixsim/` | Simulated Helix, no lessons, 1000 episodes over 8 weeks (`make baseline-sim`, seed 1). `curve.*` is the learning curve; `recall.*` is the episodic-recall evaluation | **No.** Synthetic |

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
  real plans depends on real Helix replays and a real judge (Phase 2c).
- **The real Helix baseline doesn't exist yet.** It needs Helix to emit
  episodes (see `docs/helix-integration.md`) and a few weeks of real use with
  learning off. It will be committed as `baseline-helix/`.
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
