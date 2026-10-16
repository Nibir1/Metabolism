# Architecture

Status (2026-10-16): **built through every tier.** Ingest, episodic memory,
digest, the nutrient test, delivery and the credit ledger (Phases 0–3);
elimination by drift, contradiction and supersession, the poisoning
defences and the versioned metabolome (Phase 4); transfer and procedures
(Phase 6); the parametric tier's trainer and forgetting check (Phase 7). Habit
compilation was measured and not built (Phase 5, D-031). The interfaces were
shaped by Helix, the first host, and a second host (the coding agent, Phase
8) runs the loop with no engine changes. Sections below say which parts are
built.

## Principles

1. **Domain-agnostic core.** The engine knows the *shape* of digestion, not
   what a shell command or support ticket is. Hosts supply meaning through
   ports.
2. **Durable first.** Every episode, lesson and state change goes to an
   append-only log before anything acts on it. A crash loses nothing.
3. **Evidence-gated.** A lesson moves forward in its lifecycle only on
   recorded evidence. The engine never promotes on a model's say-so alone.
4. **Informs, never authorises.** Lessons reach the host as *data*. The host
   decides what data may influence. The engine has no way to grant permissions.
5. **Local-first.** Every model call goes through a port. The default
   implementations are local (Ollama); hosted ones (DeepSeek, Jev) are opt-in.
6. **Observable.** Every decision carries a reason and links to its evidence.

## The loop

```
             ┌─────────────────────── host ───────────────────────┐
             │  episodes + outcomes            lessons as data    │
             └──────┬──────────────────────────────▲──────────────┘
                    │                              │
                    ▼                              │
  ┌────────┐   ┌─────────┐   ┌────────┐   ┌────────────┐   ┌────────────┐
  │ INGEST │──▶│ DIGEST  │──▶│ VERIFY │──▶│ ASSIMILATE │──▶│ DISTRIBUTE │
  │ log +  │   │ reflect │   │ nutrient│  │ tiered     │   │ select for │
  │ immune │   │ → cands │   │ test   │   │ store      │   │ context    │
  └────────┘   └─────────┘   └────────┘   └────────────┘   └─────┬──────┘
       ▲                                        ▲                │
       │            ┌───────────┐               │    usage       │
       │            │ ELIMINATE │◀──── credit ──┴── ledger ◀─────┘
       │            │ decay by  │
       │            │ value,    │       outcomes arrive later and are
       │            │ contradict│       credited to the lessons that
       │            └───────────┘       were in context
       └─────────── outcome labels (may arrive late: /undo, user feedback)
```

Slow stages (digest, verify, eliminate, and later compile and consolidate) run
on a schedule, when the host is idle ("sleep"), or on demand. Ingest and
distribute are on the host's hot path and must be fast and never fail the host.

## Core concepts

| Concept | Meaning |
|---|---|
| **Episode** | One unit of experience: the request, context digest, actions taken, observations, and which lessons were in context. Immutable. |
| **Outcome** | A label attached to an episode, possibly later: success, failure, partial, undone, declined, with a signal source and strength. Several per episode are allowed. |
| **Provenance** | Where an episode's content came from, with a trust level: `user-typed`, `user-voice-confident`, `user-voice-unreliable`, `tool-output`, `retrieved-web`, `file-content`, `self`. |
| **Lesson** | A candidate change to future behaviour: text, scope (host / machine / project / global), kind (fact / pattern / preference / procedure / gate), source episodes, and lifecycle state. |
| **Evidence** | A recorded test result: nutrient-test runs, credit accrued, contradiction checks. |
| **Credit** | Running estimate of a lesson's contribution to outcomes, with uncertainty. |
| **Metabolome** | The whole learned state (lessons, skills, gates, adapters) as a versioned snapshot. |
| **Host** | The system being made digestive (Helix first). |

## Lesson lifecycle

