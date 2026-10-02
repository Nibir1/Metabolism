# Decision log

Newest first. Each entry records the decision, the reason, and what would
change it. Don't reverse a decision without adding a new entry that says why.

---

### D-018 · 2026-10-02 · The nutrient test on a noisy host: noise floor, calibrated tests, asymmetric bars
**Supersedes the decision rule in D-015.**

**What happened:** the first nutrient test on real Helix replays (Jev judge)
eliminated all four real lessons for "regressions". The saved comparisons
showed the lessons working: the task-list lesson won its three relevant
episodes at 0.96 to 0.99. The regressions were noise: Helix's planner plans
the same request differently each time, and the judge confidently picked
between two equivalent plans (two paraphrases of the same answer at 0.60; a
relative versus absolute path at 0.52). D-015 counted only losses on past
successes, failed a lesson on a single one, and relied on "identical plans
are a tie", which never happens with a nondeterministic planner. The
simulator's deterministic replays could not have revealed any of this.

**Decision:**
- **Noise floor.** Each episode's baseline is replayed twice and judged
  against itself (A/A), once per run, cached. r = (decisive A/A + 1) /
  (A/A + 2). On a deterministic host the baselines are identical and cost
  no judge call.
- **Calibrated tests.** If a lesson did nothing, each comparison would be a
  decisive win with probability r/2, a decisive loss with r/2, and a tie
  otherwise. `p_help`, `p_harm` and the "worse where it applies" p-value are
  binomial tails under that null. Past-success outcomes are counted both
  ways.
- **Asymmetric bars.** Promotion to trial is reversible (Phase 3 credit keeps
  testing), so it needs p ≤ 0.1 and ≥ 2 decisive wins. Elimination is
  permanent, so "harms past successes" and "makes things worse" need
  p ≤ 0.02 and ≥ 2 decisive losses. Quarantined lessons need ≥ 4 wins and
  p ≤ 0.02.
- **Inconclusive** when the evidence does not clear the noise: the lesson
  stays where it is, never eliminated on a guess.

**Evidence:**
- `TestDecisionsHoldOnANoisyHost`: a simulated noisy host and a judge that
  reads noise as preference, 20 lessons per case, run 3 times. Correct
  lessons were accepted 59/60 and eliminated 1/60. Harmful lessons were
  eliminated 60/60. The irrelevant lesson was eliminated 0/60, inconclusive
  57/60, and promoted to trial 3/60 (the reversible cost of the 0.1 bar).
- The deterministic simulator still scores 100% (`make baseline-sim`).

**Revisit when:** labelled real comparisons show the judge's noise is not
symmetric, or enough real runs exist to tune the levels.

---

### D-017 · 2026-10-01 · Real replay is `helix replay`: plan-only, no conversation, NDJSON
**Decision:**
- The engine replays through a long-running `helix replay` process speaking
  NDJSON (replay wire v1, golden file in both repos, `make check-wire`).
- Helix reuses `/plan`'s pipeline (planner, canary, parse, safety rewrite)
  and executes nothing.
- A replay carries no session, task list or project notes, and lessons ride
  a fenced, bounded `<learned_lessons authority="data-only">` block with
  angle brackets neutralised.
- Replayed steps carry no outcome. Only model judges may judge real plans
  (`checkPair`).
- The adapter sits under `adapters/remote/` and needs `-allow-remote`,
  because every replay is a planner call to the user's provider.

**Why:** a replay must compare the lesson, not the conversation or today's
context, and it must be impossible for it to act.
**Known limits:** first plan only (not the agentic loop); plans in
`-replay-dir`, not the original directory (episodes keep only a hashed project
key); no RAG. A replay is a cleaner comparison than the live turn, not a copy
of it.

---

### D-016 · 2026-10-01 · Models: local by default, remote by flag, keys only from the environment
**Decision:**
- Reflectors and judges speak to a `Model` port. Adapters live under
  `adapters/remote/`; the CLI is the only package allowed to import them
  (`internal/privacyguard`).
- Ollama on loopback is the default and needs no opt-in. An Ollama server on
  another host, DeepSeek and Jev all need `-allow-remote`, and print what they
  are about to send.
- Keys come only from `DEEPSEEK_API_KEY` and `TYPESAFE_API_KEY`. They are
  never flags, never logged, never in an error (tested).
- Model judges see the two plans as A and B in a per-episode hashed order, so a
  judge that favours the first option cannot pass as one that favours lessons.

**Why:** D-008 made local-first a rule. This is where it is enforced in code,
and where key handling is made boring.

---

### D-015 · 2026-10-01 · The nutrient test's decision rule (rule superseded by D-018)
**Decision:** replay *relevant* episodes (the lesson's sources plus similar ones
found by episodic memory, up to 8) and a *regression* sample (up to 6 past
successes in scope that the lesson is not about), each with and without the
lesson. Verdicts below 0.6 confidence count as ties. The outcome is:
- **pass:** ≥ 2 wins, wins ≥ 3× losses, and no regressions;
- **quarantined lessons:** ≥ 4 wins and zero losses;
- **fail:** "changes nothing" (no wins and no losses), any regression, or the
  bar not met;
- **inconclusive:** fewer than 3 relevant comparisons. The lesson stays where
  it is.

