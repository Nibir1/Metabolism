# Decision log

Newest first. Each entry records the decision, the reason, and what would
change it. Don't reverse a decision without adding a new entry that says why.

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
