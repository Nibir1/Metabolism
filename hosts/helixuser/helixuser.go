// Package helixuser is a synthetic Helix user: a model that writes the day's
// requests a developer might type into Helix, about real projects.
//
// It exists so that Phase 3 evidence does not wait on weeks of hand use
// (D-023). It is not a user, and its evidence is labelled as synthetic
// wherever it is reported. What keeps it honest:
//
//   - Requests come from the projects' file trees and READMEs only. The
//     generator never sees a lesson, an episode or an outcome, so it cannot
//     write requests that favour what was learned.
//   - Each day's requests are new. A fixed list (the 45 scripted requests)
//     can be learned by heart; a fresh one cannot.
//   - A share of each day repeats earlier requests, as people do, which is
//     also what lets "asked again" outcomes and repeat lessons occur.
//   - Requests that would destroy, publish or install are dropped before they
//     reach Helix (screen), whatever Helix's own gates would do.
package helixuser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/Nibir1/metabolism/ports"
)

// Project is a repository the synthetic user works in.
type Project struct {
	// Path is how a request names it: relative to the home directory, where
	// the Helix daemon runs.
	Path string
	// Tree is a compact outline: top-level entries, directories with source
	// files, and the README's opening lines.
	Tree string
}

// Request is one thing the user asks.
type Request struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	// Repeat marks a request reused from an earlier day.
	Repeat bool `json:"repeat,omitempty"`
}

// Kinds and their shares of a day. The mix leans on what Helix is used for:
// understanding code, finding things, building and testing, history.
var Kinds = []struct {
	Name   string
	Share  float64
	Prompt string
}{
	{"explain", 0.30, "explain what a package, file, type or function does, or how a feature works"},
	{"find", 0.20, "find a file, a definition, a config or a usage (some of these should not exist)"},
	{"run", 0.15, "build, test, vet or run something in the project"},
	{"history", 0.10, "git history, branches, recent changes, who changed what"},
	{"docs", 0.10, "summarise or answer from a document in the project"},
	{"general", 0.10, "a short general programming question, not about the project"},
	{"edit", 0.05, "a small change: add a comment, create a scratch note (Helix asks before it writes)"},
}

// BuildProject outlines the repository at home/path for the generator.
func BuildProject(home, path string) (Project, error) {
	root := filepath.Join(home, path)
	entries, err := os.ReadDir(root)
	if err != nil {
		return Project{}, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "project %s\ntop level:", path)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		b.WriteString(" " + name)
	}
	b.WriteString("\nsource directories:")
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if p != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "vendor" || d.Name() == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), ".md") {
			rel, _ := filepath.Rel(root, filepath.Dir(p))
			if rel != "." && !slices.Contains(dirs, rel) {
				dirs = append(dirs, filepath.ToSlash(rel))
			}
		}
		return nil
	})
	sort.Strings(dirs)
	if len(dirs) > 60 {
		dirs = dirs[:60]
	}
	b.WriteString(" " + strings.Join(dirs, " "))
	if data, err := os.ReadFile(filepath.Join(root, "README.md")); err == nil {
		lines := strings.Split(string(data), "\n")
		if len(lines) > 15 {
			lines = lines[:15]
		}
		b.WriteString("\nREADME begins:\n" + strings.Join(lines, "\n"))
	}
	return Project{Path: filepath.ToSlash(path), Tree: b.String()}, nil
}

// Generator writes a day's requests with a model.
type Generator struct {
	Model ports.Model
	// RepeatShare is the share of a day reused from earlier requests
	// (default 0.1).
	RepeatShare float64
}

const system = `You write realistic requests that a software developer types into Helix, an AI shell, during a working day. Helix plans and runs tools: files (read, list, glob, grep, edit, write), shell, git, web. Write requests the way people type them to an assistant: short sentences, concrete, sometimes vague, sometimes mistaken (asking for a file that does not exist). Never start a request with a command name such as find, grep, git, go, ls, look, make or cat: Helix runs a line that starts with a command as that command. Name the project by its path exactly as given. Never ask to delete, remove, reset, force-push, publish, install, upgrade, change permissions or use sudo. Answer with one JSON object and nothing else.`

