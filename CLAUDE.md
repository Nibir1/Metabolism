# Metabolism — orientation for a fresh session

Metabolism is a **Digestive AI engine** written in Go. It lets an AI system
learn from its own experience and *prove* the learning helped. The engine
captures what happened and how it turned out, reflects on that into candidate
lessons, verifies each lesson before keeping it, delivers kept lessons to
future work, credits or blames them by outcome, and eliminates what stops
earning its place.

Status (2026-10-02): **Phases 0–3 built.** Phase 2: reflection and the
nutrient test, on the simulator and on real Helix replays (the first real
lesson passed, D-018). Phase 3: delivery of tested lessons into live Helix
turns by coin flip (D-019) and a credit ledger that accepts or eliminates them
by Bayes factor on live outcomes (D-020), checked on the simulator. Open exit
criteria, all waiting on real data: the real Helix baseline (weeks of use with
lessons off), a public benchmark, planted wrong lessons on real replays, a
human-labelled sample for judge agreement, real credit decisions, and the
Phase 3 go/no-go. Phase 4 started (D-025): the poisoning suite and its
defences (4a, docs/poisoning.md) and drift (4b, D-026: a recent window, and
credit that counts the turns bearing on a lesson) and contradictions and
supersession (4c, D-027: the nutrient test in context, pairs in the
ledger), all on the simulator, and the versioned metabolome (4d, D-028:
versions, rollback to trial, forget-with-proof).
See [docs/roadmap.md](docs/roadmap.md). The docs below are the source of
truth for what, why and how.

## Working in this repo

- `make` lists every target.
  - `make check`: lint (gofmt, vet, golangci-lint v2.13.2), race tests and the wire contract. Keep it green.
  - `make verify`: `check`, plus Helix's integration tests and an end-to-end CLI demo.
  - `make baseline-sim`: regenerates the synthetic baseline in `bench/results/`.
- Checking with real Helix: [docs/verifying.md](docs/verifying.md)
  (`make helix-build`, `make doctor`, `make helix-report`). The user runs Helix
  and shares the output.
- Layout:

  | Path | Contents |
  |---|---|
  | `types/` | vocabulary |
  | `ports/` | interfaces; the engine imports only these and `types` |
  | `adapters/journal/sqlite` | durable journal |
  | `adapters/ndjson` | `metabolism ingest`: reads Helix's recorded episodes (wire v1, v2 and v3) and `/lessons forget [--hard]` feedback; purges forgotten records from them |
  | `episodic/` | tier-1 memory: time-aware case recall over the journal |
  | `metabolome/` | event-sourced lessons; the only code that changes lesson state (D-014) |
  | `immune/` | deterministic screen: no lesson may weaken a safety gate |
  | `digest/` | reflection: reflector output is untrusted until validated |
  | `verify/` | the nutrient test: replays, A/A noise floor, calibrated decision rule (D-015, D-018) |
  | `deliver/` | `metabolism export`: the delivery file Helix reads, and the reference selection (D-019) |
  | `ledger/` | the credit ledger: delivered vs withheld turns, Bayes-factor decisions (D-020), relevance and drift (D-026), pairs and supersession (D-027) |
  | `judge/` | judges: heuristic, LLM (via `ports.Model`), and the plain-words pair view every model judge sees |
  | `llmreflect/` | LLM reflector (via `ports.Model`) |
  | `adapters/remote/` | the only code that reaches beyond the machine: `llm` (Ollama, DeepSeek), `jev` (typesafe-go), `helix` (drives `helix replay`). Imported only by the CLI, behind `-allow-remote` (D-016, D-017) |
  | `internal/privacyguard` | test: no network code outside `adapters/remote/` |
  | `hosts/helixsim` | simulated Helix |
  | `hosts/helixuser` | synthetic Helix user: writes a day's requests for `metabolism auto` (D-023) |
  | `versions/` | the versioned metabolome: named versions, as-of, diff, rollback, forget plans (D-028) |
  | `bench/` | metrics, curves, recall evaluation, nutrient-test and credit evaluation, judge agreement (κ) |
  | `cmd/metabolism` | CLI |