```
 candidate ──▶ quarantined ──▶ trial ──▶ accepted
     │              │           ▲ │          │
     │              │           │ ▼          ▼
     │              │         superseded ◀───┘
     │              │            │
     └──────────────┴────────────┴──────────────▶ eliminated (with reason, kept in log)
                                                     │
                     trial ◀── only by the user's ───┘
                               rollback (D-028)
```

- **candidate**: proposed by reflection, linked to source episodes.
- **quarantined**: derived wholly or partly from low-trust provenance
  (web, file content, unreliable voice). It must pass an extra check and can
  never be injected before that.
- **trial**: passed the nutrient test. Delivered on a coin flip (p = 0.5) on
  the turns it applies to, so its credit compares delivered against withheld
  turns (D-020).
- **accepted**: passed the credit ledger on live turns. Delivered on 90% of
  the turns it applies to; the 10% holdout keeps it measured.
- **superseded**: set aside for another lesson that covers the same turns,
  because the two contradict each other or it adds nothing to the other
  (D-027). Kept visible. It comes back through trial when that lesson
  leaves.
- **eliminated**: removed from use, with a reason. The log entry remains so
  "why did it forget X?" is answerable. Final, except that the user's
  rollback to a version the lesson was delivered in brings it back to trial.
  Hard deletion happens only on the user's explicit forget-with-proof
  request (privacy, D-028): the records go, an audit record with a hash of
  each stays.

## Distinguishing mechanisms

### Nutrient test (verification) — built

For candidate lesson *L* (`verify/`, decision rule D-018):

1. Choose episodes. **Relevant:** *L*'s source episodes plus similar ones found
   by episodic memory, up to 16. **Regression:** up to 10 past successes in
   scope that *L* is not about (D-024).
2. Replay each through the host's **side-effect-free replay port**, once with
   *L* in context and once without. Both sides carry the accepted lessons in
   force on that episode, so *L* is judged on what it adds to them (D-027). For Helix that is `helix replay` (D-017):
   it plans the turn's rounds, runs only read-only file steps between them
   (D-021), and never runs anything that would change something. Baselines
   are cached per run.
3. Measure the **noise floor.** Each baseline is replayed twice and judged
   against itself (A/A). r = (decisive A/A + 1) / (A/A + 2). A
   nondeterministic planner and a judge that reads noise as preference both
   show up here.
4. A **judge** compares the two plans against the original episode and its
   outcomes. On real replays, facts first (D-024, D-029): which one
   answered, then failed steps (model calls are not judged; cost is for
   credit, D-030). When the facts are the same but the answers differ, a
   model compares the answers (D-033); near-identical answers stay ties. The
   model is asked with the plans in both orders and a winner counts only
   when both answers agree (D-035), shown a plain-words view with A and B in
   a hashed order. In use: `outcome+both+deepseek`; heuristic on the
   simulator and on the coding host, where `go test` decides. Verdicts below
   0.6 confidence count as ties. Against 65 blind human labels the best
   judge agrees on 55 to 60%, the labeller with themselves on 75% (D-036).
5. **Decide** with binomial tails under the null "*L* does nothing" (a
   decisive win and a decisive loss each have probability r/2):
   - **pass → trial:** p_help ≤ 0.1 with ≥ 2 decisive wins (quarantined
     lessons: ≥ 4 wins and p ≤ 0.02). Trial is reversible.
   - **fail → eliminated:** harms past successes or makes things worse where
     it applies, at p ≤ 0.02 with ≥ 2 decisive losses. Elimination is
     permanent, so its bar is much higher.
   - When a lesson both wins and loses, its wins must also beat its losses
     in a sign test at the level of the call, and losses beat wins before an
     elimination (D-034): a judge decisive on most pairs gives a lesson that
     merely changes the plans many of both.
   - **inconclusive** otherwise, including fewer than 3 relevant
     comparisons. The lesson stays where it is.
