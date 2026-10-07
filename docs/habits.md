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

The same test showed native tool-calling works with thinking off, which
would also avoid paying for reasoning tokens. Whether a request needs the
model to think is a **planning-mode habit candidate** for 5b, to be
measured, not switched on.

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
