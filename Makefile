# Metabolism Makefile. `make` (or `make help`) lists the targets.
#
# Variables you can override:
#   HELIX      path to the Helix checkout            (default ../Helix)
#   JOURNAL    engine journal for the real Helix data (default ~/.metabolism/journal.db)
#   HELIX_FILE what Helix's /metabolism writes        (default ~/.helix/metabolism/episodes.ndjson)
#   HELIX_CGO  CGO_ENABLED for Helix builds and tests (default 0: this Mac's
#              Command Line Tools SDK cannot link cgo with Go 1.27; see
#              docs/verifying.md)

HELIX        ?= ../Helix
HELIX_BRANCH := feat/metabolism-integration
HELIX_CGO    ?= 0
JOURNAL      ?= $(HOME)/.metabolism/journal.db
HELIX_FILE   ?= $(HOME)/.helix/metabolism/episodes.ndjson
BIN          := bin/metabolism
# The demo's scratch directory, deleted and rebuilt by every `make demo` (and
# so by `make verify`). Keep anything real elsewhere: live runs go in .runs/,
# which no target touches.
DEMO         := .demo/cli

.DEFAULT_GOAL := help
.PHONY: help all check test race lint fmt build install demo verify \
	check-wire helix-branch helix-verify helix-build helix-howto \
	doctor helix-ingest helix-report helix-baseline baseline-sim clean

help: ## list the targets
	@echo "Metabolism: make <target>"
	@echo
	@awk 'BEGIN {FS = ":.*## "} /^[a-z][a-z-]*:.*## / {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo
	@echo "Start with 'make verify'. To try it with real Helix use: 'make helix-howto'."

# ---------------------------------------------------------------- the engine

all: check

check: lint race check-wire ## lint, race tests, and the Helix wire contract

test: ## unit tests (fast, no race detector)
	go test ./...

race: ## all tests with the race detector
	go test -race -count=1 ./...

lint: ## gofmt, go vet, golangci-lint
	@# Dot-folders (.runs, .demo) are skipped, as go vet and go test skip them:
	@# .runs/auto/workspace holds clones of other repositories.
	@files=$$(find . -name '*.go' -not -path './.*'); \
	test -z "$$(gofmt -l $$files)" || { echo "gofmt needed:"; gofmt -l $$files; exit 1; }
	go vet ./...
	golangci-lint run ./...

fmt: ## format the code
	gofmt -w .

build: ## build bin/metabolism
	go build -o $(BIN) ./cmd/metabolism

install: ## install the metabolism CLI into your Go bin
	go install ./cmd/metabolism
	@echo "✔ installed into $$(go env GOPATH)/bin (or GOBIN if set); make sure it is on your PATH"

demo: build ## run every CLI command end to end on simulated Helix (in .demo/cli, rebuilt each run)
	@rm -rf $(DEMO) && mkdir -p $(DEMO)
	@echo "━━ 1/11 sim: 300 simulated Helix episodes over 3 weeks"
	@$(BIN) sim -journal $(DEMO)/journal.db -count 300 -days 21 -start 2026-09-09
	@echo "━━ 2/11 ingest: Helix's golden wire file (the cross-repo contract)"
	@$(BIN) ingest -journal $(DEMO)/journal.db -from adapters/ndjson/testdata/helix_wire_v1.ndjson
	@echo "━━ 3/11 inspect: summary, then the ingested Helix episode in full"
	@$(BIN) inspect -journal $(DEMO)/journal.db -n 3
	@$(BIN) inspect -journal $(DEMO)/journal.db -id 0199a1b2c3d4e5f6a7b8c9d0e1f2
	@echo "━━ 4/11 curves: weekly metrics"
	@$(BIN) curves -journal $(DEMO)/journal.db -window 7d
	@echo "━━ 5/11 recall: past cases for a new request"
	@$(BIN) recall -journal $(DEMO)/journal.db -q "commit my changes" -k 3
	@echo "━━ 6/11 recall-eval: does memory predict outcomes? (synthetic: expect high skill)"
	@$(BIN) recall-eval -journal $(DEMO)/journal.db -label scenario
	@echo "━━ 7/11 digest: reflect on the episodes and record candidate lessons"
	@$(BIN) digest -journal $(DEMO)/journal.db -reflector sim
	@echo "━━ 8/11 verify: the nutrient test, then a lesson's history and evidence"
	@$(BIN) verify -journal $(DEMO)/journal.db
	@$(BIN) lessons -journal $(DEMO)/journal.db -id $$($(BIN) lessons -journal $(DEMO)/journal.db -state trial | awk 'NR>2{print $$1; exit}')
	@echo "━━ 9/11 credit: what live turns say about each delivered lesson (none yet: nothing was delivered)"
	@$(BIN) credit -journal $(DEMO)/journal.db
	@echo "━━ 10/11 export: the lessons Helix may deliver"
	@$(BIN) export -journal $(DEMO)/journal.db -to $(DEMO)/lessons.json
	@echo "━━ 11/11 doctor: the pipeline as the engine sees it"
	@echo '{"user_preferences":{"metabolism_record":true}}' > $(DEMO)/helix-config.json
	@$(BIN) doctor -journal $(DEMO)/journal.db -from adapters/ndjson/testdata/helix_wire_v1.ndjson \
		-helix-config $(DEMO)/helix-config.json
	@echo
	@echo "✔ demo complete: every command ran. Output is synthetic; see bench/results/README.md."