6. Record every count and p-value as Evidence. `metabolome.Move` refuses a
   move to trial that does not cite passing evidence for that lesson (D-014).

This is the defence against "a wrong lesson gets assimilated", and it is
what makes C6 measurable.

### Distribution — built (Phase 3, D-019)

- `metabolism export` writes trial and accepted lessons, each with a delivery
  probability, to a file the host reads (`deliver/`).
- The host keeps the lessons whose scope covers the turn, fills its budget
  (accepted first, then trial lessons in a random order), and flips a coin
  per lesson. It records the result on the episode: `Exposure` (delivered)
  and `Withheld` (lost the coin). `deliver.Select` is the reference; Helix
  implements the same rule without importing the engine.
- Lessons reach the host's prompt only as data, in a fenced block.

### Credit ledger — built (Phase 3, D-020)

- Each lesson is a randomised experiment: its delivered turns against its
  withheld turns, since its last state change, success meaning `done` with no
  negative outcome (`ledger/`).
- Decisions use a Bayes factor, which stays valid however often the ledger
  is settled: accept a trial lesson at 30, eliminate at 300. Accepted lessons
  keep a 10% holdout, so a lesson that stops helping can still be
  eliminated.
- Credit, not age, decides: no lesson decays by time.
- Only the turns that bear on a lesson count for acceptance: those whose
  request resembles its sources' (`ledger.Relevance`, D-026). Harm counts
  on every turn. An accepted lesson is also judged on its last 200 turns, so
  one that stops holding is retired (drift).
- Cost too (D-030): model calls per turn, delivered against withheld. A
  trial lesson that is cheaper and no worse is accepted; one that is
  costlier and no better is eliminated.
- Lessons are also judged in pairs on the turns they share (D-027). Each
  coin is flipped on its own, so every combination occurs. A lesson that
  hurts when the other is delivered (contradiction), or adds nothing beside
  it while helping without it (redundancy), is superseded by the other.

### Habit compilation (Phase 5) — measured first; not built (D-031)

Phase 5 measured before compiling, with a stop condition, and every
candidate in Helix failed it: retry level touches 1% of model calls, "answer
now" touches 19% but nothing cost-free predicts it, and planning without
reasoning is 51% cheaper but about 3.4 points less successful (habits.md).
The design below stays for a host where a candidate passes:

1. Detect a recurring slow-model decision shape, for example "is this output a
   compile error or a network error?".
2. Draft a Jev question set for it (`Noul`, `Choice`, `Score`).
3. Lint the draft with typesafe-go's analyzers. Reject compound questions
   (`atomicquestion`), known Jev failure modes (`jaggededge`), and branching
   without a confidence check (`confidencecheck`).
4. Nutrient-test the gate against past decisions.
5. Promote to a trial gate; fall back to the slow model below the confidence
   threshold.

### Immune system — built (screen, provenance, quarantine, poisoning suite: Phase 4a)

- Provenance is attached at ingest and propagates to every derived lesson,
  including what a turn consumed: web content and files from outside the
  user's repositories make its lessons untrusted (taint, D-038).
- A quarantine tier holds anything derived from low-trust content.
- `immune/` screens every lesson before it is recorded: it rejects lessons
  that teach getting around confirmations, sandbox, firewall or hooks, or
  read as prompt injections, and every gate kind (procedures are accepted
  since Phase 6, through the same text screen).
- Content is sanitised using the host's rules. Lesson text is treated as data
  by the host.
- **Poisoning tests.** `poison-eval` injects adversarial episodes, as in
  MemSecBench, and asserts they neither become accepted lessons nor change
  behaviour (docs/poisoning.md); `planted` does the same with wrong lessons
  on real Helix replays (9 of 10 kept out).
- **Invariant:** no lesson kind can express "allow" or "skip confirmation".
  Gate lessons are *restrict-only* and need human approval in safety-sensitive
  hosts.

