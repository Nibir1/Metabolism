# Metabolism

**A Digestive AI engine in Go:** AI that learns from its own experience, and
proves it.

Metabolism captures what an AI system did and how it turned out, reflects on
that into candidate lessons, **verifies each lesson before keeping it**,
delivers kept lessons to future work, credits or blames them by outcome, and
eliminates what stops earning its place.

Status (2026-10-02): **Phases 0–2 built; Phase 3 next.** The engine records
Helix's planner turns (append-only journal), recalls similar past cases,
reflects on them into candidate lessons, screens each one for safety, and
tests it by replaying past requests through real Helix with and without it.
The first lesson learned from real Helix episodes has passed that test. No
lesson reaches a live Helix turn yet: delivery and credit are Phase 3. The
roadmap says which exit criteria still wait on real data.

```bash
make            # lists every target
make check      # lint, race tests, the Helix wire contracts
make verify     # check, plus Helix's integration tests and an end-to-end demo
```

Start with the docs:

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
