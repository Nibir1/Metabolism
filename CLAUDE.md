# Metabolism — orientation for a fresh session

Metabolism is a **Digestive AI engine** written in Go. It lets an AI system
learn from its own experience and *prove* the learning helped. The engine
captures what happened and how it turned out, reflects on that into candidate
lessons, verifies each lesson before keeping it, delivers kept lessons to
future work, credits or blames them by outcome, and eliminates what stops
earning its place.

Status (2026-10-01): **Phases 0 and 1 built; Phase 2a (engine core of
reflection and the nutrient test) built.** Next: 2b (Ollama and DeepSeek
reflectors, LLM judge), then 2c (real Helix plan-only replay, Jev judge).
Phase 0 and 1 exit criteria wait on real Helix data. See
[docs/roadmap.md](docs/roadmap.md). The docs below are the source
of truth for what, why and how.

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
  | `adapters/ndjson` | `metabolism ingest`: reads Helix's recorded episodes (wire v1) |
  | `episodic/` | tier-1 memory: time-aware case recall over the journal |
  | `metabolome/` | event-sourced lessons; the only code that changes lesson state (D-014) |
  | `immune/` | deterministic screen: no lesson may weaken a safety gate |
  | `digest/` | reflection: reflector output is untrusted until validated |
  | `verify/` | the nutrient test (D-015) |
  | `judge/` | judges for the nutrient test |
  | `internal/privacyguard` | test: no network code outside `adapters/remote/` |
  | `hosts/helixsim` | simulated Helix |
  | `bench/` | metrics and curves |
  | `cmd/metabolism` | CLI |

- Helix integration work happens in `../Helix` on branch
  `feat/metabolism-integration` (the same branch name in typesafe-go). Never
  on main. Helix ↔ engine contract: wire-v1 NDJSON, pinned by a golden file in
  both repos; `make check-wire` compares them (D-013).
- The journal is append-only by database trigger. Never add a code path that
  updates or deletes records. Forget-with-proof is a planned, audited Phase 4
  migration.

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
