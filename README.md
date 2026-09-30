# Metabolism

**A Digestive AI engine in Go:** AI that learns from its own experience, and
proves it.

Metabolism captures what an AI system did and how it turned out, reflects on
that into candidate lessons, **verifies each lesson before keeping it**,
delivers kept lessons to future work, credits or blames them by outcome, and
eliminates what stops earning its place.

Status: **design phase.** Start with the docs:

| Doc | Contents |
|---|---|
| [docs/vision.md](docs/vision.md) | What Digestive AI is, the 9-criterion definition of "100%", and why this exists |
| [docs/research.md](docs/research.md) | State of the art (2026-09-30): nobody has reached 100% |
| [docs/architecture.md](docs/architecture.md) | Engine design, lesson lifecycle, ports, stack |
| [docs/roadmap.md](docs/roadmap.md) | Phase-by-phase plan with exit criteria |
| [docs/helix-integration.md](docs/helix-integration.md) | Helix, the first host |
| [docs/decisions.md](docs/decisions.md) | Decision log |
| [docs/references.md](docs/references.md) | Papers, products, benchmarks, libraries |
| [docs/verifying.md](docs/verifying.md) | `make verify`, and trying it with real Helix |