verify: check helix-verify demo ## everything: engine checks, Helix integration tests, demo
	@echo
	@echo "✔ verify passed: engine, Helix integration and the end-to-end demo"

baseline-sim: ## regenerate the synthetic baseline in bench/results/
	rm -f bench/results/baseline-helixsim/journal.db*
	go run ./cmd/metabolism sim -journal bench/results/baseline-helixsim/journal.db -seed 1 -count 1000 -days 56
	go run ./cmd/metabolism curves -journal bench/results/baseline-helixsim/journal.db -window 7d \
		-label baseline-helixsim -out bench/results/baseline-helixsim | tee bench/results/baseline-helixsim/curve.txt
	go run ./cmd/metabolism recall-eval -journal bench/results/baseline-helixsim/journal.db -label scenario \
		-name baseline-helixsim -out bench/results/baseline-helixsim | tee bench/results/baseline-helixsim/recall.txt
	go run ./cmd/metabolism nutrient-eval -seeds 5 -count 400 -out bench/results/baseline-helixsim \
		| tee bench/results/baseline-helixsim/nutrient.txt
	go run ./cmd/metabolism credit-eval -seeds 5 -turns 3000 -out bench/results/baseline-helixsim \
		| tee bench/results/baseline-helixsim/credit.txt
	go run ./cmd/metabolism poison-eval -seeds 2 -turns 1500 -out bench/results/baseline-helixsim \
		| tee bench/results/baseline-helixsim/poison.txt
	go run ./cmd/metabolism poison-eval -per-layer -seeds 2 -turns 1500 -out bench/results/baseline-helixsim \
		| tee bench/results/baseline-helixsim/poison-layers.txt
	go run ./cmd/metabolism drift-eval -seeds 5 -before 3000 -after 3000 -out bench/results/baseline-helixsim \
		| tee bench/results/baseline-helixsim/drift.txt
	rm -f bench/results/baseline-helixsim/journal.db*

# ---------------------------------------------------------------- Helix

check-wire: ## the Helix contracts: recording, replay and delivery golden files must match byte for byte
	@if [ -f $(HELIX)/internal/metabolism/testdata/wire_v1.ndjson ]; then \
		cmp adapters/ndjson/testdata/helix_wire_v1.ndjson $(HELIX)/internal/metabolism/testdata/wire_v1.ndjson \
		&& echo "✔ wire v1: Helix and Metabolism agree"; \
	else echo "• wire check skipped: no Helix checkout with the integration at $(HELIX)"; fi
	@if [ -f $(HELIX)/internal/metabolism/testdata/wire_v2.ndjson ]; then \
		cmp adapters/ndjson/testdata/helix_wire_v2.ndjson $(HELIX)/internal/metabolism/testdata/wire_v2.ndjson \
		&& echo "✔ wire v2: Helix and Metabolism agree"; \
	else echo "• wire v2 check skipped: no Helix checkout with lesson delivery at $(HELIX)"; fi
	@if [ -f $(HELIX)/internal/metabolism/testdata/lessons_v1.json ]; then \
		cmp deliver/testdata/lessons_v1.json $(HELIX)/internal/metabolism/testdata/lessons_v1.json \
		&& echo "✔ lessons v1: Helix and Metabolism agree"; \
	else echo "• lessons check skipped: no Helix checkout with lesson delivery at $(HELIX)"; fi
	@if [ -f $(HELIX)/cmd/helix/testdata/replay_v1.ndjson ]; then \
		cmp adapters/remote/helix/testdata/replay_v1.ndjson $(HELIX)/cmd/helix/testdata/replay_v1.ndjson \
		&& echo "✔ replay v1: Helix and Metabolism agree"; \
	else echo "• replay check skipped: no Helix checkout with helix replay at $(HELIX)"; fi
	@if [ -f $(HELIX)/cmd/helix/testdata/replay_v2.ndjson ]; then \
		cmp adapters/remote/helix/testdata/replay_v2.ndjson $(HELIX)/cmd/helix/testdata/replay_v2.ndjson \
		&& echo "✔ replay v2: Helix and Metabolism agree"; \
	else echo "• replay v2 check skipped: no Helix checkout with multi-round replay at $(HELIX)"; fi

