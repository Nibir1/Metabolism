# Research baseline (as of 2026-09-30)

This is a snapshot of the field when Metabolism was designed. Scores use the
9-criterion rubric in [vision.md](vision.md). Each criterion is scored 0, ½ or
1 from abstracts and summaries, so treat ½ versus 1 as roughly ±½. Refresh this
file before each phase that depends on it. URLs are in
[references.md](references.md).

## Verdict

**No system has achieved 100% Digestive AI**, in research or in production.

- Research best: about 6.5/9.
- Production best: about 6/9.
- Benchmarks show learning from experience is far from solved, even where recall
  is saturated.

## Research systems

| System | Year / venue | Score | Note |
|---|---|---|---|
| Darwin Gödel Machine (Sakana) | ICLR'26 | 6.5 | Keeps code changes only after benchmarks confirm them (SWE-bench 20→50%). Learns against a fixed benchmark, not a live stream. No weight updates. |
| Absolute Zero | NeurIPS'25 | 6 | Self-proposed tasks, verified by executing code. A training recipe, not a deployed agent. |
| MUSE | ACL'26 Findings | 6 | Reflects after each sub-task into hierarchical memory. Transfers to new tasks. No lesson verification. |
| EvolveMem | 2026 preprint | 6 | Undoes changes that cause regressions, but tunes retrieval settings, not knowledge. |
| Harness Continual Learning | Aug 2026 preprint | 5.5 | Keeps a change only if it gains *and* retains old behaviour. Names "harness-level forgetting". Frozen weights. |
| ACE (Agentic Context Engineering) | ICLR'26 | 5.5 | Evolving playbook with helpful/harmful counters and de-duplication. |
| ReMem (Evo-Memory) | 2025, GDM + UIUC | 5.5 | Think / refine / act with pruning, over task streams. |
| LifeMem | EMNLP'26 | 5.5 | Clusters workflows into skills; 13k+ tasks. |
| MindMemOS | 2026 tech report | 5 | "Dreaming" merges memories; versioned skills; no curves. |
| Voyager | TMLR | 5 | Skill library with self-verification. |
| AWM, ExpeL, Dynamic Cheatsheet, ReasoningBank, Training-Free GRPO, CASCADE, Learning on the Job, SCoL | 2023–26 | 4–4.5 | Various lesson and skill memories; little verification. |
| SEAL (MIT) | NeurIPS'25 | 4 | Writes its own fine-tuning data. Its paper reports forgetting. |
| Memento, MemVerse, CLaaS, "LMs Need Sleep" (Google) | 2025–26 | 4 | Case memory, or weight updates without reflection or pruning. |
| A-MEM, Hope / Nested Learning, Titans, Reflexion, TTT-E2E, sleep-time compute | 2023–26 | 1.5–2.5 | Pieces: memory organisation, architecture, in-task reflection. |

## Production systems

| System | Score | What it actually does |
|---|---|---|
| Relai (startup, $6.9M, Jun 2026) | 6.5 *claimed* | Turns failures into fixes and checks each against past environments before release. Vendor numbers only. **Closest competitor in spirit.** |
| Cursor Composer real-time RL (Mar 2026) | 6 | The only production system that updates weights from real outcomes, with a benchmark check before each release (about every 5 h). Centralised, no reflection, humans patched reward hacks, gains about 2–3%. |
| Letta skill learning + sleep-time | 5.5 | Reflect → skill file → reuse, +15.7 on Terminal-Bench 2.0. No verification, frozen weights. |
| Cursor Tab online RL (Sep 2025) | 5 | Policy-gradient updates from accepted and rejected suggestions. |
| Claude Code auto-memory + Auto-Dream | 4.5 | Background dedupe and pruning of memory files. Facts, not outcomes. |
| ChatGPT memory "Dreaming V3" (Jun 2026) | 4 | Background rewrite of the memory store. |
| Codex Memories, Decagon AOPs, Gemini Personal Context, Devin knowledge | 3–4 | Summaries, procedures and preferences. |
| Mem0, Zep, Salesforce Agent Memory, Copilot memory | 1.5–2.5 | Fact storage and update. |
| DeepSeek V4 | 0 | A claim of "online continual learning" is false. |

## Benchmarks

