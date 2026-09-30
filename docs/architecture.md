# Architecture

Status: **proposal.** The interfaces below are a starting point, to be shaped
by Helix (the first host) and then confirmed by a second host. Nothing here is
implemented yet.

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
 candidate ──▶ quarantined ──▶ trial ──▶ accepted ──▶ superseded
     │              │            │           │
     └──────────────┴────────────┴───────────┴──▶ eliminated (with reason, kept in log)
```

- **candidate**: proposed by reflection, linked to source episodes.
- **quarantined**: derived wholly or partly from low-trust provenance
  (web, file content, unreliable voice). It must pass an extra check and can
  never be injected before that.
- **trial**: passed the nutrient test. Injected on a small share of eligible
  episodes, with credit tracked (bandit-style exploration).
- **accepted**: positive credit with enough confidence. Injected normally.
- **superseded**: replaced by a better lesson. Kept visible and restorable.
- **eliminated**: removed from use, with a reason. The log entry remains so
  "why did it forget X?" is answerable. Hard deletion happens only on the
  user's explicit forget-with-proof request (privacy).

## Distinguishing mechanisms

### Nutrient test (verification)

For candidate lesson *L* with source and eligible past episodes *E*:

1. Replay each episode in *E* through the host's **side-effect-free replay
   port** (for Helix, plan-only mode), once with *L* in context and once without.
2. A judge compares both outputs to the known outcome of the original episode.
   The judge is a Jev `Score` or `Choice` question, a host-specific checker, or
   a local LLM.
3. Accept for trial only if with-*L* beats without-*L* by a margin, across
   enough episodes, and regresses no episode that previously succeeded.
4. Record everything as Evidence.

This is the defence against "a wrong lesson gets assimilated", and it is
what makes C6 measurable.

### Credit ledger

- Every episode records the exact set of lessons injected (the *exposure*).
- When outcomes arrive, update each exposed lesson's credit. Start with simple
  Beta-Bernoulli success counts per lesson versus a matched no-lesson baseline
  from trial randomisation. More refined attribution can come later.
- Elimination uses credit, not age: lessons with confidently non-positive
  credit decay out.

### Habit compilation (Phase 5)

1. Detect a recurring slow-model decision shape, for example "is this output a
   compile error or a network error?".
2. Draft a Jev question set for it (`Noul`, `Choice`, `Score`).
3. Lint the draft with typesafe-go's analyzers. Reject compound questions
   (`atomicquestion`), known Jev failure modes (`jaggededge`), and branching
   without a confidence check (`confidencecheck`).
4. Nutrient-test the gate against past decisions.
5. Promote to a trial gate; fall back to the slow model below the confidence
   threshold.

### Immune system

- Provenance is attached at ingest and propagates to every derived lesson.
- A quarantine tier holds anything derived from low-trust content.
- Content is sanitised using the host's rules. Lesson text is treated as data
  by the host.
- **Poisoning tests in CI.** Inject adversarial episodes, as in MemSecBench,
  and assert they neither become accepted lessons nor change behaviour.
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

## Ports (proposed Go interfaces)

The engine depends only on these. Hosts and adapters implement them.

```go
package ports

// Host is what a system implements to become digestive.
type Host interface {
    // Replay re-runs a past episode with a given lesson set and NO side
    // effects (Helix: plan-only mode). Required for the nutrient test.
    Replay(ctx context.Context, ep types.Episode, lessons []types.Lesson) (types.ReplayResult, error)
    // Describe returns the host's scopes, lesson kinds it accepts, and budgets.
    Describe() types.HostProfile
}

// Journal is the durable, append-only record of everything.
type Journal interface {
    AppendEpisode(ctx context.Context, ep types.Episode) error
    AppendOutcome(ctx context.Context, o types.Outcome) error
    AppendEvent(ctx context.Context, ev types.LifecycleEvent) error
    Scan(ctx context.Context, q types.JournalQuery) iter.Seq2[types.Record, error]
}

