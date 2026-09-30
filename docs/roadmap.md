# Roadmap: phase after phase to 100% Digestive AI

Each phase names the rubric criteria it moves (see [vision.md](vision.md)),
what it delivers, and **exit criteria** that must hold before the next phase
starts. Durations are rough estimates for one engineer working part-time.
Update this file as phases close, and record decisions in
[decisions.md](decisions.md).

Helix is the first host throughout (see [helix-integration.md](helix-integration.md)).

```
Phase  0 Foundations & measurement    ── baseline curves before any learning
Phase  1 Ingestion & episodic memory  ── C1, C3
Phase  2 Digestion & nutrient test    ── C2, C6
Phase  3 Distribution & credit ledger ── C4, C9      ◀── GO / NO-GO + first publication
Phase  4 Elimination & immune system  ── C5
Phase  5 Habit compilation (S2 → S1)  ── C6, C9 (cost)
Phase  6 Procedural tier: skills      ── C4 (transfer)
Phase  7 Parametric tier              ── C7
Phase  8 Endurance & second host      ── C8, reuse
Phase  9 The 100% audit               ── all nine, with evidence
```

---

## Phase 0: Foundations and measurement (about 2–3 weeks)

**Why first:** without a baseline, "it learned" is unfalsifiable. Every later
phase is judged against the curves produced here.

Deliverables:

- Go module `github.com/Nibir1/metabolism`, with `types/` and `ports/` as in
  [architecture.md](architecture.md).
- The SQLite `Journal` adapter: append-only, versioned schema, migration tests.
- The `bench/` harness:
  - a replay runner;
  - the metric definitions (below);
  - a runner for one public benchmark (choose τ-bench, ContinualSkillBench or
    LongMemEval-V2; see open tasks in [research.md](research.md)).
- A Helix test double (`hosts/helixsim`) with a fixture set of realistic episodes.
- **Baseline curves** for Helix with no lessons, and for the public benchmark with
  plain in-context learning. ContinualSkillBench found plain in-context
  learning is the baseline to beat.

Core metrics (Helix):

- planner calls per finished task
- step-budget exhaustion rate
- failed-step rate
- `/undo` rate
- declined-confirmation rate
- estimated tokens per finished task

Exit criteria:

- [ ] Baseline curves produced and committed (`bench/results/baseline-*`).
  - [x] Synthetic: `baseline-helixsim`. Proves the harness works; it is not a baseline.
  - [ ] Real Helix: Helix now records episodes (`/metabolism on`, branch
        `feat/metabolism-integration`) and `metabolism ingest` reads them (D-012,
        D-013). Waiting on weeks of real use with learning off, then
        `metabolism curves` → `bench/results/baseline-helix/`.
  - [ ] Public benchmark: pick one (research.md, open tasks), then write its runner.
- [x] The Journal survives a crash test (kill mid-write, reopen, no loss).
- [x] CI runs `go test -race ./...` and lint (`.github/workflows/ci.yml`).

**Status (2026-09-30): engine side done.** Built so far: `types/`, `ports/`,
`adapters/journal/sqlite` (append-only via triggers, versioned migrations,
0600/0700, crash test), `hosts/helixsim` (scenarios drawn from real Helix
failures), `bench/` (metrics, weekly curves, replay runner), `cmd/metabolism`
(`sim`, `curves`, `inspect`), and CI. Phase 0 stays open until the real Helix
baseline and the public benchmark are in place.

## Phase 1: Ingestion and episodic memory (about 3 weeks). C1, C3

Deliverables:

- [x] Helix emits Episodes and Outcomes: interactive shell and daemon
      (built in Phase 0 under D-012; daemon wired in Phase 1).
- [x] Outcome labelling, including **late outcomes**: undo, declined
      confirmations, and a repeated request.
- [x] Provenance and trust levels on every episode. Redaction reuses Helix's
      journal plus credential masking.
- [x] An episodic retrieval baseline: similar past episodes as cases
      (Memento-style). `episodic/` does time-aware BM25 with scope and recency
      boosts, rebuilt from the journal, with untrusted transcripts excluded by
      default. `metabolism recall`.
- [x] `metabolism inspect`: summary, filters (`-end`, `-scope`) and a full
      episode view (`-id`) with outcomes.

Exit criteria:

- [ ] ≥ 90% of Helix agentic runs have an outcome label. It is met by
      construction, since every recorded turn gets a host outcome, and
      `labelled` in `metabolism curves` measures it. Tick it once real data
      confirms it.
- [x] Nothing leaves the machine unless the network opt-in is set (test
      enforced). `internal/privacyguard` fails if any engine package outside
      `adapters/remote/` imports networking, or if any dependency pulls in an
      HTTP/TLS client. Helix's `internal/metabolism` has the same test.
- [ ] Episodic retrieval measured against the baseline. It is allowed to show
      no gain; that is the data point.
  - [x] The measurement: `metabolism recall-eval`, time-ordered Brier skill
        against the base rate. It was checked to find signal where there is
        some (simulator, skill 0.96) and none where there is none (noise test).
  - [ ] The real number: run on the Helix baseline once it exists.

**Status (2026-09-30): built. The exit criteria are waiting on real Helix data.**
Rubric: C1 (ingest) is met in code; the evidence bar, outcome coverage ≥ 90% on
real episodes, is pending data. C3 (persists) is met: the journal is durable,
and the index is derived from it (`TestRebuildFromJournalAfterRestart`).

## Phase 2: Digestion and the nutrient test (about 4 weeks). C2, C6

Deliverables:

- Reflector adapters (Ollama by default, DeepSeek opt-in) that produce candidate
  lessons linked to source episodes.
