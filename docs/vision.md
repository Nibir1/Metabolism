# Vision: what Metabolism is and why it exists

## The one-line thesis

> Metabolism is the first engine that lets an AI system **prove** it got better
> from its own experience, with receipts for every lesson it absorbed.

## The problem

Today's AI systems are brilliant in a single session and amnesiac across them.
Production "memory" (ChatGPT, Claude Code auto-memory, Gemini personal context,
Mem0, Zep) stores **facts and preferences** and replays them as text. It does
not learn *what works* from outcomes, and it never checks whether a stored item
actually helps.

The field agrees this is the open problem:

- Karpathy (Oct 2025): missing continual learning is a key reason agents
  "just don't work" yet.
- Hassabis (Jan 2026): "That technique has not been cracked yet."
- Letta (Dec 2025): "Modern LLMs deployed in production do not continually learn."
- Independent 2026 benchmarks: explicit memory and skill systems often do **no
  better than plain in-context learning** (ContinualSkillBench, Continual
  Learning Bench), and memory stores are easy to poison (MemSecBench: 84% of
  injected poison persists).

See [research.md](research.md) for the evidence.

## Where the name comes from

The idea began as a metaphor from Dr. Randolph Stone's Polarity Therapy chart,
which stacks body centres from bottom to top:

| Centre | AI analogue | Direction |
|---|---|---|
| **Eliminative** | Discriminative ML and deep learning: classify, score, filter | Many inputs → one decision |
| **Generative** | Generative AI (LLMs, diffusion) | Small seed → rich output |
| **Digestive** | *This project*: absorb experience, extract what nourishes, rebuild behaviour, excrete waste | Experience → changed self |

The metaphor supplies **names and intuition, not evidence.** The technical
terms are *continual learning*, *lifelong learning*, *experiential learning*
and *self-evolving agents*. Use those when searching literature. Also, the
eliminative/generative split is really the discriminative/generative split,
and generative models predate 2020 (Naive Bayes, HMMs, VAEs, GANs).

In this vocabulary, TypeSafe's **Jev** (a fast, non-generative model that
returns typed choices, scores and probabilities) is a pure *eliminative* organ,
which is exactly why it suits Metabolism's gates and judges.

## Definition: what "100% Digestive AI" means

"100%" is only meaningful against a rubric. A system is fully digestive when it
scores 1 on all nine criteria, **each backed by the evidence named here**:

| # | Criterion | Plain meaning | Evidence required for a 1 |
|---|---|---|---|
| C1 | **Ingest** | Continuously captures experiences *and* their outcomes | Every episode stored with a labelled outcome; outcome coverage ≥ 90% of episodes |
| C2 | **Break down** | Reflects on its own to pull out lessons | Lessons produced with no human in the loop, each linked to source episodes |
| C3 | **Assimilate** | The change persists across sessions and restarts | Lessons survive restarts and upgrades; versioned store |
| C4 | **Distribute** | Measurably better on *later, different* tasks | Held-out improvement on tasks unlike the ones lessons came from |
| C5 | **Eliminate** | Prunes, decays or corrects stale or wrong knowledge | Injected-wrong-lesson tests: detected and removed; drift tests pass |
| C6 | **Verify** | Checks a lesson helps before keeping it (causal credit) | Every accepted lesson has an ablation result: with vs without |
| C7 | **Consolidate** | Moves stable lessons into weights without forgetting | Parametric update with measured retention on an old-task regression suite |
| C8 | **Endure** | Runs on real, open-ended work for weeks or months | ≥ 3 months continuous real use with stable or improving metrics |
| C9 | **Compound** | Shows an improvement curve over time | Published learning curves (quality ↑, cost per outcome ↓) versus a baseline |

As of 2026-09-30 **no system scores 9/9.** The best research system (Darwin
Gödel Machine) is about 6.5, and the best production system (Cursor Composer
real-time RL) is about 6. The field has split into two halves nobody has
joined:

- **Memory and harness learners:** reflection, pruning and sometimes
  verification, but frozen weights (Letta, MUSE, ACE, EvolveMem, Harness CL).
- **Weight learners:** weight updates, but no reflection, pruning or
  per-lesson verification (SEAL, Cursor RL, "LMs Need Sleep").

**Nobody combines C6 (verify), C7 (consolidate) and C8 (endure).** That
combination is Metabolism's target.

## Why build it, as a single engineer

Being first at "memory" is impossible, since the space is crowded. The openings
that remain suit one careful engineer better than a lab:

1. **Proof, not storage.** No benchmark checks whether a specific lesson caused
   a gain. Building that measurement is cheap and positions the project.
   Labs *use* benchmarks.
2. **An immune system.** Memory poisoning is largely undefended. Helix already
   has a mature zero-authority, provenance-labelled safety architecture that
   memory systems lack.
3. **Open, local-first, model-agnostic and in Go.** Labs build learning into
   their own products. Letta, the closest open alternative, is Python. There is
   room for a self-hostable, auditable engine.
4. **A real first user.** Helix already exists, already emits outcome signals,
   and its author already digests failures into planner rules *by hand* (see
   [helix-integration.md](helix-integration.md)). Metabolism automates that.
5. **Value even if it fails.** A rigorous negative result ("why agent memory
   doesn't compound, measured") is publishable and builds expertise in the
   field's central open problem.

## Distinguishing ideas

These are Metabolism's own contributions. Some overlap prior work, and the
overlap is noted.

1. **Nutrient test (verify before keep).** Every candidate lesson is replayed
   against past episodes with and without it, and kept only if it wins.
   *Overlap:* DGM, Relai, EvolveMem and Harness CL gate changes on measured
   gain. Metabolism's twist is doing it per lesson, as a first-class record,
   and **safely** through the host's no-side-effect replay mode.
2. **Credit ledger.** Every output records which lessons were in its context.
   Outcomes propagate credit or blame back to them. Lessons decay by
   *nutritional value* (measured contribution), not age.
3. **Habit compilation (System 2 → System 1).** When the slow model keeps
   making the same kind of decision, the engine compiles it into a Jev question
   set: a 100 ms typed check. Generated questions are linted by typesafe-go's
   analyzers (`atomicquestion`, `jaggededge`, `confidencecheck`) before use.
   *Novelty not yet verified by a literature search. Do that before Phase 5.*
4. **Three digestion depths with evidence-gated promotion:**

   | Tier | Stores | Examples |
   |---|---|---|
   | Episodic | cases | Memento-style |
   | Procedural | lessons, skills, compiled gates | Voyager / ACE / CASCADE-style |
   | Parametric | LoRA on a small open model | only for stable, proven, high-volume lessons |

5. **Immune system.** Provenance and trust levels on every episode, quarantine
   for anything derived from untrusted content, and poisoning tests in CI.
6. **Metabolic rate.** Tokens and dollars are calories. The headline metric is
   cost per good outcome falling while quality rises.
7. **Versioned metabolome.** Memory state is versioned like git: snapshots,
   branches, rollback, and forget-with-proof.
8. **Metabolic Benchmark.** Publish the evaluation that measures C1–C9,
   especially causal credit, long tenure and poisoning resistance.

## What Metabolism is not

- Not a wrapper around a model, and not a coding harness like Claude Code or Codex.
- Not a vector database with a new name. Storage is the easy part.
- Not a claim that the problem is solved. The roadmap climbs the rubric one
  criterion at a time and reports honestly where it stands.