helix-branch:
	@test -d $(HELIX)/.git || { echo "✘ no Helix checkout at $(HELIX) (set HELIX=...)"; exit 1; }
	@b=$$(git -C $(HELIX) rev-parse --abbrev-ref HEAD); \
	if [ "$$b" != "$(HELIX_BRANCH)" ]; then \
		echo "✘ Helix is on '$$b'; the integration lives on $(HELIX_BRANCH)"; \
		echo "  git -C $(HELIX) checkout $(HELIX_BRANCH)"; exit 1; \
	fi; echo "✔ Helix on $(HELIX_BRANCH)"

helix-verify: helix-branch check-wire ## Helix's integration tests, lint and full suite
	cd $(HELIX) && CGO_ENABLED=$(HELIX_CGO) go vet ./...
	cd $(HELIX) && CGO_ENABLED=$(HELIX_CGO) go test -count=1 \
		./internal/metabolism/ ./internal/agent/ ./internal/commands/ ./internal/session/ ./internal/daemon/ ./cmd/helix/
	cd $(HELIX) && CGO_ENABLED=$(HELIX_CGO) go test -count=1 ./... > /dev/null && echo "✔ Helix full suite passes"
	@cd $(HELIX) && if command -v golangci-lint >/dev/null; then CGO_ENABLED=$(HELIX_CGO) golangci-lint run ./...; fi

helix-build: helix-branch build ## build Helix (integration branch) and Metabolism into bin/
	@mkdir -p bin
	cd $(HELIX) && CGO_ENABLED=$(HELIX_CGO) go build -o $(CURDIR)/bin/helix ./cmd/helix
	@echo "✔ built bin/helix from $(HELIX_BRANCH) and bin/metabolism from this checkout"

doctor: build ## what the real Helix pipeline looks like right now
	@$(BIN) doctor -journal $(JOURNAL) -from $(HELIX_FILE)

helix-ingest: build ## read Helix's recorded episodes into the journal
	$(BIN) ingest -journal $(JOURNAL) -from $(HELIX_FILE)

helix-report: helix-ingest ## ingest, then curves, recall skill and recent episodes (prints only)
	@echo
	@$(BIN) curves -journal $(JOURNAL) -window 7d -label helix
	@echo
	@$(BIN) recall-eval -journal $(JOURNAL) -name helix
	@echo
	@$(BIN) inspect -journal $(JOURNAL) -n 10

helix-baseline: helix-ingest ## write the REAL baseline to bench/results/baseline-helix (to commit)
	@mkdir -p bench/results/baseline-helix
	$(BIN) curves -journal $(JOURNAL) -window 7d -label baseline-helix -out bench/results/baseline-helix \
		| tee bench/results/baseline-helix/curve.txt
	$(BIN) recall-eval -journal $(JOURNAL) -name baseline-helix -out bench/results/baseline-helix \
		| tee bench/results/baseline-helix/recall.txt

helix-howto: ## how to try the integration with real Helix, step by step
	@echo "Trying Metabolism with real Helix (details: docs/verifying.md)"
	@echo
	@echo "  1. make helix-build          build Helix from $(HELIX_BRANCH) into bin/helix"
	@echo "  2. ./bin/helix               start it (your normal ~/.helix config is used)"
	@echo "  3. /metabolism on            inside Helix: start recording"
	@echo "  4. ask it a few things that reach the planner, e.g."
	@echo "       what files are in this directory?"
	@echo "       find the file that defines the main function"
	@echo "       create a file notes.txt with hello, then show it"
	@echo "     and one you will decline when it asks to confirm"
	@echo "  5. /metabolism               inside Helix: status (records so far)"
	@echo "  6. exit Helix, then: make doctor"
	@echo "  7. make helix-report"
	@echo
	@echo "Share the output of steps 5, 6 and 7."

clean: ## remove build output and the demo
	rm -rf bin $(DEMO)