- Lesson kinds: fact, pattern, preference. Scopes: machine, project, global.
- The **nutrient test**, with replay through Helix plan-only mode. Judges: a Jev
  `Score` through typesafe-go, plus a deterministic checker where possible.
- The lifecycle state machine: candidate → quarantined → trial.
- The evidence record for every decision.

Exit criteria:

- [ ] Every trial lesson has with/without ablation evidence (the C6 bar).
- [ ] Planted wrong lessons are rejected by the nutrient test at ≥ 90%.
- [ ] Judge agreement with a human-labelled sample is ≥ 80%, or the judge is replaced.

## Phase 3: Distribution and the credit ledger (about 4 weeks). C4, C9

Deliverables:

- The Selector, which delivers lessons within Helix's budget as a new
  `<learned_lessons authority="data-only">` block.
- Exposure logging, trial randomisation, and Beta-Bernoulli credit per lesson.
- The lifecycle moves on to trial → accepted.
- A learning-curve report: `metabolism bench curves`.
- The `/lessons` command in Helix.

**GO / NO-GO gate.** Compare with the Phase 0 baseline after at least 4 weeks
of real Helix use plus the public benchmark:

- **GO** if lessons beat the baseline with confidence on at least 2 core
  metrics and regress none.
- **NO-GO:** stop adding features. Write up the negative result ("verified
  lessons vs in-context learning, measured"), diagnose, and decide whether to
  pivot.

Either way, **publish**: a blog post or arXiv note with the curves, method and code.

Exit criteria:

- [ ] Go/no-go decision recorded in [decisions.md](decisions.md).
- [ ] Publication out.

## Phase 4: Elimination and the immune system (about 4 weeks). C5

Deliverables:

- Decay by credit, contradiction detection, and supersession with reasons.
- A quarantine tier for low-trust provenance, with extra verification.
- A poisoning test suite in CI, modelled on MemSecBench: persistence, attack
  success and repair.
- The versioned metabolome: snapshots, rollback, and forget-with-proof (hard
  delete plus an audit record).
- Drift tests: a lesson that stops holding (for example, a repo switches build
  tools) is retired.

Exit criteria:

- [ ] Poison persistence ≤ 5% and end-to-end attack success ≤ 2% on the suite.
- [ ] Drifted lessons retired within N episodes (N set in Phase 3 data).
- [ ] No regression on the Phase 3 curves.

## Phase 5: Habit compilation, System 2 → System 1 (about 4 weeks). C6, C9

**Before starting:** finish the literature check for novelty (research.md
open task).

Deliverables:

- Detection of recurring decision shapes in episodes.
- A Jev question-set generator, gated by typesafe-go's analyzers
  (`atomicquestion`, `jaggededge`, `confidencecheck`).
- Nutrient testing of gates against past decisions; fallback below the
  confidence threshold.
- In Helix, compiled gates are **restrict-only** and require the user's
  approval to become active.

Exit criteria:

- [ ] At least one compiled gate cuts slow-model calls for its decision by ≥ 50% at
      equal or better accuracy.
- [ ] Cost per finished task falls on the curves.

## Phase 6: Procedural tier and skills (about 4–6 weeks). C4

Deliverables:

- Procedure lessons: multi-step workflows extracted from successful episodes
  (Voyager / AWM / LifeMem-style), verified by replay.
- Transfer tests: a lesson learned in repo A helps in repo B; a lesson learned on
  one model helps on another.

Exit criteria:

- [ ] Measured transfer on held-out projects or tasks (the C4 bar).

## Phase 7: Parametric tier (about 6–8 weeks). C7

Deliverables:

- Promotion rule: only stable, high-credit, high-volume lessons.
- Dataset builder from verified episodes and lessons.
- A trainer port. First adapter: a hosted LoRA API (Together or Fireworks) on a
  small open model; alternatively Tinker through a sidecar. **This is the only
  place non-Go code is allowed.**
- A regression suite of old episodes to measure forgetting, required before any
  adapter is used.
- Adapter serving through a local runtime (Ollama or llama.cpp) as a Helix
  provider.

Exit criteria:

- [ ] Retention ≥ 95% on the old-task suite, and gains ≥ the text-lesson
      version at lower tokens per task (the C7 bar).
- [ ] Adapter rollback works.

## Phase 8: Endurance and a second host (about 3+ months, mostly elapsed time). C8

Deliverables:

- At least 3 months of continuous real Helix use with the engine on.
- Tenure analysis: does performance hold or improve at weeks 3, 9 and 12? (See
  "Ground Truth First" in research.md.)
- **A second host** with no engine changes, to prove the core is domain-agnostic.
  Candidates: a support-ticket simulator, or a coding agent on a public benchmark.
- The Metabolic Benchmark packaged and published.

Exit criteria:

- [ ] Stable or improving curves over ≥ 3 months (the C8 bar).
- [ ] The second host runs with zero changes to `engine/`.

## Phase 9: The 100% audit

- Score Metabolism on C1–C9 against the evidence bars in
  [vision.md](vision.md), ideally with an outside reviewer.
- Publish the scorecard, including any criterion still below 1.
- Only then use the words "100% Digestive AI".

---

## Standing rules for every phase

- Tests are written with the code. Race detector on. Poisoning suite from Phase 4 on.
- Each phase ends with a curve comparison against the previous phase. **A
  phase that regresses a core metric isn't done.**
- Negative results are recorded in [decisions.md](decisions.md), not hidden.
- Refresh [research.md](research.md) at Phases 3, 5 and 7. The field moves fast.
