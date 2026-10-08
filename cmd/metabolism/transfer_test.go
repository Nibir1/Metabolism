package main

import (
	"testing"

	"github.com/Nibir1/metabolism/types"
)

// Groups come from the workspace path a request names, without trailing
// punctuation, or from the host's project scope.
func TestGrouper(t *testing.T) {
	g, err := grouper(defaultGroupBy)
	if err != nil {
		t.Fatal(err)
	}
	for text, want := range map[string]string{
		"run the tests in Development/Personal/Metabolism/.runs/auto/workspace/Helix.":           "Helix",
		"what does verify do in Development/Personal/Metabolism/.runs/auto/workspace/Metabolism": "Metabolism",
		"how do goroutines work?": "",
	} {
		if got := g(types.Episode{Request: types.Request{Text: text}}); got != want {
			t.Errorf("%q: %q, want %q", text, got, want)
		}
	}
	scope, _ := grouper("scope")
	if got := scope(types.Episode{Scope: types.Scope{Level: types.ScopeProject, Key: "helix-1a2b"}}); got != "project:helix-1a2b" {
		t.Fatalf("scope group %q", got)
	}
	if _, err := grouper("no-group"); err == nil {
		t.Fatal("a pattern without a group must be refused")
	}
}
