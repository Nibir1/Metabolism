# Metabolism — orientation for a fresh session

Metabolism is a **Digestive AI engine** written in Go. It lets an AI system
learn from its own experience and *prove* the learning helped. The engine
captures what happened and how it turned out, reflects on that into candidate
lessons, verifies each lesson before keeping it, delivers kept lessons to
future work, credits or blames them by outcome, and eliminates what stops
earning its place.

Status (2026-09-30): **design only.** No engine code yet. The docs below are
the source of truth for what, why and how.

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
