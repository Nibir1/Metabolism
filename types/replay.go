package types

// HostProfile describes a host to the engine.
type HostProfile struct {
	Name string `json:"name"`
	// Kinds are the lesson kinds the host can use.
	Kinds []LessonKind `json:"kinds"`
	// Budget bounds how much learned context the host will accept per request.
	Budget Budget `json:"budget"`
}

// Budget bounds a delivery of lessons to a host.
type Budget struct {
	MaxLessons int `json:"max_lessons"`
	MaxChars   int `json:"max_chars"`
}

// ReplayResult is what a host produced when re-running a past episode with no
// side effects (for Helix: planning without executing).
type ReplayResult struct {
	EpisodeID ID     `json:"episode_id"`
	Steps     []Step `json:"steps"`
	Text      string `json:"text,omitempty"`
	Usage     Usage  `json:"usage"`
}

// Preference is a judge's comparison of two replays.
type Preference string

const (
	PreferWith    Preference = "with"
	PreferWithout Preference = "without"
	PreferNeither Preference = "neither"
)

// Verdict is a judge's decision, with how sure it is and why.
type Verdict struct {
	Prefer     Preference `json:"prefer"`
	Confidence float64    `json:"confidence"`
	Reason     string     `json:"reason"`
}

// Selection is the set of lessons delivered for one request.
type Selection struct {
	Lessons []Lesson `json:"lessons"`
	// Held is the lessons eligible but deliberately withheld for the
	// trial's no-lesson arm. Recorded so credit compares like with like.
	Held []ID `json:"held,omitempty"`
}
