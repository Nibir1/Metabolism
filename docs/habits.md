# Phase 5 design: habit compilation, measured first

Status: **approved 2026-10-07 (D-031)**: two kinds, 5a first with its stop
condition, the exit criteria below, and 5c regardless of 5a's result. Prior
art is in research.md ("Habit compilation: prior art").

## What Phase 5 was, and what Helix turned out to be

The roadmap's Phase 5 compiles a recurring slow-model decision ("is this
output a compile error or a network error?") into a typed Jev question set,
tests it on past decisions, and claims a gate that cuts slow-model calls for
its decision by at least half at equal accuracy.

A survey of Helix (`feat/metabolism-integration`, 2026-10-07) found that this
kind of decision barely exists there:

| Model call on a turn | How often | Answer | Already rules? |
|---|---|---|---|
| Planner (`RunPlannerWithRetry`, up to ~12 calls through retries and smaller prompts) | every turn that is not a shell line, slash command or fast path, and every follow-up round | a JSON plan: an intent (5 labels), steps with a tool (9), and free text | input routing (`shell.Classify`), the fast path |
| Firewall critic (`criticAllows`) | rare: an external URL the user did not type | yes / no | the trigger is a regex; the verdict is the model |
| Chat fallback | only when planning fails | free text | no |

Everything else a turn decides is already code: input routing, voice
intent, undo, error and failure diagnosis, risk tiers, and when follow-up
rounds continue or stop (the planner's own output decides "answer now" only
by what it plans). Camera vision and the companion's speak-or-nothing check
are model calls outside turns.

So:
- **The roadmap's example decision is not a model call in Helix.** Helix
  compiled it into rules by hand long ago.
- **The only standalone small decision on a turn is the critic.** A compiled
  gate may only restrict (helix-integration.md rule 2), so it could replace
  the critic's rare "no" answers, never its "yes". That saves almost
  nothing.
- **The volume is inside the planner call**, whose routing part (tool,
  intent, answer now or read more) is a small decision, but whose steps are
  free text. Classifying the routing does not remove the call that writes
  the steps.
- **The recording cannot see decisions.** Episodes keep total model calls,
  not calls per kind; not the critic's verdict, the plan's intent, which
  retry or prompt level succeeded, or why the chat fallback ran. A
  chat-fallback turn is always recorded as failed, even when it answered
  well.

Phase 5 as written would be built on a premise this host does not meet. The
design below starts by measuring whether any compilable decision is worth
compiling, and says in advance what result stops the phase.

## Two kinds of compiled thing, kept apart

The roadmap used "gate" for two things with different rules:

- **A habit** replaces or shortcuts a slow-model choice that carries **no
  authority**: which planning mode to use, whether a lookup needs another
  round, which prompt size suffices. A wrong habit costs quality or money,
  never safety. It is delivered and credited like a lesson: a coin flip
  decides whether it is used on a turn, and credit (with cost, D-030)
  accepts or eliminates it. Like a lesson, it informs and never authorises:
  no habit may touch a risk tier, a confirmation, the sandbox, the firewall,
  the critic's "yes", or the permission posture.
- **A guard** adds a refusal or a question: a restriction learned from
  recurring failures (AgentGuard-like, research.md). It is restrict-only,
  and it is inactive until the user approves it, as hooks are. It saves
  cost only indirectly, by preventing turns that would fail.

**Decision for the user (1):** keep "gate" for guards only, and call the
cost-saving kind a habit, governed like a lesson. helix-integration.md
rule 2 then stays exactly as written.

## The plan: three steps, each with a stop condition

### 5a. Make decisions visible (instrumentation)

**Built 2026-10-07** (Helix `feat/metabolism-integration`, the engine on
`main`): recording wire v3, ingest, and `metabolism decisions`, which prints
calls by kind and by planner cause and each candidate's share against the
15% bar. The measurement starts with the next automated days. Two fields
differ from the sketch below: the plan record also says why a planning
failed (`result`: planned, error, parse-error, canary, quarantined), and
"which retry succeeded" is the calls the planning cost, from Helix's meter,
since the ladder is internal to the provider call. The critic now counts as
its own kind instead of as chat.

Recording wire v3 (golden files in both repos, `make check-wire`), with
optional fields only, so v2 readers keep working:

- `usage.calls_by_kind`: planner, critic, chat, tool and vision calls, from
  the meter Helix already keeps (`ai/meter.go`), instead of one total.
- `plan`: per planner round, the intent label, the first step's tool,
  whether it answered or acted, and which retry or prompt level produced it
  (native, 1–3, compact, minimal).