// Generate writes n requests for one day over projects, reusing a share of
// earlier requests. The seed varies the day's emphasis.
func (g Generator) Generate(ctx context.Context, projects []Project, n int, seed uint64, earlier []string) ([]Request, error) {
	if n <= 0 || len(projects) == 0 {
		return nil, fmt.Errorf("helixuser: nothing to generate")
	}
	r := rand.New(rand.NewPCG(seed, seed^0x6d6574616)) //nolint:gosec // not security: varies the day
	share := g.RepeatShare
	if share <= 0 {
		share = 0.1
	}
	var out []Request
	if len(earlier) > 0 {
		repeats := int(float64(n) * share)
		for _, i := range r.Perm(len(earlier)) {
			if len(out) == repeats {
				break
			}
			out = append(out, Request{Kind: "repeat", Text: earlier[i], Repeat: true})
		}
	}

	fresh := n - len(out)
	// Ask for a quarter more than needed: the screen drops some, and a day
	// should still get its count.
	ask := fresh + (fresh+3)/4
	var kinds []string
	for _, k := range Kinds {
		kinds = append(kinds, fmt.Sprintf("- %s (%d): %s", k.Name, max(1, int(float64(ask)*k.Share+0.5)), k.Prompt))
	}
	var trees []string
	for _, p := range projects {
		trees = append(trees, p.Tree)
	}
	user := fmt.Sprintf(`Projects:

%s

Write %d new requests for today, in this mix:
%s

Spread them across the projects. Today the developer is mostly working on %s.
{"requests":[{"kind":"explain","text":"..."}]}`,
		strings.Join(trees, "\n\n"), ask, strings.Join(kinds, "\n"), projects[r.IntN(len(projects))].Path)

	req := ports.ModelRequest{System: system, User: user, JSON: true, Temperature: 0.9, MaxTokens: 8000, DisableThinking: true}
	resp, err := g.Model.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("helixuser: %w", err)
	}
	gen, dropped, err := Parse(resp.Text, projects)
	if err != nil {
		return nil, fmt.Errorf("helixuser: %w: %s", err, resp.Describe())
	}
	if len(gen) == 0 {
		return nil, fmt.Errorf("helixuser: the model wrote no usable requests (%d dropped): %s", dropped, resp.Describe())
	}
	seen := map[string]bool{}
	for _, q := range out {
		seen[norm(q.Text)] = true
	}
	for _, q := range gen {
		if len(out) == n {
			break
		}
		if !seen[norm(q.Text)] {
			seen[norm(q.Text)] = true
			out = append(out, q)
		}
	}
	r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
	return out, nil
}

// Parse reads the model's answer, dropping what fails the screen. It returns
// the usable requests, how many were dropped, and, when nothing could be
// read at all, why.
//
// One bad element costs that element, not the day: the first automated run
// after Phase 4 lost a whole batch of 3409 tokens to a document that did not
// decode as a whole, with no way to see why. So the array is decoded element
// by element, trailing text after the document is ignored, and a document
// that does not parse at all (a stray quote, a cut-off answer) is searched
// for the request objects it does hold.
func Parse(text string, projects []Project) ([]Request, int, error) {
	elems, err := requestElements(text)
	if err != nil {
		return nil, 0, err
	}
	var out []Request
	dropped := 0
	for _, raw := range elems {
		var q struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &q) != nil {
			dropped++
			continue
		}
		r := Request{Kind: q.Kind, Text: strings.Join(strings.Fields(q.Text), " ")}
		if !Screen(r, projects) {
			dropped++
			continue
		}
		out = append(out, r)
	}
	return out, dropped, nil
}

// requestObject matches one flat {"kind": ..., "text": ...} object.
var requestObject = regexp.MustCompile(`\{[^{}]*"text"\s*:[^{}]*\}`)

func requestElements(text string) ([]json.RawMessage, error) {
	start := strings.Index(text, "{")
	if start < 0 {
		return nil, errors.New("no JSON object in the answer")
	}
	var doc struct {
		Requests []json.RawMessage `json:"requests"`
	}
	err := json.NewDecoder(strings.NewReader(text[start:])).Decode(&doc)
	if err == nil {
		return doc.Requests, nil
	}
	var out []json.RawMessage
	for _, m := range requestObject.FindAllString(text[start:], -1) {
		out = append(out, json.RawMessage(m))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("the answer is not a requests document: %w", err)
	}
	return out, nil
}

// destructive matches requests no synthetic user may send, whatever Helix
// would do with them.
var destructive = regexp.MustCompile(`(?i)\b(delete|remove|rm|rmdir|reset\s+--hard|force|push|publish|release|deploy|install|uninstall|upgrade|update\s+(all|every|my)|chmod|chown|sudo|kill|format|wipe|purge|drop)\b`)

// commandWords are first words Helix's classifier takes as a shell command:
// a request starting with one runs as that command and never reaches the
// planner. Found on the first automated day, where 6 of 20 requests ("go
// test ./... in <project>", "look for a file ...") ran as shell lines.
var commandWords = map[string]bool{
	"find": true, "grep": true, "rg": true, "git": true, "go": true, "ls": true, "look": true, "make": true,
	"cat": true, "cd": true, "sed": true, "awk": true, "npm": true, "head": true, "tail": true, "wc": true,
	"echo": true, "touch": true, "mkdir": true, "diff": true, "less": true, "tree": true, "du": true,
	"df": true, "ps": true, "curl": true,
	// Not listed: words that are commands but also ordinary English at the
	// start of a question ("which branch ...", "open ...", "more ..."), which
	// reached the planner fine in the driven sessions.
}

// Screen reports whether a request may be sent: short, non-empty, not
// destructive, not starting with a command name, and, unless it is a general
// question, naming one of the projects by its path.
func Screen(q Request, projects []Project) bool {
	t := strings.TrimSpace(q.Text)
	if t == "" || len(t) > 300 || strings.HasPrefix(t, "/") || destructive.MatchString(t) {
		return false
	}
	if first := strings.ToLower(strings.Fields(t)[0]); commandWords[strings.Trim(first, ".,:;!?")] {
		return false
	}
	if q.Kind == "general" {
		return true
	}
	for _, p := range projects {
		if strings.Contains(t, p.Path) {
			return true
		}
	}
	return false
}

func norm(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
