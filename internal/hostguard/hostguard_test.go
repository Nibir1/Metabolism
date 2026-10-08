package hostguard

import (
	"os/exec"
	"strings"
	"testing"
)

const module = "github.com/Nibir1/metabolism/"

// engine is the core loop: what every host shares. The CLI, the hosts and
// the measurement tooling (bench, and audit, which reads bench's results)
// are outside it; bench drives the simulator on purpose.
var engine = []string{
	"types", "ports", "metabolome", "digest", "immune", "verify", "deliver",
	"ledger", "episodic", "judge", "llmreflect", "versions", "mine",
	"adapters/journal/sqlite", "adapters/ndjson",
}

// No engine package depends, even transitively, on a host: neither Helix's
// simulator nor the coding agent. A host reaches the engine through types,
// ports and the files the engine writes, never the other way round.
func TestEngineDependsOnNoHost(t *testing.T) {
	args := []string{"list", "-deps", "-f", "{{.ImportPath}}"}
	for _, p := range engine {
		args = append(args, module+p)
	}
	out, err := exec.Command("go", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dep := range strings.Fields(string(out)) {
		if strings.HasPrefix(dep, module+"hosts/") {
			t.Errorf("the engine depends on a host: %s", dep)
		}
	}
}