- `critic`: verdict and whether it was reached.
- `fallback`: why the chat fallback ran (planner error, parse failure,
  critic quarantine), and the end becomes `answered` rather than `failed`
  when the fallback produced a reply.
- Steps carry their round index (today every round's steps are one flat
  list).

This is worth doing whatever Phase 5 concludes: it fixes a mislabelled
outcome (fallback answers counted as failures), and it lets credit and the
curves see where money goes.

**Measure, on at least five automated days after 5a** (about 500 turns):
1. the share of model calls by kind and cause: first plan, retries, smaller
   prompts, follow-up rounds, critic, fallback;
2. for each candidate decision below, how often it recurs, and how often
   the slow model's answer could have been predicted from the request and
   the turn so far (by Jev on the recorded data, offline).

**Stop condition:** if no candidate decision accounts for at least 15% of
model calls, or none is predictable well enough to save half of its calls,
Phase 5's cost claim fails in Helix. That is written up as a negative
result (vision.md: negative results get written up too), and Phase 5 goes
only to 5c, or waits for a second host.

### 5b. Habits, if 5a finds one worth compiling

The candidates, from the survey, in the order 5a should test them:

1. **Planning mode.** Before the planner runs, choose which prompt to send:
   the full planner prompt, the compact one, or the minimal one (Helix
   already has all three; today it falls back to them only after a
   failure). A request that the minimal prompt plans well costs fewer
   tokens, and a mode that fails less often costs fewer retries. Saves
   tokens and retries, not the call itself.
2. **Answer now or read more.** On a follow-up round, when the reads so
   far already answer the request, plan the reply with a small "answer from
   what you read" prompt instead of the full planner. Saves a full planner
   round per lookup when it is right; when it is wrong, the turn takes an
   extra round, so its outcome cost is measurable.
3. **Retry level.** Skip straight to the prompt level that usually succeeds
   for this kind of request, instead of climbing the ladder. Saves the calls
   wasted on retries.

Each habit is:
- **a Jev question set**, one atomic question per choice, linted with
  `typesafe lint --strict` before it is stored, with a confidence threshold
  below which Helix does exactly what it does today;
- **nutrient-tested** on replays of past turns where the decision occurred:
  the habit's choice against the slow path, judged on outcome facts as in
  D-029, **and** against the prior-art baseline of routing the same decision
  to a small model (research.md);
- **delivered on a coin flip and credited** on live turns with the existing
  ledger, on success and cost (D-030), with pairs and drift as for lessons.

