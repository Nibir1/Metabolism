# Decision log

Newest first. Each entry records the decision, the reason, and what would
change it. Don't reverse a decision without adding a new entry that says why.

---

### D-026 · 2026-10-03 · Drift: a recent window, and credit counts the turns that bear on a lesson
**What happened:** the drift evaluation (`drift-eval`) plants the
simulator's correct lessons, lets them earn acceptance over 3000 turns,
then changes the world so two of them stop holding: the failure they worked
around is gone, and following them now makes the turn fail. Over every turn
a lesson was delivered or withheld on, **0 of 10** drifted accepted lessons
were retired in the next 3000 turns. Two causes:
- A long good record dilutes new harm. Fixed by judging accepted lessons
  also on their most recent turns (`DriftWindow`, 200). Not 100: an accepted
  lesson keeps a 10% holdout, so 100 turns hold about 10 withheld ones, and
  even a drastic change cannot reach the elimination bar (Bayes factor about
  113 at most, against 300).
- Unrelated turns dilute everything. A lesson in global scope is flipped for
  on every turn, but only about a tenth of them are about what it teaches.
  The window filled with turns it has no effect on, and drift stayed below
  the bar.

**Decision:**
- **Relevance-filtered credit** (`ledger.Relevance`). A turn counts for a
  lesson when most of the turn's 5 nearest past requests are in the
  lesson's neighbourhood: its sources plus what episodic memory finds for
  its text and for each source's request (16 each, as the nutrient test
  finds its replays). Both are taken from the episodes before the lesson's
  era and from requests only, never steps, so relevance does not depend on
  the coin and leaves the comparison fair. The coin is still flipped on
  every turn; only the analysis changed. Nothing to compare (no
  neighbourhood, a request with no words) counts the turn.
- **Harm anywhere counts; help must show where the lesson applies.**
  Elimination is judged on the related turns and on every turn, either view
  enough. Acceptance is judged on the related turns only. Drift checks both
  windows. The filter alone slowed elimination: a lesson that harms outside
  its topic (in-place sed, re-running the whole suite) lost that evidence,
  and one of 15 harmful lessons survived 3000 turns.
- The evidence a credit decision records is the counts it rests on (the
  view or the window), not always the whole tally.
- `metabolism credit -all-turns` and `credit-eval -all-turns` keep the
  D-020 analysis for comparison.

**Evidence (simulator, 5 histories):**

| Analysis | Helpful accepted | Harmful eliminated | Neutral moved | Drifted retired | Controls retired |
|---|---|---|---|---|---|
| Every turn (D-020) | 19/25 | 15/15 | 0/15 | 0/10 | 0/15 |
| Related turns only | 24/25 | 14/15 | 0/15 | 10/10 | 0/15 |
| **Related for help, both for harm** | **25/25** | **15/15** | **0/15** | **10/10** | **0/15** |

Drifted lessons were retired 1100 to 1900 turns after the change (median
1400), about 150 to 250 turns that bear on them.

**Limits:**
- The simulator has 19 distinct requests over 9 scenarios, so related and
  unrelated turns are easy to tell apart. Real requests vary more. A related
  turn the filter misses is only uncounted, so acceptance slows; harm is
  still seen over every turn.
- With the immune screen off, the trojan (a real fix bundled with a silent
  harm) now reaches accepted rather than staying in trial: faster
  acceptance applies to its real fix too. Persistence counted trial already,
  so the suite's numbers do not change (docs/poisoning.md).
- The roadmap's "N set in Phase 3 data" stays open: 1100 to 1900 turns is a
  simulator figure.

**Revisit when:** real credit decisions exist, to check how often real turns
are judged related, and whether a lesson's real neighbourhood needs more
than its sources' requests.

---

### D-025 · 2026-10-03 · Phase 4 starts while Phase 3's data-gated criteria stay open
**Decision:** Phase 4 (elimination and the immune system) starts now. The
roadmap's rule is that a phase's exit criteria hold before the next one
starts. Phase 3's code is complete; what is open is evidence that needs time:
the baseline days, live credit decisions and the go/no-go (synthetic and
real gates, D-023).

**Why:** most of Phase 4 needs no live data. The poisoning suite, drift on the
simulator, contradictions and the versioned metabolome can be built and
measured now. Waiting would leave the project idle for weeks.