- Helix integration work happens in `../Helix` on branch
  `feat/metabolism-integration` (the same branch name in typesafe-go). Never
  on main. Helix ↔ engine contracts, each pinned by a golden file in both
  repos: recording wire v1, v2 and v3 (D-013, D-019, D-031), replay v1 and v2 (D-017, D-021) and the
  delivery file v1 (D-019). `make check-wire` compares them all.
- Helix is built and tested with `CGO_ENABLED=0` on this machine (the cgo
  linker fails; see [docs/verifying.md](docs/verifying.md)).
- `metabolism auto -allow-remote` runs one automated day into `.runs/auto/`
  (D-023): a synthetic user drives real Helix, then ingest, digest, verify,
  credit. It changes the user's Helix config only during the run and
  restores it byte for byte. Run it only when the user asks, in their
  terminal pane: this is a work PC, so no schedules or unattended runs.
  It refuses to start in DeepSeek's peak hours (double price) unless
  `-allow-peak`: run days after 12:00 CEST on weekdays, or at weekends.
- `make demo` (and so `make verify`) deletes and rebuilds `.demo/cli/`. Keep
  journals and files from live runs in `.runs/`, which no target touches.
  The first live-run data was lost to an older demo that cleared all of
  `.demo/`.
- The journal is append-only by database trigger. Never add a code path that
  updates records, or deletes them other than `Journal.Forget`, which deletes
  only under an append-only forget record (migration 2, D-028). Rollback
  appends events; it never rewrites.

## Read in this order

1. [docs/vision.md](docs/vision.md) — what "Digestive AI" means, the 9-criterion
   rubric that defines "100%", and why this engine exists.
2. [docs/research.md](docs/research.md) — state of the art as of 2026-09-30:
   nobody has reached 100%; the best systems score ~6.5/9. What is solved and
   what is open.
3. [docs/architecture.md](docs/architecture.md) — engine concepts, the loop,
   the lesson lifecycle, ports, storage, the Go stack.
4. [docs/roadmap.md](docs/roadmap.md) — phase after phase, each with exit
   criteria and the rubric criteria it moves.
5. [docs/helix-integration.md](docs/helix-integration.md) — Helix (the user's
   Go AI shell, `../Helix`) is the **first host**. How the two connect.
6. [docs/decisions.md](docs/decisions.md) — decisions already made, with
   reasons. Don't re-litigate them without new evidence.
7. [docs/references.md](docs/references.md) — external papers, products,
   libraries and benchmarks with URLs.
8. [docs/poisoning.md](docs/poisoning.md) — Phase 4a: the poisoning suite's
   threat model, cases and metrics (modelled on MemSecBench).
9. [docs/habits.md](docs/habits.md) — Phase 5 design (proposed, D-031):
   what Helix's model decisions really are, and measuring before compiling.

## Non-negotiables

- **Go only for the engine.** The single accepted exception is weight training
  (Phase 7), which goes through an external trainer over HTTP or a sidecar.
- **Measure before learning.** The evaluation harness and baseline curves come
  before any learning feature. A lesson with no evidence is not a lesson.
- **Learning informs; it never grants authority.** In a host with safety gates
  (Helix), a lesson can never lower a risk tier, answer a confirmation or
  loosen a sandbox. At most it can *propose* a restriction the user approves.
- **Local-first and private by default.** Sending experience to a hosted model
  (DeepSeek, Jev) is opt-in per host. The default reflector is a local model.
- **Everything learned is visible, attributable and reversible.** Provenance on
  every lesson, a versioned store, forget-with-reason.
- **Domain-agnostic core.** No host types in the engine. Helix shapes the
  interfaces; a second host later proves them.
- **Honesty over hype.** Don't claim a rubric criterion without the evidence
  named in [docs/vision.md](docs/vision.md). Negative results get written up too.

## Related repositories

- `../Helix` (github.com/Nibir1/Helix): first host. Go, about 50k LOC, safety-first
  AI shell.
- github.com/Nibir1/typesafe-go: the user's Go SDK for TypeSafe's Jev model,
  used for fast typed gates and judging.
