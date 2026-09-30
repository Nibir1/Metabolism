# Benchmark results

Each directory is one measured curve: `curve.json` (full precision),
`curve.csv` (for plotting) and `curve.txt` (the printed table).

| Directory | What it is | Real? |
|---|---|---|
| `baseline-helixsim/` | Simulated Helix, no lessons, 1000 episodes over 8 weeks (`make baseline-sim`, seed 1) | **No.** Synthetic |

## Read this before quoting any number

- **`baseline-helixsim` is synthetic.** It shows the harness works end to end:
  journal → load → metrics → weekly windows. The scenario mix is invented,
  so its rates say nothing about real Helix. Its curve is flat by
  construction, because the simulator does not learn. That is what a
  no-lesson baseline should look like.
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