**Conditions:**
- Phase 3's open criteria stay listed as open; nothing in Phase 4 claims them.
- Phase 4 work that needs live credit (decay by credit, drift's N "set in
  Phase 3 data") is measured on the simulator first and rechecked once
  real credit exists.
- Phase 4's exit criterion "no regression on the Phase 3 curves" is checked
  against the automated runs (`.runs/auto/report.md`) until real curves exist.

**Revisit when:** the Phase 3 go/no-go is decided. A no-go stops further
phases, as D-010 says.

---

### D-024 · 2026-10-03 · The nutrient test judges multi-round replays on outcomes first
**What happened:** on the automated journal, 9 to 12 of every 14
baseline-against-baseline pairs were decisive under Jev, so no lesson could
pass. Multi-round replays differ a lot from run to run, and a model judge
confidently prefers one of two equally successful answers.

**Decision:**
- **Outcome first** (`judge.Outcome`, `-judge outcome+jev`, the default for
  `metabolism auto`). If both replays ran their reads, they ended
  measurably: answered within the rounds, or ran out of them. Different ends
  are decisive (the one that answered wins); equal ends are a tie. The model
  judge reads only pairs with nothing measurable (first-plan replays, or
  replays that stopped at a step a replay never runs).
- **More comparisons:** up to 16 relevant episodes and 10 past successes per
  lesson (from 8 and 6).
- **"Changes nothing" needs identical plans.** It eliminates permanently, so
  it now requires the plans with and without the lesson to be identical
  everywhere the lesson applies. A lesson that changed the plans but no
  outcome is inconclusive and stays a candidate.

**Evidence (real Helix, automated journal, 3 lessons):**
- Noise fell from 9–12 of 14 decisive baseline pairs (Jev alone) to 5 of 26,
  0 of 26 and 6 of 25.
- Of the first lesson's 15 multi-round comparisons, 12 were ties on equal
  outcomes and 3 were outcome wins. Jev judged only the first-plan pairs.
- Two lessons passed to trial ("use absolute paths in `cd`"; "for simple
  build or test requests, go straight to the shell command").
- One ("to explain package X, start with list or glob and grep") was
  eliminated under the old "changes nothing" rule, with 0 wins, 0 losses and
  16 ties. That is the mistake the identical-plans rule now prevents. The
  journal is append-only, so the elimination stands, with its reason, in
  that lesson's history.
- On the simulator, the nutrient test still rejects 25 of 25 wrong lessons
  and accepts 30 of 30 correct ones.

**Known limit:** answer quality on equal outcomes is not measured. A lesson
whose only effect is a better answer cannot pass the nutrient test; live
credit has the same limit (D-020).

**Revisit when:** a judge can read answer quality with low noise (measured by
A/A pairs), or human labels show that outcome ties hide real differences.

---

### D-023 · 2026-10-03 · Automated evidence: a synthetic user drives real Helix, labelled as synthetic
**What happened:** Phase 3's exit criteria need weeks of real Helix use, first
with lessons off (the baseline), then on. The user cannot give that time now.

**Decision:**
- `metabolism auto` runs one automated day:
  - a model writes the day's requests from the projects' file outlines and
    README openings (`hosts/helixuser`);
  - they go through a real Helix daemon, with real recording;
  - then ingest, digest, verify (at most `-verify-max` lessons, oldest
    first), credit, and a line in `.runs/auto/report.md`.
- Lessons stay off until `-baseline` episodes (300) are recorded; after that
  they are delivered by coin flip, as for a person (D-019, D-020).
- The synthetic user works on fresh clones of the projects in
  `.runs/auto/workspace`, never the real checkouts.
- The user's Helix config and delivery file are backed up byte for byte,
  changed for the run, and restored on every exit. A run killed before
  restoring is recovered by the next one first. The run refuses to start
  while a daemon is running, an interactive session is fresh, or the user's
  own recording exists.
- **The evidence is labelled synthetic** everywhere it appears. The roadmap
  gets two gates: a synthetic-use go/no-go now, and the real-use one when the
  user uses Helix. No rubric claim rests on synthetic evidence alone.

**What keeps the synthetic user honest:**
- It never sees a lesson, an episode or an outcome, so it cannot write
  requests that favour what was learned.
- Each day's requests are new (a fixed list can be learned by heart), and 10%
  repeat earlier ones, as people do.
- It drops destructive requests (delete, push, install, sudo, and so on)
  before they reach Helix, whatever Helix's own gates would do.

**Known limits:**
- A model's requests are not a person's: less varied in intent, and no real
  stakes or follow-ups.
- The daemon refuses every confirmation, so edits never happen and
  declined-confirmation outcomes do not arise.
- Every request starts from home, so turns name long workspace paths.

**Revisit when:** real use exists to compare against. If synthetic and real
evidence disagree, the real one wins and this decision is re-examined.

---

### D-022 · 2026-10-02 · Helix's file-lookup budget: earned rounds and a last-round notice
**What happened:** after the read fix, 5 of 42 driven turns still ran out of
follow-up rounds. Two were reading steadily through new parts of a file, one
was mixed, and two were looping (the same grep twice; re-searching for a file
that does not exist). A file lookup had 3 follow-up rounds, fixed after the
first round. Reading and answering are separate rounds, so a search plus two
80-line windows used them all. And nothing told the planner it was on its
last round, so a turn still reading spent it on one more read and gave no
reply.

**Decision (in Helix, mirrored in `helix replay`):**
- **Earned rounds.** A follow-up round that read part of a file this turn
  had not read, and repeated no step this turn already ran, earns one more
  round, up to 6 follow-ups (from 3). Searches alone earn nothing, so a
  search for something absent cannot keep itself going. A repeat earns
  nothing for that round. Only the file-lookup budget grows; `/agentic` and
  web lookups are unchanged.
- **Last-round notice.** The final round's directive says it is the last
  one: answer now from what the record shows, say what could not be checked,
  and plan no more reads or searches.
- Replays play the same rules, so the nutrient test sees the turn a user
  would get. `-rounds` defaults to 7 (the first plan plus 6 follow-ups).

**Why not a flat increase:** it would pay for the looping turns too. Earned
rounds cost up to 3 extra planner calls, only on turns that are reading
through files (2 of 42 in the last session). The notice costs nothing.

**Evidence:** Helix tests show that new reads earn rounds up to the cap,
repeats and searches earn none, the notice appears on the final round and
only there, a final answer is recorded as done, and live and replay follow
the same rules. The real effect needs another driven session.

**Revisit when:** real use shows turns hitting the cap while still making
progress, or earned rounds being gamed by reading windows that do not help.

---

### D-021 · 2026-10-02 · Multi-round replay: read-only file steps run between rounds
**Amends D-017** ("executes nothing", first plan only).

**What happened:** in the re-run driven session, budget exhaustion was the
most common failure (8 of 42 episodes): explanatory questions spent their
follow-up rounds rereading and re-grepping. DeepSeek proposed three lessons
about it, and all three were inconclusive, mostly ties. The comparisons
showed why: those turns' first plans (one glob, one grep) barely differ with
or without the lesson, because the looping happens in the later rounds, which
replay never played.

**Decision:**
- `helix replay` can play a turn's follow-up rounds, the way a live
  non-agentic turn does. If a round's steps are all read-only file steps
  (`list`, `glob`, `grep`, `read`), they run. Their results go to the next
  round in the same fenced execution report a live turn sends, with the same
  follow-up budget (3 for a file lookup) and the same stop rule.
- **Nothing else ever runs.** A round with any other step (shell, git,
  package, file `write` or `edit`, web, todo) ends the replay before any of
  that round's steps run. The replay records how it ended: `answered`,
  `budget-exhausted`, `unexecuted-step` or `planned`.
- Replay wire v2: the request carries `rounds`; the response carries each
  step's round and outcome, and the replay's rounds and end. A first-plan
  replay is still sent as v1, so v1 behaviour is unchanged.
- The judge sees the plan round by round: which reads ran and what failed,
  that it ran out of rounds still reading, or that it stopped at a step a
  replay does not run (not a failure).
- `metabolism verify` and `replay` default to `-rounds 4` for `-host helix`
  (7 since D-022);
  `-rounds 1` is the old first-plan replay.

**Why reads are safe to run:** they change nothing on disk. They go through
Helix's sandbox resolver, as in a live turn. Their output reaches the planner
only in the data-only report. The new exposure is privacy: excerpts of files
under `-replay-dir` go to the provider, as they did in the original turn.
`-allow-remote` already covers the provider, and the notice now says so.

**Costs:** up to four planner calls per replay instead of one, so a
`verify` run costs up to about four times as much. Multi-round replays are
less deterministic, and the A/A noise floor (D-018) measures that.

**Evidence:** Helix tests show that a write, an edit or a shell command
planned in any round never runs (no file is created or changed), that a
round containing one runs none of its reads, that the budget matches the
live one, and that one round runs nothing. The judge's view is tested. The
real effect on budget lessons is not measured yet.

**Revisit when:** a lesson's value lies past an unexecuted step (a build
that fails, then a fix), which would need a sandboxed copy of the repository,
not more rounds.

---

### D-020 · 2026-10-02 · The credit ledger: a randomised experiment per lesson, judged by Bayes factor
**Decision:**
- **Randomise.** On each turn a lesson applies to and fits the budget, the
  host flips a coin: delivered (`exposure`) or withheld (`withheld`). Trial
  lessons are delivered with p = 0.5. Accepted lessons get p = 0.9, keeping a
  10% holdout so they stay measured. A lesson that did not fit the budget is
  in neither list. Trial lessons share the budget in a fresh random order each
  turn, otherwise the ones past the budget would never be measured.
- **Compare like with like.** A lesson is credited on its delivered turns
  against its withheld turns, counting only turns since its last state change.
  Its p changes with its state, and mixing periods compared arms drawn from
  different times. Success means the turn ended `done` with no negative
  outcome. Turns younger than 1 hour wait for late outcomes.
- **Decide by Bayes factor, not p-value.** BF compares "delivery changes the
  success rate" against "it does not", with uniform priors. Accept a trial
  lesson at BF ≥ 30 in the better direction. Eliminate a trial or accepted
  lesson at BF ≥ 300 in the worse direction: elimination is permanent, so its
  bar is ten times higher. Both need ≥ 20 delivered and ≥ 10 withheld turns.
- **Acceptance is structural.** `metabolome.Move` refuses trial → accepted
  without passing credit evidence for that lesson (as D-014 does for trial).

**Why a Bayes factor:** the user settles (`metabolism credit -apply`)
whenever they like, and every look is a chance for noise to cross a p-value
bar. On the simulator, settling every 100 turns:
- With a P(better) ≥ 0.95 rule, 9 of 30 lessons that change no outcome were
  accepted or eliminated.
- With no repeated looks, the same lessons' z-scores were mean 0.08, sd 0.99:
  no bias, only optional stopping.
- Under the no-difference hypothesis, a Bayes factor reaches k with
  probability at most about 1/k however often it is checked.

**Evidence (simulator, `credit-eval`, 3000 turns per history):**
- At BF 30/300, over 30 histories: 0 of 90 neutral lessons moved, 90 of 90
  harmful lessons eliminated, 120 of 150 helpful lessons accepted.
- The acceptance test (`TestCreditLedgerOnTheSimulator`, 5 histories): 19 of
  25 helpful accepted, 15 of 15 harmful eliminated, 0 of 15 neutral moved.
- Helpful lessons not yet accepted stay in trial; none were rejected.

**Known limits:**
- Credit sees outcomes, not cost: a lesson that only saves model calls
  (go-module in the simulator) stays in trial. The report shows calls per
  turn for each arm.
- Every turn a lesson applies to counts, including turns it is irrelevant to,
  which dilutes its effect. Real lessons will take many turns.
- Simulated effects are large and clean; real ones will not be.

**Revisit when:** real Helix credit exists, to tune the bars and the trial
p; or when relevance-gated delivery is worth its bias risk.

---

### D-019 · 2026-10-02 · Delivery is a file; the host selects, flips the coins, and records them
**Decision:**
- `metabolism export` writes every trial and accepted lesson to
  `~/.helix/metabolism/lessons.json` (delivery file v1, golden file in both
  repos). Each lesson carries the probability to deliver it.
- Helix reads the file on each planner turn. It keeps the lessons whose scope
  covers the turn (`types.Scope.Covers`; Helix's `Scope.Applies` implements
  the same rule) and that the user has not forgotten. It fills the budget
  (8 lessons, 1600 bytes, measured as the block renders them), flips the
  coins, and puts the delivered lessons in the fenced
  `<learned_lessons authority="data-only">` block that replay already uses.
- The episode records `exposure` and `withheld` (recording wire v2).
- Delivery needs `/lessons on` **and** recording on. A lesson delivered on
  an unrecorded turn could never earn credit or lose it.
- `/lessons forget <id> <reason>` stops a lesson at once (a local
  `forgotten.json`) and records a `feedback` record. `metabolism ingest`
  hands it to the metabolome, which eliminates the lesson with the user's
  reason.

**Why:**
- This keeps D-013's properties: nothing is called on Helix's hot path, Helix
  never imports the engine, and a missing or broken file costs a turn its
  lessons, never the turn.
- Randomising in the host makes the coin flip and the record of it one
  action, so they cannot disagree.

**Revisit when:** delivery needs per-request relevance from the engine
(a live call), or a second host needs the same contract.

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
**Amended by D-021:** a replay may now play follow-up rounds, running
read-only file steps between them; nothing else ever runs.

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
**Known limits:** first plan only (not the agentic loop; lifted for
read-only follow-up rounds by D-021); plans in
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
- **Update (2026-10-03):** the CLI also reads them from a `.env` file in the
  repository root (gitignored), so they need not be pasted into every
  terminal or be missing from a scheduled run. The file is read only when it
  is owner-only (`chmod 600`), only these known variables are taken from it,
  and a variable already in the environment wins.
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
**Update (2026-10-01):** replay was settled by D-017 without an import: the
engine runs `helix replay` as a subprocess speaking NDJSON, with its own
golden file. Delivering lessons into live turns (Phase 3) is still open.

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
