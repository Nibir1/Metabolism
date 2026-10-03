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

**Status (2026-10-02): built in all three stages.** The first real lesson has
passed the nutrient test on real Helix replays. Two exit criteria stay open
and need real data (below). Delivered in three stages.

**2a: engine core (built 2026-10-01).**
- [x] Lessons, lifecycle events and evidence as journal records. The
      event-sourced `metabolome` (D-014) refuses a move to trial without
      passing evidence for that lesson.
- [x] `immune`: a deterministic screen run before any lesson is recorded. It
      rejects lessons that teach getting around confirmations, sandbox,
      firewall or hooks, or acting without consent, or disguising a request
      to get past a refusal, or that recommend `--no-verify`, `--force`,
      `sudo`, `chmod 777` or `/permissions auto`, or read as prompt
      injections. It also rejects gate and procedure kinds, bad lengths, and
      fence-breaking markup. Provenance is the least trusted of a lesson's
      sources.
- [x] `digest`: reflector output is untrusted. Sources must be episodes it
      was shown, scopes must match a source, it must pass the screen, and it
      must not duplicate a known lesson (eliminated ones included). Recorded
      as candidate, or quarantined when learned from untrusted content.
- [x] `verify`, the nutrient test (D-015; its decision rule is now D-018):
      relevant and regression replays,
      judged, with evidence recorded and the lesson moved to trial,
      eliminated, or nowhere when inconclusive. Baseline replays are cached.
- [x] `judge.Heuristic`: deterministic (fewer failed steps, then fewer
      model calls).
- [x] The lifecycle state machine: candidate → quarantined → trial (plus
      eliminated).
- [x] CLI: `digest`, `verify`, `lessons [-id]`, `nutrient-eval`.

**2b: real reflectors and judges (built 2026-10-01).**
- [x] `ports.Model`; `adapters/remote/llm` (Ollama, OpenAI-compatible
      DeepSeek), tested against fake servers, key never in an error.
- [x] `llmreflect`: a bounded, fenced, failure-first prompt; strict parsing
      that drops malformed items; untrusted transcripts never shown. `digest`
      and `immune` still validate everything.
- [x] `judge.LLM` and `adapters/remote/jev` (one atomic `Choice` through
      typesafe-go, clean under `typesafe-lint`). Both see A/B in a hashed
      order (tested with an always-"a" judge).
- [x] CLI opt-in (D-016): `digest -reflector ollama|deepseek`,
      `verify -judge heuristic|ollama|deepseek|jev`, and `nutrient-eval -judge`,
      which measures a judge against the simulator's ground truth.
- [x] Live runs (2026-10-01): Jev and DeepSeek judges each 11/11 on the
      simulator; DeepSeek reflector found 5 of 6 planted failure patterns
      (`bench/results/README.md`). Fixes found by running live: realistic
      simulator plans, identical replays tie without a judge, plain-words
      presentation, judge thinking disabled, one retry on empty answers, and
      the simulator refusing to judge lessons it cannot model.

**2c: real Helix replay (built 2026-10-01).**
- [x] Helix (`feat/metabolism-integration`): `helix replay`. The plan-only
      pipeline is shared with `/plan`; the fenced lessons block is bounded,
      sanitised and fence-proof; no session or recording; an always-refuse
      prompter; stdout carries the protocol only. Tested: a planned `touch`
      never runs; hostile lessons cannot close the fence. `/plan` now uses
      the live turn's prompt builder.
- [x] `adapters/remote/helix`: one process per run, timeouts kill a hung
      replay, broken streams restart it, Helix episodes only. Tested against
      a fake `helix replay` and the shared golden file.
- [x] CLI: `verify -host helix` (needs `-allow-remote` and a model judge),
      `verify -save` for labelling, `replay` (one episode, both plans, the
      verdict), `label` (blind, resumable) and `label -score` (agreement and
      Cohen's κ). D-017.
- [x] First live run (2026-10-02, 42 daemon-driven episodes, DeepSeek
      reflector, Jev judge). All 4 real lessons were eliminated by noise
      "regressions" even though the comparisons showed them working. That led
      to D-018: an A/A noise floor, tests calibrated to it, and asymmetric
      bars (permanent elimination needs p ≤ 0.02). Accepted by a 20-trial
      noisy-host test.
- [x] Re-run under D-018 (2026-10-02). The first real lesson passed to
      trial: "don't call todo add with no task list loaded", p_help 0.011
      against measured noise r = 0.25, no harm. Two were inconclusive. 34
      judged comparisons were saved.
- [x] Judge cross-check (Jev against DeepSeek, `metabolism crosscheck`):
      κ 0.19 overall, but 7/9 agreement on the lesson that passed, and
      chance only on noise pairs (`bench/results/README.md`).
- [x] Helix fixes found by the daemon-driven session (Helix branch): the
      daemon no longer waits on a pager; each daemon request starts in the
      daemon's home directory (a `cd` used to carry into later requests);
      shell exit codes are kept on every turn, so a failing command is
      recorded as failed outside `/agentic` too
      ([helix-integration.md](helix-integration.md), "Driven session").
- [ ] Blind-label a human sample for judge agreement. Two labelling passes
      were not usable (uniform answers); `label -score` now refuses those.
      Best done on real-usage comparisons, where the labeller has context.

Exit criteria:

- [x] Every trial lesson has with/without ablation evidence (the C6 bar).
      Enforced structurally by `metabolome.Move`, and tested.
- [ ] Planted wrong lessons are rejected by the nutrient test at ≥ 90%.
  - [x] On the simulator: 25/25 wrong rejected, 30/30 correct accepted
        (`bench/results/baseline-helixsim/nutrient.*`). **This proves the
        test's logic, not its power.** The simulator's host knows which
        lesson fixes which failure.
  - [ ] On real Helix replays (2c). There are no planted wrong lessons in
        real data yet; the noisy-host test (D-018) is the proxy so far.
- [ ] Judge agreement with a human-labelled sample is ≥ 80%, or the judge is
      replaced (2c).

## Phase 3: Distribution and the credit ledger (about 4 weeks). C4, C9

**Status (2026-10-02): built.** The go/no-go gate waits on weeks of real use
with lessons on, after the real baseline (Phase 0) exists.

Deliverables:

- [x] Delivery (D-019): `metabolism export` writes trial and accepted lessons
      to `~/.helix/metabolism/lessons.json` (delivery file v1, golden file in
      both repos). Helix selects by scope and budget and puts the lessons in
      live planner turns, in the same fenced `<learned_lessons>` block replay
      uses. Delivery needs `/lessons on` and recording on.
- [x] Exposure logging and trial randomisation (D-020): a coin per lesson per
      turn, recorded as `exposure` and `withheld` (recording wire v2). Trial
      p = 0.5; accepted p = 0.9 (a 10% holdout).
- [x] The credit ledger (`ledger/`, D-020): delivered against withheld turns,
      within each lesson's current state, decided by Bayes factor (accept at
      30, eliminate at 300). `metabolism credit` reports; `-apply` records
      the evidence and moves the lesson.
