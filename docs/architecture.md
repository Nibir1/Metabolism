# Architecture

Status (2026-10-02): **built through VERIFY.** Ingest, episodic memory,
digest and the nutrient test exist (Phases 0–2). Distribution, the credit
ledger and elimination by credit are Phases 3–4; habit compilation and the
parametric tier come later. Sections below say which parts are built and
which are planned. The interfaces are shaped by Helix, the first host, and a
second host must confirm them (Phase 8).

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

### Nutrient test (verification) — built

For candidate lesson *L* (`verify/`, decision rule D-018):

1. Choose episodes. **Relevant:** *L*'s source episodes plus similar ones found
   by episodic memory, up to 8. **Regression:** up to 6 past successes in
   scope that *L* is not about.
2. Replay each through the host's **side-effect-free replay port**, once with
   *L* in context and once without. For Helix that is `helix replay`, which
   plans and executes nothing (D-017). Baselines are cached per run.
3. Measure the **noise floor.** Each baseline is replayed twice and judged
   against itself (A/A). r = (decisive A/A + 1) / (A/A + 2). A
   nondeterministic planner and a judge that reads noise as preference both
   show up here.
4. A **judge** compares the two plans against the original episode and its
   outcomes. Heuristic on the simulator; a model judge (Jev, DeepSeek,
   Ollama) on real plans, shown a plain-words view with A and B in a hashed
   order. Verdicts below 0.6 confidence count as ties.
5. **Decide** with binomial tails under the null "*L* does nothing" (a
   decisive win and a decisive loss each have probability r/2):
   - **pass → trial:** p_help ≤ 0.1 with ≥ 2 decisive wins (quarantined
     lessons: ≥ 4 wins and p ≤ 0.02). Trial is reversible.
   - **fail → eliminated:** harms past successes or makes things worse where
     it applies, at p ≤ 0.02 with ≥ 2 decisive losses. Elimination is
     permanent, so its bar is much higher.
   - **inconclusive** otherwise, including fewer than 3 relevant
     comparisons. The lesson stays where it is.
6. Record every count and p-value as Evidence. `metabolome.Move` refuses a
   move to trial that does not cite passing evidence for that lesson (D-014).

This is the defence against "a wrong lesson gets assimilated", and it is
what makes C6 measurable.

### Credit ledger — planned (Phase 3)

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

### Immune system — screen and provenance built; quarantine checks and poisoning suite planned

- Provenance is attached at ingest and propagates to every derived lesson.
- A quarantine tier holds anything derived from low-trust content.
- `immune/` screens every lesson before it is recorded: it rejects lessons
  that teach getting around confirmations, sandbox, firewall or hooks, or
  read as prompt injections, and every gate or procedure kind for now.
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

// Model is a text model (Ollama, DeepSeek) behind reflectors and judges.
type Model interface {
    Name() string
    Complete(ctx context.Context, req ModelRequest) (ModelResponse, error)
}

// Selector chooses which trial and accepted lessons to deliver for a new
// request, within the host's budget, and records the exposure. Declared;
// implemented in Phase 3.
type Selector interface {
    Select(ctx context.Context, req types.Request, scope types.Scope, budget types.Budget) (types.Selection, error)
}
```

`ports.Store` is still declared but unused: the event-sourced `metabolome`
replaced the separate mutable store (D-014). Snapshots and rollback (Phase 4)
will be folds of the journal.

Notes carried forward from the original sketch, which had these problems:

- A buffer held only in memory → replaced by the `Journal`, so a crash loses nothing.
- Ignored gate errors → a gate error means **fail closed**: don't
  promote, and don't inject new lessons.
- One failed stage killing the whole engine → stages are isolated and
  supervised. A failing stage is reported and retried; the host path never breaks.
- A "dreaming" loop that threw its output away → every stage writes its
  output to the Journal.

## Package layout

Built:

```
metabolism/
  cmd/metabolism/        CLI: sim, ingest, inspect, curves, recall, recall-eval,
                         digest, verify, lessons, replay, label, crosscheck,
                         nutrient-eval, doctor
  types/                 Episode, Outcome, Lesson, Evidence, Credit, Experience…
  ports/                 the interfaces above
  metabolome/            event-sourced lessons, lifecycle and evidence (D-014)
  immune/                deterministic safety screen, provenance trust
  digest/                reflection: validate, screen, dedupe, record
  llmreflect/            LLM reflector over ports.Model
  verify/                the nutrient test (D-015, D-018)
  judge/                 heuristic and LLM judges, the plain-words pair view
  episodic/              tier 1: time-aware BM25 case recall
  adapters/
    journal/sqlite/      modernc.org/sqlite (pure Go, same as Helix), append-only
    ndjson/              Helix's recorded episodes, wire v1 (D-013)
    remote/llm/          Ollama, OpenAI-compatible (DeepSeek)   ┐ the only
    remote/jev/          Jev judge via typesafe-go              │ network code;
    remote/helix/        drives `helix replay` (D-017)          ┘ CLI only
  internal/privacyguard/ test: no network code outside adapters/remote/
  bench/                 metrics, curves, recall, nutrient and agreement evaluation
  hosts/helixsim/        simulated Helix (the real Helix code lives in Helix)
```

Planned: `ledger/` (credit, Phase 3), `eliminate/` (decay, contradiction,
supersession, Phase 4), `compile/` (habit compilation, Phase 5),
`consolidate/` (parametric tier, Phase 7), and a scheduler that runs the slow
stages when the host is idle.

## Technology choices

| Need | Choice | Why |
|---|---|---|
| Language | Go 1.25+ | User's stack; Helix is Go; concurrency; single binary |
| Storage | SQLite via `modernc.org/sqlite` | Pure Go, no CGO, already a Helix dependency |
| Case recall | In-process BM25 rebuilt from the journal (built); embeddings with brute-force cosine or `chromem-go` only if recall needs them | The corpus is small (thousands of episodes); avoid a service |
| Local model | Ollama HTTP API | Helix already supports it; most practical local inference |
| Hosted generator / reflector | DeepSeek API (`deepseek-flash`, `deepseek-v4-pro`) | Cheap and strong; opt-in |
| Fast judge / gates | Jev via `github.com/Nibir1/typesafe-go` | Typed, calibrated, 70–500 ms, about $0.04 per 1M input tokens; the user's SDK |
| Parametric training | External trainer over HTTP (Together or Fireworks), or Tinker via a Python sidecar | Nothing in pure Go trains LoRA on LLMs today (Born can't) |
| Durable workflows | Not needed at first; the Journal plus a scheduler | Temporal only if multi-node |

**Rejected:** Born for training (no LoRA, verified only at about 1B
parameters); Gorgonia (dormant); "Topological AI" (not credible); DeepSeek-API
LoRA (doesn't exist).
