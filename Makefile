.PHONY: all test race lint fmt build baseline-sim clean

all: lint race

test:
	go test ./...

race:
	go test -race -count=1 ./...

lint:
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
	go vet ./...
	golangci-lint run ./...

fmt:
	gofmt -w .

build:
	go build -o bin/metabolism ./cmd/metabolism

# The synthetic baseline: simulated Helix, no lessons. It tests the harness;
# it is NOT the real Helix baseline (see bench/results/README.md).
BASELINE_SIM := bench/results/baseline-helixsim
baseline-sim:
	rm -f $(BASELINE_SIM)/journal.db*
	go run ./cmd/metabolism sim -journal $(BASELINE_SIM)/journal.db -seed 1 -count 1000 -days 56
	go run ./cmd/metabolism curves -journal $(BASELINE_SIM)/journal.db -window 7d \
		-label baseline-helixsim -out $(BASELINE_SIM) | tee $(BASELINE_SIM)/curve.txt
	rm -f $(BASELINE_SIM)/journal.db*

clean:
	rm -rf bin
