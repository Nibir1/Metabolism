// Package deliver writes the lessons a host may use, and is the reference
// for how a host selects among them (Phase 3, D-019).
//
// Delivery is a file, like recording (D-013): `metabolism export` writes
// every trial and accepted lesson, each with the probability the host should
// deliver it on a turn it applies to. The host reads the file, flips the
// coins and records the result on the episode (Exposure, Withheld). Nothing
// is called on the host's hot path, so a missing, stale or broken file costs
// a turn its lessons and never the turn itself.
//
// testdata/lessons_v1.json is pinned byte for byte by a golden file in Helix
// (`make check-wire`).
package deliver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/Nibir1/metabolism/ledger"
	"github.com/Nibir1/metabolism/metabolome"
	"github.com/Nibir1/metabolism/types"
)

// Version is the delivery file format version.
const Version = 1

// Delivery probabilities (D-020). A trial lesson is a coin flip, so its
// withheld turns are as many as its delivered ones. An accepted lesson keeps
// a 10% holdout, so its credit keeps being measured after acceptance.
const (
	PTrial    = 0.5
	PAccepted = 0.9
)

// File is the delivery file.
type File struct {
	V           int       `json:"v"`
	GeneratedAt time.Time `json:"generated_at"`
	Lessons     []Lesson  `json:"lessons"`
}

// Lesson is one deliverable lesson. Text goes into the host's prompt (as
// data); Evidence and Credit are for the user (/lessons) and never do.
type Lesson struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	State string `json:"state"`
	Kind  string `json:"kind"`
	Scope Scope  `json:"scope"`
	// P is the probability of delivering the lesson on a turn it applies to.
	P        float64 `json:"p"`
	Evidence string  `json:"evidence,omitempty"`
	Credit   string  `json:"credit,omitempty"`
}

// Scope is types.Scope on the wire.
type Scope struct {
	Level string `json:"level"`
	Key   string `json:"key,omitempty"`
}

// Build lists every trial and accepted lesson, accepted first, oldest first
// within a state: that is the order a host fills its budget in.
func Build(m *metabolome.Metabolome, tallies map[types.ID]*ledger.Tally, now time.Time) File {
	f := File{V: Version, GeneratedAt: now.UTC(), Lessons: []Lesson{}}
	for _, state := range []types.LessonState{types.StateAccepted, types.StateTrial} {
		ls := m.Lessons(state)
		slices.SortStableFunc(ls, func(a, b types.Lesson) int { return a.CreatedAt.Compare(b.CreatedAt) })
		for _, l := range ls {
			e, _ := m.Get(l.ID)
			p := PTrial
			if state == types.StateAccepted {
				p = PAccepted
			}
			f.Lessons = append(f.Lessons, Lesson{
				ID: string(l.ID), Text: l.Text, State: string(l.State), Kind: string(l.Kind),
				Scope: Scope{Level: string(l.Scope.Level), Key: l.Scope.Key}, P: p,
				Evidence: lastEvidence(e.Evidence), Credit: creditLine(tallies[l.ID]),
			})
		}
	}
	return f
}

func lastEvidence(evs []types.Evidence) string {
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Passed() {
			return fmt.Sprintf("%s %s: %s", evs[i].Kind, evs[i].At.UTC().Format("2006-01-02"), evs[i].Summary)
		}
	}
	return ""
}

func creditLine(t *ledger.Tally) string {
	if t == nil || t.Exposed+t.Withheld == 0 {
		return "no live turns yet"
	}
	e, w := t.Rate()
	return fmt.Sprintf("succeeded %.0f%% of %d turns delivered, %.0f%% of %d withheld (P(better) %.2f)",
		100*e, t.Exposed, 100*w, t.Withheld, t.PBetter())
}

// Write saves f atomically (temp file, then rename), 0600 in a 0700
// directory, so a host never reads half a file.
func Write(path string, f File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lessons-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Selection is the result of selecting for one turn.
type Selection struct {
	Delivered []types.ID
	Withheld  []types.ID
}

// Select is the reference selection a host implements (Helix does, in Go,
// without importing this package).
//
//  1. Keep the lessons that apply to ep (types.Scope.Covers).
//  2. Fill the budget: accepted lessons first, in file order, then trial
//     lessons in a fresh random order each turn. Without the shuffle, trial
//     lessons past the budget would never reach a turn and never be measured.
//  3. Flip a coin with each budgeted lesson's P: delivered or withheld.
//
// A lesson that did not fit the budget is in neither list, so the withheld
// turns stay a fair comparison: each one is a turn where the lesson would
// have been delivered but for the coin.
func Select(f File, ep types.Episode, budget types.Budget, r *rand.Rand) Selection {
	var accepted, trial []Lesson
	for _, l := range f.Lessons {
		sc := types.Scope{Level: types.ScopeLevel(l.Scope.Level), Key: l.Scope.Key}
		if !sc.Covers(ep) {
			continue
		}
		if l.State == string(types.StateAccepted) {
			accepted = append(accepted, l)
		} else {
			trial = append(trial, l)
		}
	}
	r.Shuffle(len(trial), func(i, j int) { trial[i], trial[j] = trial[j], trial[i] })

	var s Selection
	n, chars := 0, 0
	for _, l := range append(accepted, trial...) {
		size := len(l.Text) + 3 // "- " and a newline, as the host renders it
		if (budget.MaxLessons > 0 && n == budget.MaxLessons) || (budget.MaxChars > 0 && chars+size > budget.MaxChars) {
			continue
		}
		n, chars = n+1, chars+size
		if r.Float64() < l.P {
			s.Delivered = append(s.Delivered, types.ID(l.ID))
		} else {
			s.Withheld = append(s.Withheld, types.ID(l.ID))
		}
	}
	return s
}

// Read loads a delivery file, refusing unknown fields and other versions.
func Read(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	var f File
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return File{}, fmt.Errorf("%s: %w", path, err)
	}
	if f.V != Version {
		return File{}, fmt.Errorf("%s: delivery file version %d, this build reads %d", path, f.V, Version)
	}
	return f, nil
}