Every count goes into the evidence, so the decision can be recomputed by a reader.
**Why:** it is simple enough to audit, and strict where it matters. Harming a
past success is disqualifying however many wins a lesson has.
**Revisit when:** real Helix replays (Phase 2c) produce enough data to calibrate
the thresholds, or when the judge is replaced.

### D-014 · 2026-10-01 · The metabolome is event-sourced from the journal
**Decision:** lesson bodies, lifecycle events and evidence are journal records
(append-only, like everything else). `metabolome` folds them into current
state and is the only code that appends them. The journal refuses events and
evidence for unknown lessons. `metabolome.Move` refuses a move to trial unless
it cites passing nutrient-test evidence for that same lesson.
**Why:** "why is this lesson trusted?" must always be answerable from the
journal alone. Making C6 structural means no code path can promote an
untested lesson. This replaces the separate mutable Store the architecture
sketched.

---

### D-013 · 2026-09-30 · Helix talks to the engine through a file, not a Go import
**Decision:** Helix writes versioned NDJSON (`~/.helix/metabolism/episodes.ndjson`,
wire v1), and `metabolism ingest` reads it into the journal. Helix does not
import the engine's module.
**Why:**
- Helix's build and release stay independent of an unpublished, fast-changing module.
- A local file keeps Helix's telemetry-free guarantee obvious: nothing leaves the
  machine until the user runs ingest.
- The contract is pinned by one golden file kept in both repos
  (`make check-wire`), and both sides decode it with unknown fields refused.

**Revisit when:** Phase 2 needs `Host.Replay` (plan-only replay called by the
engine) and Phase 3 needs lessons delivered into Helix's prompt. Both need a
live call. The likely shape is Helix importing a small, stable `ports`/`types`
module once it is published, or a local socket through Helix's daemon.

---

### D-012 · 2026-09-30 · Helix episode emission moves forward into Phase 0
**Why:** a real Helix baseline can't exist until Helix emits episodes, and
learning can't be judged without one. The Helix-side emission and late outcomes
(helix-integration.md, changes 1–2) are therefore Phase 0 work, with learning
off. Weeks of use then produce `bench/results/baseline-helix/` while Phases 1–2
are built. Until then, the synthetic `baseline-helixsim` checks only that the
harness works.

---

### D-011 · 2026-09-30 · Dropped from the design: Topological AI, Born for training, DeepSeek-API LoRA
**Why:** Topological AI and TOPO-2026 have no credible source. Born has no
LoRA and is verified only on about 1B-parameter models. The DeepSeek API offers
no fine-tuning. See the fact-check in [research.md](research.md).
**Revisit if:** peer-reviewed evidence or new capabilities appear.

### D-010 · 2026-09-30 · A go/no-go gate at Phase 3, with a publication either way
**Why:** ContinualSkillBench and Continual Learning Bench show explicit memory
often doesn't beat plain in-context learning. We must find out early whether
verified lessons do, and a negative result is still valuable.

### D-009 · 2026-09-30 · Measure before learning (Phase 0 is the evaluation harness)
**Why:** without baselines, "it learned" can't be falsified. Every rubric
criterion needs evidence.

### D-008 · 2026-09-30 · Local-first; hosted models are opt-in
**Why:** Helix is telemetry-free by construction, and experience data (shell
history, files) is sensitive. The default reflector and judge are local
(Ollama). DeepSeek and Jev need explicit opt-in.

### D-007 · 2026-09-30 · Lessons inform and never authorise; gates are restrict-only
**Why:** a learning system that can loosen safety is an attack surface. Memory
poisoning is largely undefended (MemSecBench), and Helix's value rests on its
safety guarantees.

### D-006 · 2026-09-30 · The engine is a separate Go module; the Helix adapter lives in Helix
**Why:** keeps the core domain-agnostic. Helix shapes the interfaces; a second
host (Phase 8) proves them ("rule of two").

### D-005 · 2026-09-30 · Helix is the first host (not customer support)
**Why:**
- It is the user's own real project, so it has a guaranteed first user.
- It already emits rich outcome signals.
- It already has a safe replay mode (`/plan`).
- It already has a zero-authority immune architecture.
- Its author already does manual digestion.

Customer support stays a candidate second host.

### D-004 · 2026-09-30 · Target positioning: "prove it learned", not "first with memory"
**Why:** the memory space is crowded (Letta, Mem0, Zep, lab memory features).
The open gaps are verification (C6), consolidation (C7) and endurance (C8)
combined, plus poisoning resistance. See [vision.md](vision.md).

### D-003 · 2026-09-30 · "100% Digestive AI" is defined by the 9-criterion rubric with evidence bars
**Why:** a claim needs a definition to be testable. The rubric is in
[vision.md](vision.md).

### D-002 · 2026-09-30 · Jev via typesafe-go for fast judges and gates
**Why:**
- Typed, calibrated outputs at 70–500 ms and low cost.
- The user's SDK is well tested and has lint analyzers that suit habit
  compilation.

**Constraints:** Jev is closed and API-only (no self-hosting or fine-tuning), and
early access opened 2026-09-15. Every judge must also have a local fallback.

### D-001 · 2026-09-30 · Go for the engine; the only exception is weight training (Phase 7)
**Why:**
- The user's stack, and Helix is Go.
- A single binary, strong concurrency.
- The ecosystem covers orchestration, storage and model clients.

Nothing in pure Go trains LoRA on LLMs, so Phase 7 uses an external trainer
over HTTP or a sidecar.
