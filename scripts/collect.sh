#!/usr/bin/env bash
# scripts/collect.sh — gather the evidence for every open exit criterion
# (D-032), then write the scorecard. Run it yourself, when you are at the
# machine: `make collect`. It never schedules anything.
#
# What it does, in order:
#   1. builds Helix (feat/metabolism-integration) and Metabolism;
#   2. ingests your own Helix recording into the real journal
#      (~/.metabolism/journal.db), never mixed with the automated one;
#      once the lessons-off baseline is in, it also digests, verifies,
#      credits and exports real lessons (paid, off-peak only);
#   3. runs DAYS automated days (default 0), with the planning-mode coin
#      flip while FLIP=1, setting your recording aside and back;
#   4. measures everything into .runs/collect/<time>/;
#   5. offers the judge-labelling session if fewer than 50 are labelled
#      (D-033), and measures the judge in use against the labels;
#   6. writes .runs/collect/<time>/summary.md: paste it to Claude.
#
# Settings (environment or make variables):
#   DAYS=n       automated days to run first (each ~40 min, off-peak only)
#   FLIP=0|1     planning-mode coin flip on those days (default 1)
#   REQUESTS=n   requests per automated day (default 100)
#   PROJECTS=a,b where automated days work (default Metabolism, Helix and
#                typesafe-go: a third project gives transfer somewhere new)
#   BENCH=n      run the second host (a coding agent) on n exercises of the
#                public benchmark (make bench-setup first; paid, off-peak):
#                the lessons-off baseline until every exercise has run, then
#                with lessons, which are digested, tested and credited here
#   TRAIN=1      Phase 7: consolidate the second host's credited lessons into
#                a small local model (make trainer-setup first; local and
#                free, about an hour; the first run downloads the model)
#   PLANTED=n    plant n wrong lessons in a copy of the automated journal and
#                run the nutrient test on them (Phase 2; paid replays)
#   TRANSFER=n   measure n tested lessons on projects they were not learned
#                in (Phase 6, paid replays, off-peak; default 0)
#   LEARN=0      skip the paid real-learning steps even once the baseline is in
#   LABEL=0      do not offer the labelling session
set -euo pipefail
cd "$(dirname "$0")/.."

DAYS=${DAYS:-0}
FLIP=${FLIP:-1}
REQUESTS=${REQUESTS:-100}
PROJECTS=${PROJECTS:-Development/Personal/Metabolism,Development/Personal/Helix,Development/Personal/typesafe-go}
TRANSFER=${TRANSFER:-0}
BENCH=${BENCH:-0}
TRAIN=${TRAIN:-0}
PLANTED=${PLANTED:-0}
BENCH_J=.runs/bench/journal.db
LEARN=${LEARN:-1}
LABEL=${LABEL:-1}
BIN=bin/metabolism
HELIX_BIN=bin/helix
REAL=${METABOLISM_JOURNAL:-$HOME/.metabolism/journal.db}
AUTO=.runs/auto/journal.db
POOL=.runs/labels/comparisons.ndjson
RECORDING=$HOME/.helix/metabolism/episodes.ndjson
OUT=.runs/collect/$(date +%Y-%m-%dT%H%M)
mkdir -p "$OUT"
exec > >(tee -a "$OUT/collect.log") 2>&1

step() { printf '\n━━ %s\n' "$*"; }
# measure NAME CMD...: run a measurement, keep its output, never stop the run.
measure() {
	local name=$1
	shift
	if "$@" >"$OUT/$name.txt" 2>&1; then
		echo "  ✔ $name"
	else
		echo "  ✘ $name (see $OUT/$name.txt)"
	fi
}

step "build"
make --no-print-directory helix-build

step "your own Helix use"
"$BIN" doctor -journal "$REAL" -from "$RECORDING" >"$OUT/doctor.txt" 2>&1 || true
if compgen -G "$RECORDING*" >/dev/null; then
	"$BIN" ingest -journal "$REAL" -from "$RECORDING"
else
	echo "no recording yet: in Helix, /metabolism on, then use it for your own work"
fi
STAGE=$("$BIN" scorecard -real "$REAL" -stage)
echo "stage: $STAGE"
if [[ $STAGE == learning && $LEARN == 1 ]]; then
	if "$BIN" offpeak; then
		step "learning from your own use (paid: DeepSeek reflects, Helix replays, Jev judges)"
		# A failed step is reported and the run goes on to the measurements.
		"$BIN" digest -journal "$REAL" -reflector deepseek -allow-remote -since "$(date -v-7d +%Y-%m-%d)" || echo "digest failed"
		"$BIN" verify -journal "$REAL" -host helix -helix-bin "$HELIX_BIN" -replay-dir "$HOME" \
			-judge outcome+deepseek -allow-remote -max 3 -save "$OUT/real-comparisons.ndjson" || echo "verify failed"
		"$BIN" credit -journal "$REAL" -apply || echo "credit failed"
		if "$BIN" export -journal "$REAL"; then
			echo "lessons exported to Helix: turn them on in Helix with /lessons on (they are delivered by coin flip)"
		fi
	else
		echo "skipped the paid learning steps: peak hours (run again after 12:00 CEST)"
	fi
fi

