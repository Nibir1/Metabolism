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