- [x] Lifecycle trial → accepted, refused by `metabolome.Move` without
      passing credit evidence.
- [x] A report comparing turns with and without each lesson: `metabolism
      credit` shows success and model calls per turn for both arms.
- [x] Helix `/lessons`: list (and what applies here), `on|off`, `why <id>`
      (evidence and credit), `forget <id> <reason>` (stops at once; the
      engine eliminates it at the next ingest). `metabolism doctor` reports
      delivery.
- [x] On the simulator (`credit-eval`, `TestCreditLedgerOnTheSimulator`):
      every harmful lesson eliminated, no lesson that changes no outcome
      moved, most helpful lessons accepted within 3000 turns. A p-value rule
      moved 9 of 30 neutral lessons under frequent settling, which is why the
      bar is a Bayes factor.
- [x] Multi-round replay (D-021): lessons about follow-up rounds (rereading,
      re-grepping until the budget runs out) were untestable on first plans.
      Replays now play the rounds a live turn would, running only read-only
      file steps between them. Not yet measured on real episodes.
- [ ] Backlog: run automated days on a schedule. Not on the current machine
      (a work PC): days are run by hand while the user is at it, so nothing
      changes the Helix setup unattended.
- [ ] Not in this phase: `/lessons forget --hard`, snapshot and rollback
      (Phase 4); per-request relevance in delivery.

Known issues carried in from Phase 2 (they distort the data Phase 3 measures):

- Helix: plain English starting with a command word ("summarise", "find")
  is sent to the shell and never reaches the planner, so it is not recorded.
- Helix: the daemon offers the planner the task-list tool with no task list
  loaded.
- Helix: `helix remote manual` leaves the daemon's microphone stream open
  (listening by default is an owner decision, Helix threat model V2b).
- Metabolism: repeat detection compares a request only with the immediately
  preceding turn.

**GO / NO-GO gate.** Compare with the Phase 0 baseline after at least 4 weeks
of real Helix use plus the public benchmark:

- **GO** if lessons beat the baseline with confidence on at least 2 core
  metrics and regress none.
- **NO-GO:** stop adding features. Write up the negative result ("verified
  lessons vs in-context learning, measured"), diagnose, and decide whether to
  pivot.

Either way, **publish**: a blog post or arXiv note with the curves, method and code.

The randomised delivery gives the gate a second comparison besides the
Phase 0 baseline: turns with lessons against turns where the coin withheld
them, over the same weeks.

**Two gates (D-023).** A synthetic-use gate runs on `metabolism auto`: a
model writes each day's requests and real Helix runs them, first with lessons
off (the baseline), then on. It gives evidence in days instead of weeks, and
it is labelled synthetic everywhere. The real-use gate still needs the user's
own weeks of Helix, and only it can support a rubric claim.

Exit criteria:

- [ ] Real credit: lessons decided by live Helix turns (accepted or
      eliminated), and the D-020 bars rechecked against what real effects
      look like.
- [ ] Go/no-go decision recorded in [decisions.md](decisions.md).
- [ ] Publication out.

## Phase 4: Elimination and the immune system (about 4 weeks). C5

**Status (2026-10-03): started (D-025)**, while Phase 3's data-gated criteria
stay open. 4a, the poisoning suite, first: design in
[poisoning.md](poisoning.md). Its bars hold on the suite (0% persistence, 0%
attack success, 128 cases). 4b, drift: accepted lessons are judged on a
recent window too, and credit counts the turns that bear on a lesson
(D-026). On the simulator 10 of 10 drifted lessons were retired, none
wrongly.

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
- [ ] Drifted lessons retired within N episodes (N set in Phase 3 data). On
  the simulator: 10/10 within 1100 to 1900 turns, 0/15 false retirements
  (`drift-eval`, D-026). N on real data stays open.
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
