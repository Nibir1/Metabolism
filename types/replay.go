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
	// Rounds and End describe a multi-round replay (D-021): how many planner
	// rounds there were and how it ended (ReplayEnd*). Zero values: a
	// first-plan replay.
	Rounds int    `json:"rounds,omitempty"`
	End    string `json:"end,omitempty"`
}

// How a multi-round replay ended.
const (
	// ReplayEndPlanned: one round, or nothing for a replay to run.
	ReplayEndPlanned = "planned"
	// ReplayEndAnswered: it stopped the way the live turn would, with
	// nothing left to answer from.
	ReplayEndAnswered = "answered"
	// ReplayEndBudget: the follow-up rounds ran out while it was still
	// reading.
	ReplayEndBudget = "budget-exhausted"
	// ReplayEndUnexecuted: a round planned a step a replay never runs (a
	// command, a write); the live turn would have acted there.
	ReplayEndUnexecuted = "unexecuted-step"
)

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