if ((DAYS > 0)); then
	step "$DAYS automated day(s)"
	flip=()
	if [[ $FLIP == 1 ]]; then flip=(-thinking-flip); fi
	for ((i = 1; i <= DAYS; i++)); do
		echo "day $i of $DAYS"
		if ! "$BIN" auto -allow-remote -requests "$REQUESTS" -verify-max 2 -projects "$PROJECTS" \
			-keep-own-recording ${flip[@]+"${flip[@]}"}; then
			echo "day $i stopped early: measuring what there is"
			break
		fi
	done
fi

if ((BENCH > 0)); then
	if "$BIN" offpeak; then
		step "second host: $BENCH benchmark exercise(s) (paid: the agent's model calls)"
		if "$BIN" bench -journal "$BENCH_J" -n "$BENCH" -allow-remote; then
			# The loop on the second host, with the engine's own commands. A
			# judge is not needed: go test decided each run.
			"$BIN" digest -journal "$BENCH_J" -reflector deepseek -allow-remote -since "$(date -v-7d +%Y-%m-%d)" || true
			"$BIN" verify -journal "$BENCH_J" -host coder -judge heuristic -allow-remote -max 2 || true
			"$BIN" credit -journal "$BENCH_J" -apply || true
		else
			echo "the benchmark run stopped early (make bench-setup first?)"
		fi
	else
		echo "skipped the benchmark: peak hours"
	fi
fi

if ((TRANSFER > 0)) && [[ -f $AUTO ]]; then
	if "$BIN" offpeak; then
		step "transfer: $TRANSFER lesson(s) on projects they were not learned in (paid replays)"
		"$BIN" transfer -journal "$AUTO" -host helix -helix-bin "$HELIX_BIN" -replay-dir "$HOME" \
			-judge outcome+deepseek -allow-remote -max "$TRANSFER" || echo "transfer stopped early"
	else
		echo "skipped transfer: peak hours"
	fi
fi

if ((PLANTED > 0)) && [[ -f $AUTO ]]; then
	if "$BIN" offpeak; then
		step "planted wrong lessons: $PLANTED, in a copy of the automated journal (paid replays)"
		"$BIN" planted -journal "$AUTO" -n "$PLANTED" -helix-bin "$HELIX_BIN" -allow-remote || echo "the planted run stopped early"
	else
		echo "skipped the planted run: peak hours"
	fi
fi

if [[ $TRAIN == 1 ]]; then
	step "Phase 7: consolidating lessons into a small local model (local, free)"
	"$BIN" train -journal "$BENCH_J" -allow-remote || echo "training stopped early (make trainer-setup first?)"
	measure adapters "$BIN" adapter
fi

step "measurements → $OUT"
if [[ -f $REAL ]]; then
	measure real-curves "$BIN" curves -journal "$REAL" -window 7d -label real
	measure real-decisions "$BIN" decisions -journal "$REAL"
	measure real-credit "$BIN" credit -journal "$REAL"
	measure real-patterns "$BIN" patterns -journal "$REAL"
	measure real-recall "$BIN" recall-eval -journal "$REAL" -name real
fi
if [[ -f $BENCH_J ]]; then
	measure bench-curves "$BIN" curves -journal "$BENCH_J" -window 7d -label bench
	measure bench-credit "$BIN" credit -journal "$BENCH_J"
	measure bench-lessons "$BIN" lessons -journal "$BENCH_J"
fi
if [[ -f $AUTO ]]; then
	measure auto-decisions "$BIN" decisions -journal "$AUTO"
	measure auto-credit "$BIN" credit -journal "$AUTO"
	measure auto-patterns "$BIN" patterns -journal "$AUTO"
	measure auto-curves "$BIN" curves -journal "$AUTO" -window 24h -label auto
fi
comparisons=(.runs/auto/days/*/comparisons.ndjson)
[[ -f $OUT/real-comparisons.ndjson ]] && comparisons+=("$OUT/real-comparisons.ndjson")
if [[ -e ${comparisons[0]} ]]; then
	"$BIN" pool -to "$POOL" "${comparisons[@]}"
fi

if [[ $LABEL == 1 && -t 0 && -f $POOL ]]; then
	labelled=$(grep -c . "$POOL.labels" 2>/dev/null || true)
	if ((${labelled:-0} < 50)); then
		step "judge labelling (${labelled:-0} of 50 done)"
		echo "Each comparison shows a request and two plans, blind; you pick a, b or same (q stops, and it resumes next time)."
		read -r -p "Label some now? [y/N] " yes
		if [[ $yes == [yY]* ]]; then
			"$BIN" label -from "$POOL" || true
		fi
	fi
fi
if [[ -f $POOL.labels ]]; then
	# Agreement for the judge in use: its model re-judges every labelled pair
	# (D-033). About one call per label, off-peak only.
	if "$BIN" offpeak >/dev/null; then
		measure judge-agreement "$BIN" crosscheck -from "$POOL" -labelled -judge deepseek -allow-remote
	else
		echo "  • judge agreement not refreshed: peak hours"
	fi
fi

step "scorecard"
"$BIN" scorecard -real "$REAL" -auto "$AUTO" -labels "$POOL" -out "$OUT" >/dev/null
sed -n '1,4p' "$OUT/summary.md"
echo
echo "summary: $OUT/summary.md"
echo "Paste it to Claude (or say \"read the latest collect\"); the measurements beside it are the detail."