### Digestion tiers

| Tier | Holds | Delivered as | Phase |
|---|---|---|---|
| Episodic | past cases | retrieved examples | 1 |
| Procedural | lessons, skills, compiled gates | context block, tool procedures, Jev gates | 2–6 |
| Parametric | LoRA adapters on a small open model | model weights | 7 |

A lesson is promoted to a deeper tier only when it is stable, has proven
credit, and is high-volume. Parametric training requires a regression suite of
old episodes to measure forgetting.

## Ports

The engine depends only on these (`ports/ports.go`, abridged). Hosts and
adapters implement them.

```go
package ports

// Journal is the durable, append-only record of everything: episodes,
// outcomes, and (D-014) lessons, lifecycle events and evidence.
type Journal interface {
    AppendEpisode(ctx context.Context, ep types.Episode) error
    AppendOutcome(ctx context.Context, o types.Outcome) error
    AppendLesson(ctx context.Context, l types.Lesson) error
    AppendEvent(ctx context.Context, ev types.LifecycleEvent) error
    AppendEvidence(ctx context.Context, e types.Evidence) error
    AppendTag(ctx context.Context, t types.Tag) error // a named version (D-028)
    // Forget deletes the named records under one audit record; the only
    // way anything leaves the journal (D-028).
    Forget(ctx context.Context, f types.Forget) (types.Forget, error)
    Compact(ctx context.Context) error
    LastSeq(ctx context.Context) (int64, error)
    Scan(ctx context.Context, q types.JournalQuery) iter.Seq2[types.Record, error]
    Close() error
}

// Host is what a system implements to become digestive.
type Host interface {
    Describe() types.HostProfile
    // Replay re-plans a past episode with a lesson set and NO side effects
    // (Helix: `helix replay`). ErrCannotReplay: this host cannot judge it.
    Replay(ctx context.Context, ep types.Episode, lessons []types.Lesson) (types.ReplayResult, error)
}

// Judge compares two replays of one episode, given how it really went.
type Judge interface {
    Name() string
    Compare(ctx context.Context, original types.Episode, outcomes []types.Outcome,
        with, without types.ReplayResult) (types.Verdict, error)
}

// Reflector turns experience into candidate lessons. Its output is untrusted
// until digest validates and immune screens it.
type Reflector interface {
    Name() string
    Reflect(ctx context.Context, batch []types.Experience, existing []types.Lesson) ([]types.Lesson, error)
}

// Model is a text model (Ollama, DeepSeek, a local MLX server) behind
// reflectors, judges and the coding host's agent.
type Model interface {
    Name() string
    Complete(ctx context.Context, req ModelRequest) (ModelResponse, error)
}

// Trainer fine-tunes a model on verified examples (Phase 7): the one place
// non-Go code runs, as a separate process (adapters/remote/mlx).
type Trainer interface {
    Name() string
    Train(ctx context.Context, req TrainRequest) error
}

```

`ports.Store` and `ports.Selector` are still declared but unused. The
event-sourced `metabolome` replaced the separate mutable store (D-014);
versions are folds of the journal up to a position, and rollback appends
events (`versions/`, D-028). Selection
happens in the host, from the delivery file (D-019), so the engine is never
on the host's hot path.

Notes carried forward from the original sketch, which had these problems:

- A buffer held only in memory → replaced by the `Journal`, so a crash loses nothing.
- Ignored gate errors → a gate error means **fail closed**: don't
  promote, and don't inject new lessons.
- One failed stage killing the whole engine → stages are isolated and
  supervised. A failing stage is reported and retried; the host path never breaks.
- A "dreaming" loop that threw its output away → every stage writes its
  output to the Journal.

## Package layout