| Benchmark | What it shows |
|---|---|
| LoCoMo, LongMemEval | Recall is saturated (92–96%, vendor self-reported). Not a test of learning. |
| LongMemEval-V2 (May 2026) | Learning from environment experience: best 72.5% against an 86% oracle ceiling. A plain coding agent gets 69%. |
| MemoryAgentBench (ICLR'26) | Multi-hop forget and update ≤ 7%. Every system fails selective forgetting. |
| ContinualSkillBench (Aug 2026) | Explicit skill maintenance is no better than plain in-context learning. |
| AgentMemoryBench (ICLR'26) | No design handles mixed task streams robustly. |
| Evo-Memory / ReMem | Big gains on repetitive embodied tasks, small ones on reasoning. |
| LifelongAgentBench | Plain experience replay helps little and hits context limits. |
| "Ground Truth First" (Jul 2026) | Best at 3 weeks falls to 72% at 9 weeks. Rankings flip with tenure. |
| MemSecBench (Jul 2026) | 84.2% of poison persists; 59.6% exploitation; 50.3% end-to-end attack success; 56.1% selective repair (24 configurations, no defences evaluated). Our suite: [poisoning.md](poisoning.md). |
| TRACE | Weight-level continual learning still forgets. |
| METR time horizons | Each task is a fresh episode; measures no on-the-job learning. |

**Nothing measures:**

- whether a specific lesson caused a gain (ablation);
- cost per outcome over months;
- an agent getting better on a real job over weeks;
- retiring stale skills safely;
- resisting poisoning while staying useful;
- transfer of lessons across domains or models;
- self-reinforcing wrong beliefs.

## Field-level gaps (Metabolism's openings)

1. **C8 is effectively zero.** No system reports months of running on real work.
2. **C7 is disconnected from the memory loop.** Weight learners don't reflect;
   memory learners don't consolidate.
3. **C6 is rare and benchmark-bound.** Most systems rely on the model judging
   itself.
4. **C5 is heuristic and unmeasured.** Later experience can erase earlier gains.
5. **Poisoning is undefended.**

## Lab leader positions

- Karpathy (Oct 2025): about a decade.
- Sutskever (Nov 2025): a major bottleneck; SSI will "learn from deployment".
- Amodei (2025–26): may fall out of pretraining plus RL generalisation.
- Sholto Douglas (Jan 2026): expects it "solved in a satisfying way" in 2026.
- Hassabis (Jan 2026): "not been cracked yet".

## Fact-check of the conversation that started this project

The idea came from a chatbot conversation (not kept in this repo). Several of
its claims were wrong. **Don't reuse them:**

- **Fabricated or not credible:**
  - "Topological AI" (prime-anchored embeddings, "mathematical guarantees
    against forgetting") and "TOPO-2026" come from one author's self-published
    Medium posts and a Hugging Face model card. There is no paper and no peer review.
  - "Daphnia" Go vector DB: doesn't exist.
  - The claim that typesafe-go was "untested" and flagged by a "community
    index": false. It has 63 test files and lint analyzers.
  - Jev `RawQuestion`: doesn't exist. The types are `Choice`, `Score`, `Noul`.
- **Wrong in detail:**
  - Born (pure-Go ML): not zero-dependency, verified only on TinyLlama 1.1B,
    no LoRA support.
  - agent-sdk-go: no Temporal or Restate integration.
  - stash: needs Postgres, and its pipeline has 9 stages, not 8.
  - Gorgonia: dormant since 2024.
  - go-deepseek/deepseek: stale.
  - "Governed Memory": its features were merged from several papers.
- **Architecturally impossible as stated:**
  - DeepSeek's API has no fine-tuning or LoRA.
  - Jev is closed and API-only, so it can't be fine-tuned.
  - V4-Flash is 284B total / 13B active; LoRA on it needs a GPU cluster,
    not a Go binary.
- **Accurate:**
  - Jev basics: typed outputs, 70–500 ms, hosted API.
  - DeepSeek V4 Flash and Pro: open weights under MIT; API IDs `deepseek-flash`
    and `deepseek-v4-pro`.
  - MUSE, Memento, MMAG, Hope, CASCADE, MemVerse, CLARE, MindMemOS, EvolveMem,
    Meta-UCF (modest gains), LifelongAgentBench, Evo-Memory, Karpathy's quote.
  - Eino, Google adk-go, CortexDB, pgvector-go, the Pinecone client,
    cohesion-org/deepseek-go.

## Habit compilation: prior art (checked 2026-10-07)

The question: has anyone compiled an LLM's repeated decisions into a fast
classifier or a set of typed gates? **Yes, in several forms, so the idea
itself is not new.** What has not been found is the combination Phase 5
plans. URLs are in references.md.

**Making repeated LLM work cheap: established.**
- Cascades and routers: FrugalGPT (2023) sends a query to the cheapest
  model first and escalates below a confidence threshold. RouteLLM (2024)
  learns which queries need the strong model. Both work on whole queries.
- Neural caching: Cache & Distil (Ramírez et al., ACL Findings 2024) trains
  a small student online on the LLM's answers, with an active-learning
  policy deciding when the student answers and when to defer. It is the
  closest prior art to habit compilation, on whole user requests rather
  than decisions inside an agent.
- Distilling System 2 into System 1 (Yu, Xu, Weston, Kulikov, 2024)
  fine-tunes the model on its own verified slow answers so it skips the
  reasoning. The "System 2 to System 1" framing in vision.md is theirs too.
- Agentic Plan Caching (NeurIPS 2025) extracts plan templates from
  completed agent runs and reuses them on similar tasks, adapted by a small
  model: 50% lower cost on average.
- Per-step routing inside an agent loop is practice in 2026: small models
  for routine steps, the large model for planning and recovery (StepRouter,
  RSI-Router, AgentCollab). Mostly engineering reports, not evaluations.

**Compiling LLM decisions into rules or code, accepted on held-out data:
established in 2026.**
- RuleChef (July 2026): an LLM proposes rules for a classification task;
  each is accepted only on held-out precision, with human feedback; at
  inference only the rules run.
- Trace2Policy (June 2026): rules recovered from expert traces, compiled to
  Python, refined against validation sets; it replaced the LLM in
  production at 79.6% accuracy.
- On-device agentic operation caches (Gulwani et al., September 2026): an
  assistant's frequent natural-language-to-action requests become
  classification over cached operations.
- These work on standalone tasks or on whole requests, not on recurring
  decisions inside an agent's own loop.

**Learning restrictions from an agent's own traces: established.**
- AgentGuard (September 2026) mines recurring failures from coding-agent
  trajectories into restrict-only behavioural constraints. It does not
  report human approval, pre-deployment verification or fewer model calls.
- AgentSpec, ShieldAgent and GuardAgent enforce safety rules written by
  people or extracted from policy documents, not learned from the agent's
  own decisions.

**What none of them does, together** (the honest claim for Phase 5):
1. The unit is a recurring decision *inside* the agent's loop, found in its
   own episodes ("is this output a compile error or a network error?"), not
   a whole request (Cache & Distil, routers) or a whole plan (APC).
2. It compiles into a typed question set for a calibrated typed model
   (Jev), linted for atomic questions and known failure modes before use.
   The others fine-tune a student, write code rules, or cache outputs.
3. It is accepted by replaying the agent's own past decisions under an
   A/A-calibrated rule, then credited on live turns by coin flip against
   withholding it. None of the works found credits a compiled component on
   randomised live outcomes.
4. It is restrict-only and needs the user's approval in a host with safety
   gates.

Each part has a neighbour; the combination, and above all the live
randomised credit, is what is new. That is a narrower claim than "habit
compilation is new", and the one Phase 5 may make.

**What Phase 5 should take from them:**
- Cache & Distil's deferral policy: margin sampling or query-by-committee
  on the gate's confidence decides when to fall back to the slow model.
- RuleChef's and Trace2Policy's acceptance on held-out data, which the
  nutrient test already does in its own way.
- APC and per-step routing as **the baselines a gate must beat**. Phase 5's
  exit criterion (≥ 50% fewer slow-model calls for a decision at equal or
  better accuracy) should be measured against "route this step to a small
  model" as well as against "always ask the slow model". A gate that only
  matches a small model on that step is not worth its complexity.

## Open research tasks

- [x] Literature search: has anyone compiled repeated LLM decisions into a fast
      classifier or typed-gate set? Yes, in several forms (see "Habit
      compilation: prior art" above); the Phase 5 combination is not found.
- [ ] Close read of Relai and Harness Continual Learning (the closest
      competitors): what exactly do they verify, and how?
- [ ] Pick the public benchmark: τ-bench, ContinualSkillBench or
      LongMemEval-V2. It is an open Phase 0 exit criterion, and the Phase 3
      go/no-go gate needs it.
- [ ] Check Together, Fireworks and Tinker for LoRA on small open models over HTTP.
