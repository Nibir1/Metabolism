# Metabolism

**A Digestive AI engine in Go: AI that learns from its own experience, and proves it.**

Most "AI memory" stores facts and hopes they help. Metabolism treats every
lesson as a claim that has to be tested. It captures what an AI system did and
how it turned out, reflects on that into candidate lessons, **tests each lesson
before keeping it**, delivers kept lessons to future work by coin flip, credits
or blames them on real outcomes, and eliminates what stops earning its place.
Everything learned is visible, attributable and reversible, and nothing it
learns can make the host less careful.

Its first host is [Helix](https://github.com/Nibir1/Helix), a safety-first AI
shell written in Go.

---

## How it works

```
   host (Helix)                      Metabolism
 ┌──────────────┐  episodes +   ┌──────────┐   ┌──────────┐   ┌──────────────┐
 │  a turn runs │──outcomes────▶│  journal │──▶│  digest  │──▶│ immune screen│
 └──────────────┘  (local file) └──────────┘   └──────────┘   └──────┬───────┘
        ▲                         append-only     reflects into       │ candidate
        │                                         candidate lessons   ▼ lessons
        │  lessons, by coin flip  ┌──────────┐   ┌──────────┐   ┌──────────────┐
        └─────────────────────────│ deliver  │◀──│  ledger  │◀──│ nutrient test│
                                  └──────────┘   └──────────┘   └──────────────┘
                                   trial and       credit on       replay with and
                                   accepted        live outcomes:  without the lesson,
                                   lessons         accept or       against an A/A
                                                   eliminate       noise floor
```

1. **Capture.** The host records each turn (request, steps, outcome, model
   cost) to a local file; `metabolism ingest` reads it into an append-only
   journal. Nothing leaves the machine unless you opt in.
2. **Digest.** A reflector (a local model by default) reads past turns and
   proposes lessons, each citing the episodes it came from.
3. **Screen.** A deterministic immune screen rejects any lesson that would
   skip a confirmation, loosen a sandbox or read like a prompt injection.
4. **Verify: the nutrient test.** Past requests are replayed with and without
   the lesson, and each pair is judged against a baseline-against-baseline
   noise floor. Only lessons that help, beyond noise, reach trial.
5. **Deliver and credit.** Trial lessons are delivered on a coin flip, so
   every lesson has matched turns with and without it. A Bayes-factor credit
   ledger accepts what helps and eliminates what harms, on real outcomes.
6. **Eliminate and remember why.** Contradicting or redundant lessons are
   superseded; drifted ones retire. Every change is an event in a versioned
   store, with rollback and forget-with-proof.

The full design is in [docs/architecture.md](docs/architecture.md).

## What "done" means

[docs/vision.md](docs/vision.md) defines a fully digestive system as one that
scores 1 on nine criteria, each with the evidence required. As of 2026-09-30
no published system scores 9/9; the best are around 6.5
([docs/research.md](docs/research.md)).

| | Criterion | Status (2026-10-16) |
|---|---|---|
| C1 | Ingest: outcomes on ≥ 90% of real episodes | waiting on real use |
| C2 | Break down: lessons with no human in the loop, with sources | **met** |
| C3 | Assimilate: survives restarts; versioned store | **met** |
| C4 | Distribute: helps on held-out tasks | **met** (3 of 5 lessons helped on other projects) |
| C5 | Eliminate: wrong lessons removed; drift handled | planted lessons kept out 9 of 10; drift waits on real use |
| C6 | Verify: with/without evidence before keeping | **met** |
| C7 | Consolidate: into weights without forgetting | built; no lessons to train on yet |
| C8 | Endure: ≥ 3 months of real use | waiting on real use |
| C9 | Compound: improvement curves against a baseline | waiting on real use |

**Rubric: 4 of 9.** Run `make collect` for the live scorecard.

### What has been learned so far, including what did not work

- **The nutrient test keeps wrong lessons out on real replays**: 9 of 10
  planted wrong lessons never reached trial (D-033, D-034).
- **Lessons transfer**: tested on projects they were not learned in, 3 of 5
  helped and 1 harmed (it was specific to its own projects).
- **A model judge agrees with a person only so far.** Against 65 blind human
  labels the best judge agrees on 55 to 60%; the person agrees with
  themselves on 75%. Stronger models and worked examples did not close the
  gap (D-035, D-036). Facts (did the task finish, did steps fail) decide
  first wherever they can.
- **Habit compilation did not pay off in Helix.** Every candidate failed its
  stop condition, including planning without reasoning: 51% cheaper, about
  3.4 points less successful over 683 coin-flipped turns
  ([docs/habits.md](docs/habits.md)).
- **The coding benchmark had no middle ground.** DeepSeek passed 91% of the
  exercises and local 3B and 7B models 3 to 5%, so no lesson could change an
  outcome there (D-037).

Negative results are written up like positive ones; every decision and its
evidence is in [docs/decisions.md](docs/decisions.md).

## Getting started

**Requirements:** Go 1.25 or later. macOS for the coding host's sandbox
(`sandbox-exec`). [Helix](https://github.com/Nibir1/Helix), branch
`feat/metabolism-integration`, checked out next to this repository, for real
use. Optional: a DeepSeek and a TypeSafe (Jev) API key, for hosted reflection
and judging.

```bash
make            # list every target
make build      # bin/metabolism
make check      # lint, race tests, and the Helix wire contracts
make demo       # every CLI command end to end on a simulated Helix
```

**With real Helix** ([docs/verifying.md](docs/verifying.md) has the details):

```bash
make helix-build    # build Helix from its integration branch, and Metabolism
./bin/helix         # then, inside Helix: /metabolism on
make collect        # ingest your turns, measure, and write the scorecard
```

Use Helix normally with recording on; leave `/lessons` off for the first
three weeks, which are the baseline. After that, `make collect` starts
learning from your own turns and you turn lessons on with `/lessons on`.

**API keys** go in a git-ignored `.env` file in this directory (see
[.env.example](.env.example)); the CLI reads them, and every command that
sends anything off the machine says so and needs `-allow-remote`.

## Privacy and safety

- **Local first.** Recording is off until you turn it on, stays on your
  machine, and is masked by the host. Sending experience to a hosted model is
  an explicit opt-in per command. A test fails the build if any engine package
  outside `adapters/remote/` can reach the network.
- **Learning informs; it never grants authority.** No lesson can lower a risk
  tier, answer a confirmation or loosen a sandbox. A restriction can only be
  *proposed*, for you to approve.
- **Visible and reversible.** Every lesson has its sources, history and
  evidence (`metabolism lessons -id`). Versions can be named, compared and
  rolled back; `metabolism forget` deletes with an audit record that keeps a
  hash, not the content.
- **Append-only.** The journal refuses updates and deletes at the database
  level, except deletions under a forget record.

## Command overview

| Area | Commands |
|---|---|
| Capture and inspect | `ingest`, `inspect`, `doctor`, `curves`, `recall`, `recall-eval` |
| Learn | `digest`, `verify`, `lessons`, `replay`, `export`, `credit` |
| Judges | `label`, `crosscheck` |
| Versions | `snapshot`, `versions`, `diff`, `rollback`, `forget` |
| Experiments | `auto`, `bench`, `planted`, `transfer`, `decisions`, `patterns`, `train`, `adapter` |
| Simulator evaluations | `sim`, `nutrient-eval`, `credit-eval`, `poison-eval`, `drift-eval`, `pairs-eval` |
| Evidence | `scorecard`, `pool`, `offpeak` |

`bin/metabolism help` lists them; `bin/metabolism <command> -h` shows flags.

## Repository layout

The engine imports only `types/` and `ports/`; hosts, adapters and the CLI sit
outside it, and a test keeps the engine free of host code. The full map, with
what each package holds, is in [AGENTS.md](AGENTS.md) and
[docs/architecture.md](docs/architecture.md#package-layout).

## Documentation

| Doc | Contents |
|---|---|
| [AGENTS.md](AGENTS.md) | Orientation for contributors and coding agents: layout, rules, how to work here |
| [docs/vision.md](docs/vision.md) | What Digestive AI is, the nine-criterion definition of "100%", and why |
| [docs/research.md](docs/research.md) | State of the art (2026-09-30) and what is still open |
| [docs/architecture.md](docs/architecture.md) | The loop, the lesson lifecycle, ports, packages, technology |
| [docs/roadmap.md](docs/roadmap.md) | Phase by phase, with exit criteria and status |
| [docs/decisions.md](docs/decisions.md) | Every decision, its evidence, and what would change it |
| [docs/helix-integration.md](docs/helix-integration.md) | Helix, the first host, and the contracts between them |
| [docs/verifying.md](docs/verifying.md) | Checking it: automated, with real Helix, live models, `make collect` |
| [docs/poisoning.md](docs/poisoning.md) | The poisoning suite: threat model, cases, metrics |
| [docs/habits.md](docs/habits.md) | Phase 5: measuring model decisions before compiling them |
| [docs/references.md](docs/references.md) | Papers, products, libraries and benchmarks |
| [bench/results/README.md](bench/results/README.md) | Recorded results and how they were produced |

## Related projects

- [Helix](https://github.com/Nibir1/Helix): the first host, a safety-first AI
  shell in Go.
- [typesafe-go](https://github.com/Nibir1/typesafe-go): the Go SDK for
  TypeSafe's Jev model, used for fast typed judging.