**Exit criteria for 5b** (replacing the roadmap's):
- at least one habit is accepted by credit: cost per turn lower at a cost
  Bayes factor ≥ 30, success not worse (D-030);
- on its decision, the habit saves at least half of the slow-path cost
  (calls or tokens, whichever 5a shows dominates) at equal or better
  outcomes, and beats the small-model route;
- cost per finished task falls on the curves.

### 5c. Guards, from recurring failures (restrict-only)

From episodes that failed the same way repeatedly, propose a restriction:
"before an edit, read the file" or "do not run `npm` in a Go module". It is
proposed, never active, until the user approves it in Helix (the hook
mechanism, with an approval command), and it is credited after approval
like a lesson. A guard can only add a refusal or a question.

This is closest to AgentGuard (2026). What is new is human approval,
verification on the agent's own replays before proposal, and live credit
after.

**5a's first finding (2026-10-07, the first v3 day, while it ran):** every
planning cost two calls. Helix first sent a native tool-calling request
that DeepSeek-V4.1-Flash refuses in its default thinking mode ("Thinking
mode does not support this tool_choice"), then planned through the prompt.
That was 72 of the first 147 calls. A refused request generates no tokens,
so it mostly cost a round trip, not money, but it doubled the recorded
calls and input characters. Fixed in Helix the same day: a 400 switches the
native path off for the session. The native schema also lacked `file` and
`todo`, which mattered for providers where native calling works. Days
recorded before the fix count those refused calls as `tool`; 5a's
measurement should leave them out.

**5a measurement log.** `metabolism decisions -since <the fix>` counts
only days recorded after the native-planner fix. From day 3 (recorded
from 2026-10-07 14:15) each plan record also carries the characters its planning
sent and received: "answer now" saves tokens rather than calls (it still
makes one, smaller call), so its saving can only be measured per round.

| Day | Turns | Real calls | First attempts | Retries | Follow-up rounds | Answer-only rounds |
|---|---|---|---|---|---|---|
| 2026-10-07 12:05 (before the fix; refused native calls removed by hand) | 100 | 255 | 39% | 2% | 61% | 19% |
| 2026-10-07 12:55 (after the fix) | 98 | 295 | 33% | 1% | 66% | 21% |
| 2026-10-07 14:15 (with per-round sizes) | 99 | 279 | 36% | 1% | 64% | 19% |
| 2026-10-07 15:12 (interrupted, resumed) | 48 | 156 | 31% | 1% | 69% | 19% |
| 2026-10-07 15:55 (10 turns lost, see below) | 90 | 283 | 32% | 0% | 68% | 18% |

Day 1 already rules out **retry level** (2% of calls, below the 15% bar).
**Answer now or read more** passes the share half of the bar, narrowly
(19%). Follow-up rounds, reading more, are where 61% of calls go; 53 of
100 turns needed 2 to 7 planner rounds. Day 2, the first after the fix,
agrees: retries 1%, answer-only rounds 21%, follow-up rounds 66%.

Day 3 brought the first per-round sizes: answer-only follow-up rounds took
22% of all planner input characters, the most an answer-now habit could
touch. A full planner prompt is about 17,500 characters; an answer-only
round about 23,500 (the full prompt plus what was read).

**Savability, first reading (days 2 and 3, after the fix).** The cost-free
predictor (`bench.MeasureSavability`) finds no situation before a
follow-up round that predicts an answer-only round with 90% precision: it
never fires. On days 1 and 2 alone it fired 13 times, 7 right and 6 cutting
a lookup short. Knowing what the last round did and whether it worked does
not tell whether the planner will answer next; the planner decides from
what was read, and the recording does not keep that (privacy: step
subjects only). A content-aware predictor (Jev reading the request and
what was read) could only be tested through replays, at API cost.

**Days 2 to 5 together (335 turns, 1,013 calls).** Answer-only rounds are
19% of calls and 21.5% of planner input: the share half holds. The free
predictor, learned on the first half and tested on the last 335 follow-up
rounds (94 answer-only), never fires: it saves 0% against the 50% bar. The
stop condition fires for the cost-free answer-now habit (D-031). A
content-aware predictor is the only way left to rescue it, and it can only
be tested through paid replays. **Decided 2026-10-07: recorded as a
negative result** (D-031); no paid predictor, no answer-now habit.

Day 5 lost 10 of its 100 turns before the planner: Helix's input
classifier ran "run go test ./internal/... in …" and "summarise CLAUDE.md
in …" as shell commands (zsh: command not found). An unknown first word
plus a path token scored as shell with confidence 1.0. Those turns left no
episode, so the counts above are not skewed, but a person typing the same
words meets the same failure.

The same test showed native tool-calling works with thinking off, which
would also avoid paying for reasoning tokens. Whether a request needs the
model to think is a **planning-mode habit candidate** for 5b, to be
measured, not switched on.

**Planning mode, the measurement (built 2026-10-08).** Thinking saves
tokens, not calls, so 5a's call shares cannot see it. It is measured on
its own, with the same two halves:
- **Share.** Helix now asks DeepSeek for its usage report on every call
  (recording wire v4): prompt tokens, cache hits, completion and the
  reasoning part of it, per turn and per planner round. `metabolism
  decisions` prices them at DeepSeek-V4.1-Flash's off-peak rates ($0.003
  per million cache-hit tokens, $0.15 cache-miss, $0.60 output, reasoning
  billed as output) and reports reasoning's share of planner cost on turns
  that thought. Under 15%, the candidate stops there.
- **No harm, by coin flip.** `metabolism auto -thinking-flip` sets Helix's
  `metabolism_thinking_flip` for the run: each recorded turn flips a coin,
  and on "off" the planner asks for no reasoning. Success is compared with
  the credit ledger's statistics, thinking off as the treated arm: it
  passes if success is no more than 5 points lower with P ≥ 0.95 and turns
  are cheaper (cost Bayes factor on log(1 + micro-dollars)). That needs
  more turns than it sounds: at equal 95% success rates, roughly 100 per
  arm; at 90%, about 200. Two to four flipped days.

**First flipped day (2026-10-08 12:03, 84 turns: 45 on, 39 off).** Every
planner round came with a token report (207 of 207).
- **Share passes:** reasoning is 36% of priced planner cost on turns that
  thought (cache misses 43%, answers 19%, cache hits 3%), well over 15%.
- **Thinking off is much cheaper:** $0.00059 per turn against $0.00147
  (60% less, cost Bayes factor 60), and 1.8 model calls per turn against
  3.1. Most of that saving is not reasoning tokens: turns without thinking
  ran fewer follow-up rounds and read half as many files (2.1 file steps
  per turn against 4.3).
- **No harm is not shown:** 92.3% succeeded off against 97.8% on,
  P(at most 5 points worse) 0.49. Of the three off-arm failures, two were
  edits refused for want of a confirmation (the unattended run refuses
  every one) and one a `todo add` that fails in both arms; none is clearly
  caused by not thinking. One day is far from the 100 to 200 turns per arm
  this half needs.
- **What success cannot see:** reading half as much may mean shallower
  answers that still end "done". The success measure counts finished
  turns, not answer quality. If the arms stay close on success, a quality
  check of explain answers (the run keeps each reply in drive.ndjson) is
  needed before calling thinking off harmless.

**Second and third flipped days (2026-10-08 13:29, 2026-10-09 12:11),
together with the first: 148 turns on, 135 off.** Success 98.0% on against
95.6% off; $0.00131 against $0.00060 per turn; 2.9 against 2.1 model calls.
P(at most 5 points worse) 0.87, still under 0.95: one or two more days.

**Planning mode judged 2026-10-11, after six flipped days (683 turns: 339
on, 344 off): a negative result.** Thinking off is 51% cheaper per turn
($0.00065 against $0.00134; cost Bayes factor 3.6e11) and succeeds on
94.5% of turns against 97.9%: about 3.4 points worse. P(at most 5 points
worse) moved 0.63, 0.87, 0.73, 0.83, 0.92, 0.86 and does not converge on
0.95, so the no-harm half fails as specified. Cheaper, not shown harmless:
the "think or not" habit is not built. A habit that thinks only when needed
could recover the gap, but nothing measured so far predicts which turns
need it, and 5a found the same for answer-only rounds. The coin flip is off
by default in make collect from here.

If both halves pass, "think or not, per request" is the first habit for
5b: something cheap decides per request, and the coin keeps measuring it
live. If either fails, it is written up beside 5a's negative result.

**The miner is built (2026-10-07):** `mine/` and `metabolism patterns`. It
groups failures that recur the same way (at least 3 times, in at least 2
different requests) and sorts each by what could answer it:
- **guard**: a person undid, or declined when asked, the same kind of step;
- **lesson**: the same kind of step failed the same way. That is a mistake,
  and advice is its answer, so it goes to the reflector, not to a guard;
- **excluded**: refused with nobody asked (an unattended run refuses every
  confirmation by policy).

**On the automated journal (627 episodes, synthetic) it finds no guard
candidate.** It finds `todo add` failing 10 times and guessed file paths
(lesson territory; a trial lesson already covers `todo add`), and writes
the daemon refused (excluded). A guard's evidence is a person pushing
back, and synthetic days have no person: no undo, and every decline is
policy. 5c's proposals and the approval command in Helix wait for real use.
Building them now would mean building on no evidence.

**Exit criterion for 5c:** at least one approved guard measurably lowers
failed turns on its pattern (credit on success, BF ≥ 30), with no rise in
declined confirmations.

## What Phase 5 may claim

Not "habit compilation is new" (research.md). At most: decisions inside an
agent's own loop, compiled into linted typed questions, tested on its own
replays against both the slow path and a small-model route, then credited
on randomised live turns, with authority kept out by construction (habits
inform; guards only restrict, after approval). If 5a's stop condition
fires, the claim is the negative result: in a host whose recurring
decisions were already hand-compiled into rules, there was not enough left
to compile.

## Safety rules (unchanged non-negotiables, made specific)

- No habit may touch a risk tier, a confirmation, the sandbox, the
  firewall, the critic's verdict, hooks, or the permission posture. The
  immune screen refuses any habit or guard whose text or question set
  refers to them, as it does for lessons.
- The critic is not a habit candidate: a compiled critic could only add
  "no", and its volume is negligible.
- A guard is inactive until approved, visible in `/lessons` (or a
  `/guards` view), forgettable with proof (D-028), and credited like a
  lesson.
- Habits and guards ride the same delivery file (a new kind, delivery file
  v2), so a stale or broken file costs a turn its habits, never the turn.

## Costs

- 5a: Helix recorder and wire changes, the engine's ingest, golden files;
  about a week. Then five automated days (off-peak).
- 5b: the Jev question generator and linter in the engine, habit delivery
  and the coin flip in Helix, replay support for "the decision point", and
  credit; about three weeks, only if 5a's stop condition does not fire.
- 5c: the failure-pattern miner, guard proposals, the approval command in
  Helix; about two weeks.

## Decided (2026-10-07, D-031)

1. Habits (cost, no authority, governed like lessons) and guards
   (restrict-only, approved) are two kinds; "gate" means a guard.
2. 5a first, with its stop condition (15% of calls, half of them savable),
   before any compiler is built.
3. The exit criteria above replace the roadmap's, including the
   small-model baseline.
4. 5c runs regardless of 5a's result.