```
metabolism/
  cmd/metabolism/        the CLI (metabolism help lists every command)
  types/                 Episode, Outcome, Lesson, Evidence, Credit, Experience…
  ports/                 the interfaces above, and the trainer port (Phase 7)
  metabolome/            event-sourced lessons, lifecycle and evidence (D-014)
  immune/                deterministic safety screen, provenance trust
  digest/                reflection: validate, screen, dedupe, record
  llmreflect/            LLM reflector over ports.Model
  verify/                the nutrient test (D-015, D-018, D-034) and transfer (Phase 6)
  deliver/               the delivery file and the reference selection (D-019)
  ledger/                credit from randomised delivery (D-020), relevance and
                         drift (D-026), pairs (D-027), cost (D-030)
  versions/              named versions, as-of, diff, rollback, forget plans (D-028)
  judge/                 heuristic, outcome-first and LLM judges, both-orders
                         asking, the plain-words pair view (D-029, D-033, D-035)
  episodic/              tier 1: time-aware BM25 case recall
  mine/                  recurring failures sorted into guard and lesson territory (5c)
  parametric/            tier 3: promotion, training set, forgetting check,
                         adapter registry with rollback (Phase 7)
  audit/                 the scorecard: every open exit criterion and the C1–C9 rubric
  adapters/
    journal/sqlite/      modernc.org/sqlite (pure Go, same as Helix), append-only;
                         deletes only under a forget record (D-028)
    ndjson/              Helix's recorded episodes, wire v1 to v5, and purging
                         them of forgotten records (D-028)
    remote/llm/          Ollama, OpenAI-compatible (DeepSeek, a local MLX server) ┐ the only
    remote/jev/          Jev judge via typesafe-go                                │ network code;
    remote/helix/        drives `helix replay` (D-017)                            │ CLI only
    remote/mlx/          local LoRA training and serving (Phase 7)                ┘
  internal/privacyguard/ test: no network code outside adapters/remote/
  internal/hostguard/    test: no engine package depends on a host
  bench/                 metrics, curves, recall, nutrient, credit, decisions,
                         planning mode and agreement evaluation
  hosts/helixsim/        simulated Helix (the real Helix code lives in Helix)
  hosts/helixuser/       synthetic Helix user for automated days (D-023)
  hosts/coder/           the second host: a coding agent on a public benchmark (D-032)
  scripts/collect.sh     make collect: gathers the evidence, ends with the scorecard
```

Contradiction and supersession live in `ledger/` (pairs) and `verify/`
(context), not a separate `eliminate/` package. Not built: `compile/` (habit
compilation; every Phase 5 candidate failed its stop condition) and a
scheduler: on a work machine nothing runs unattended, so the slow stages run
when the user starts `make collect`.

## Technology choices

| Need | Choice | Why |
|---|---|---|
| Language | Go 1.25+ | User's stack; Helix is Go; concurrency; single binary |
| Storage | SQLite via `modernc.org/sqlite` | Pure Go, no CGO, already a Helix dependency |
| Case recall | In-process BM25 rebuilt from the journal (built); embeddings with brute-force cosine or `chromem-go` only if recall needs them | The corpus is small (thousands of episodes); avoid a service |
| Local model | Ollama HTTP API | Helix already supports it; most practical local inference |
| Hosted generator / reflector | DeepSeek API (`deepseek-flash`, `deepseek-v4-pro`) | Cheap and strong; opt-in |
| Fast judge / gates | Jev via `github.com/Nibir1/typesafe-go` | Typed, calibrated, 70–500 ms, about $0.04 per 1M input tokens; the user's SDK |
| Parametric training | MLX's LoRA trainer, run as a separate local process (D-032) | Private and free on Apple Silicon; nothing in pure Go trains LoRA on LLMs today (Born can't) |
| Durable workflows | Not needed at first; the Journal plus a scheduler | Temporal only if multi-node |

**Rejected:** Born for training (no LoRA, verified only at about 1B
parameters); Gorgonia (dormant); "Topological AI" (not credible); DeepSeek-API
LoRA (doesn't exist).