// Store is the queryable, versioned metabolome.
type Store interface {
    PutLesson(ctx context.Context, l types.Lesson) error
    Lessons(ctx context.Context, q types.LessonQuery) ([]types.Lesson, error)
    RecordEvidence(ctx context.Context, e types.Evidence) error
    Credit(ctx context.Context, lessonID string) (types.Credit, error)
    Snapshot(ctx context.Context, label string) (types.SnapshotID, error)
    Restore(ctx context.Context, id types.SnapshotID) error
}

// Reflector turns episodes into candidate lessons (the slow model).
type Reflector interface {
    Reflect(ctx context.Context, eps []types.Episode, existing []types.Lesson) ([]types.Lesson, error)
}

// Judge compares replay outputs against a known outcome (Jev, checker or LLM).
type Judge interface {
    Compare(ctx context.Context, original types.Episode, with, without types.ReplayResult) (types.Verdict, error)
}

// Selector chooses which accepted/trial lessons to deliver for a new request,
// within the host's budget. It records the exposure for the credit ledger.
type Selector interface {
    Select(ctx context.Context, req types.Request, budget types.Budget) (types.Selection, error)
}
```

Notes carried forward from the original sketch, which had these problems:

- A buffer held only in memory → replaced by the `Journal`, so a crash loses nothing.
- Ignored gate errors → a gate error means **fail closed**: don't
  promote, and don't inject new lessons.
- One failed stage killing the whole engine → stages are isolated and
  supervised. A failing stage is reported and retried; the host path never breaks.
- A "dreaming" loop that threw its output away → every stage writes its
  output to the Journal.

## Planned package layout

```
metabolism/
  cmd/metabolism/        CLI: inspect, replay, bench, snapshot
  types/                 Episode, Outcome, Lesson, Evidence, Credit…
  ports/                 interfaces above
  engine/                scheduler, stage supervisor, lifecycle state machine
  digest/                reflection prompts and parsing
  verify/                nutrient test
  episodic/              tier 1: time-aware case recall (Phase 1)
  ledger/                credit assignment
  immune/                provenance, quarantine, poisoning tests
  eliminate/             decay, contradiction, supersession
  compile/               habit compilation → Jev gates (Phase 5)
  consolidate/           parametric tier (Phase 7)
  adapters/
    journal/sqlite/      modernc.org/sqlite (pure Go, same as Helix)
    ndjson/              host-recorded episodes, wire v1 (Helix; D-013)
    store/sqlite/
    reflector/ollama/    local default
    reflector/deepseek/  opt-in
    judge/jev/           via github.com/Nibir1/typesafe-go
    judge/llm/
  bench/                 Metabolic Benchmark harness and public benchmark runners
  hosts/helixsim/        test double for Helix (the real adapter lives in Helix)
```

## Technology choices

| Need | Choice | Why |
|---|---|---|
| Language | Go 1.25+ | User's stack; Helix is Go; concurrency; single binary |
| Storage | SQLite via `modernc.org/sqlite` | Pure Go, no CGO, already a Helix dependency |
| Vector search | Start with SQLite plus brute-force cosine; `chromem-go` if needed | The corpus is small (thousands of lessons); avoid a service |
| Local model | Ollama HTTP API | Helix already supports it; most practical local inference |
| Hosted generator / reflector | DeepSeek API (`deepseek-flash`, `deepseek-v4-pro`) | Cheap and strong; opt-in |
| Fast judge / gates | Jev via `github.com/Nibir1/typesafe-go` | Typed, calibrated, 70–500 ms, about $0.04 per 1M input tokens; the user's SDK |
| Parametric training | External trainer over HTTP (Together or Fireworks), or Tinker via a Python sidecar | Nothing in pure Go trains LoRA on LLMs today (Born can't) |
| Durable workflows | Not needed at first; the Journal plus a scheduler | Temporal only if multi-node |

**Rejected:** Born for training (no LoRA, verified only at about 1B
parameters); Gorgonia (dormant); "Topological AI" (not credible); DeepSeek-API
LoRA (doesn't exist).
