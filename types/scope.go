package types

import "fmt"

// ScopeLevel is how widely a lesson applies.
type ScopeLevel string

const (
	// ScopeGlobal applies everywhere the host runs.
	ScopeGlobal ScopeLevel = "global"
	// ScopeMachine applies on one machine ("this Mac's sed needs -i ''").
	ScopeMachine ScopeLevel = "machine"
	// ScopeProject applies in one project ("this repo builds with make").
	ScopeProject ScopeLevel = "project"
)

// Scope locates an episode or lesson. Key names the machine or project and is
// empty for ScopeGlobal. The host chooses keys; the engine only compares them.
type Scope struct {
	Level ScopeLevel `json:"level"`
	Key   string     `json:"key,omitempty"`
}

// Validate reports whether s is well formed.
func (s Scope) Validate() error {
	switch s.Level {
	case ScopeGlobal:
		if s.Key != "" {
			return fmt.Errorf("global scope takes no key, got %q", s.Key)
		}
	case ScopeMachine, ScopeProject:
		if s.Key == "" {
			return fmt.Errorf("%s scope needs a key", s.Level)
		}
	default:
		return fmt.Errorf("unknown scope level %q", s.Level)
	}
	return nil
}
