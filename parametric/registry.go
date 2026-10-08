package parametric

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Registry is the adapters trained so far and which one is in use, as an
// append-only log (registry.ndjson), like the journal: using a version or
// rolling back appends an entry and never rewrites one.
type Registry struct {
	Dir string // .runs/train
}

// Version is one trained adapter.
type Version struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"` // the adapter's directory
	Base     string    `json:"base"` // the base model it was trained on
	At       time.Time `json:"at"`
	Lessons  []string  `json:"lessons"`  // the promoted lessons it consolidates
	Examples int       `json:"examples"` // training examples
	Eval     *Eval     `json:"eval,omitempty"`
}

type entry struct {
	Kind    string    `json:"kind"` // add | use | rollback
	At      time.Time `json:"at"`
	Version *Version  `json:"version,omitempty"`
	Name    string    `json:"name,omitempty"` // for use and rollback: the version now in use ("" for none)
	Reason  string    `json:"reason,omitempty"`
}

func (r Registry) log() string { return filepath.Join(r.Dir, "registry.ndjson") }

func (r Registry) read() ([]entry, error) {
	f, err := os.Open(r.log())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var es []entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	for sc.Scan() {
		var e entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return nil, fmt.Errorf("%s: %w", r.log(), err)
		}
		es = append(es, e)
	}
	return es, sc.Err()
}

func (r Registry) append(e entry) error {
	if err := os.MkdirAll(r.Dir, 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(r.log(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return json.NewEncoder(f).Encode(e)
}

// State is the registry folded: every version, the one in use, and the
// ones used before it, most recent last.
type State struct {
	Versions []Version
	Active   string   // "" when no adapter is in use
	History  []string // earlier active versions, for rollback
}

// Load folds the log.
func (r Registry) Load() (State, error) {
	es, err := r.read()
	if err != nil {
		return State{}, err
	}
	var s State
	for _, e := range es {
		switch e.Kind {
		case "add":
			s.Versions = append(s.Versions, *e.Version)
		case "use":
			s.History = append(s.History, s.Active)
			s.Active = e.Name
		case "rollback":
			if n := len(s.History); n > 0 {
				s.History = s.History[:n-1]
			}
			s.Active = e.Name
		}
	}
	return s, nil
}

// Get returns a version by name.
func (s State) Get(name string) (Version, bool) {
	for _, v := range s.Versions {
		if v.Name == name {
			return v, true
		}
	}
	return Version{}, false
}

// Add records a trained version (not yet in use).
func (r Registry) Add(v Version) error {
	s, err := r.Load()
	if err != nil {
		return err
	}
	if _, dup := s.Get(v.Name); dup {
		return fmt.Errorf("adapter version %s exists", v.Name)
	}
	return r.append(entry{Kind: "add", At: v.At, Version: &v})
}

// Use puts a version in use ("" for none: the base model alone).
func (r Registry) Use(name, reason string, now time.Time) error {
	s, err := r.Load()
	if err != nil {
		return err
	}
	if name != "" {
		if _, ok := s.Get(name); !ok {
			return fmt.Errorf("%w: %s", ErrNoVersion, name)
		}
	}
	return r.append(entry{Kind: "use", At: now, Name: name, Reason: reason})
}

// Rollback puts back the version in use before the current one.
func (r Registry) Rollback(reason string, now time.Time) (string, error) {
	s, err := r.Load()
	if err != nil {
		return "", err
	}
	if len(s.History) == 0 {
		return "", errors.New("nothing to roll back to: no adapter was in use before")
	}
	prev := s.History[len(s.History)-1]
	return prev, r.append(entry{Kind: "rollback", At: now, Name: prev, Reason: reason})
}

// Latest is the most recent version with an evaluation.
func (s State) Latest() (Version, bool) {
	for i := len(s.Versions) - 1; i >= 0; i-- {
		if s.Versions[i].Eval != nil {
			return s.Versions[i], true
		}
	}
	return Version{}, false
}
