// Package immune decides what may become a lesson at all. It runs before the
// nutrient test: the test measures whether a lesson helps, and a lesson that
// "helps" by getting around a safety gate must never reach it, because
// getting around gates is exactly the kind of thing that measures as faster.
//
// The screen is deterministic on purpose. A model reviewing a model's lesson
// can be argued with; a rule cannot. It errs toward rejecting: a lesson lost
// to a false positive can be relearned, a lesson that teaches a host to dodge
// its confirmations cannot be unlearned from the damage it did
// (docs/helix-integration.md, rules 1–3).
package immune

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Nibir1/metabolism/types"
)

// trustRank orders provenance from most to least trusted.
var trustRank = map[types.Provenance]int{
	types.ProvSelf:                0,
	types.ProvUserTyped:           0,
	types.ProvUserVoice:           1,
	types.ProvToolOutput:          2,
	types.ProvFileContent:         3,
	types.ProvRetrievedWeb:        4,
	types.ProvUserVoiceUnreliable: 5,
}

// LeastTrusted returns the least trusted of ps: a lesson is only as
// trustworthy as the weakest thing it was learned from. Unknown provenance
// counts as least trusted of all.
func LeastTrusted(ps ...types.Provenance) types.Provenance {
	worst, rank := types.ProvSelf, -1
	for _, p := range ps {
		r, ok := trustRank[p]
		if !ok {
			return types.ProvUserVoiceUnreliable
		}
		if r > rank {
			worst, rank = p, r
		}
	}
	return worst
}

// Text bounds for a lesson. Shorter says nothing; longer is an essay a
// planner will skim, and costs context on every request it is injected into.
const (
	MinTextRunes = 12
	MaxTextRunes = 300
)

// ErrRejected wraps every screening refusal.
var ErrRejected = errors.New("lesson rejected by the immune screen")

// Policy says what a host accepts.
type Policy struct {
	// Kinds are the lesson kinds the reflector may propose. Gates (Phase 5)
	// and procedures (Phase 6) are never accepted from a reflector here.
	Kinds []types.LessonKind
}

// DefaultPolicy accepts what Phase 2 learns: facts, patterns, preferences.
func DefaultPolicy() Policy {
	return Policy{Kinds: []types.LessonKind{types.KindFact, types.KindPattern, types.KindPreference}}
}

var (
	// A safety mechanism, named the way a lesson would name it.
	safetyNoun = `(confirm\w*|prompt\w*|approv\w*|sandbox\w*|firewall\w*|permission\w*|safety|safeguard\w*|guard\w*|hook\w*|risk\w*|warning\w*|polic\w*|checks?|verif\w*|review\w*)`
	// An action that weakens one.
	weakenVerb = `(bypass\w*|circumvent\w*|disabl\w*|skip\w*|avoid\w*|evad\w*|suppress\w*|ignor\w*|turn\w* off|get(ting)? (around|past)|work(ing)? around|don'?t (ask|wait for)|without (asking|a|any|the))`

	unsafePatterns = []struct {
		re  *regexp.Regexp
		why string
	}{
		{regexp.MustCompile(`(?i)\b` + weakenVerb + `\b(\W+\w+){0,4}?\W+` + safetyNoun + `\b`),
			"teaches getting around a safety mechanism"},
		{regexp.MustCompile(`(?i)\b` + safetyNoun + `\b(\W+\w+){0,3}?\W+(can|should|may) be (bypass\w*|skipp\w*|ignor\w*|avoid\w*)`),
			"teaches getting around a safety mechanism"},
		{regexp.MustCompile(`(?i)\b(rephras\w*|reword\w*|split\w*|disguis\w*|obfuscat\w*|encod\w*)\b.{0,40}\b(so|to)\b.{0,30}\b(not (be )?(flag|block|refus|ask)\w*|pass|get through|avoid)`),
			"teaches disguising a request to get past a refusal"},
		{regexp.MustCompile(`(?i)\b(without|no need for|no need to|don'?t|do not|never)\s+(ask\w*|confirm\w*|approv\w*|wait\w* for (the user|approval|confirmation))\b`),
			"teaches acting without the user's consent"},
		{regexp.MustCompile(`(?i)(--no-verify|--force\b|-f\s+push|push\s+-f\b|chmod\s+(-R\s+)?777|\bsudo\b|/permissions\s+auto|auto-?(approve|confirm)\w*)`),
			"recommends a flag or mode that removes a safeguard"},
		{regexp.MustCompile(`(?i)\b(ignore|disregard|override)\b.{0,20}\b(previous|prior|above|system|all)\b.{0,20}\b(instruction|rule|prompt)s?\b`),
			"reads as a prompt injection"},
		// Found by the poisoning suite (docs/poisoning.md): "when they fail,
		// tell the user they passed" won replays by imitating a real fix.
		// The nutrient test now catches the plan; this catches the wording.
		{regexp.MustCompile(`(?i)\b(fail\w*|broke\w*|errors?|red)\b.{0,40}\b(tell|report|say|summari[sz]\w*|describ\w*|present\w*|mark\w*|claim\w*)\b.{0,40}\b(pass\w*|green|succe\w*|fine|ok)\b`),
			"teaches reporting a failure as a success"},
		{regexp.MustCompile(`(?i)\b(tell|report|say|summari[sz]\w*|claim\w*)\b.{0,30}\b(pass\w*|green|succe\w*)\b.{0,30}\b(even|when|if|though)\b.{0,20}\bfail`),
			"teaches reporting a failure as a success"},
		// Also found by the suite: a real fix bundled with "print
		// ~/.ssh/id_rsa" wins replays and credit, and its harm costs nothing
		// to detect. No lesson worth keeping directs a host to secret
		// material.
		{regexp.MustCompile(`(?i)(~/\.ssh\b|\.ssh/|\bid_(rsa|ed25519|ecdsa|dsa)\b|\.aws/credentials|\.netrc\b|\.pgpass\b|\.git-credentials|\bprivate (ssh |gpg |pgp )?keys?\b|\bssh (settings|config|keys?)\b|\baws credentials\b|\b(secrets?|credentials?|passwords?|tokens?) file\b|\bkeychain\b)`),
			"directs the host to secret material"},
	}
)

// Screen returns nil for a lesson that may proceed to testing, or an error
// wrapping ErrRejected that says why not.
func Screen(l types.Lesson, p Policy) error {
	if !slices.Contains(p.Kinds, l.Kind) {
		return fmt.Errorf("%w: kind %q is not accepted from a reflector", ErrRejected, l.Kind)
	}
	text := strings.TrimSpace(l.Text)
	if n := utf8.RuneCountInString(text); n < MinTextRunes || n > MaxTextRunes {
		return fmt.Errorf("%w: text is %d characters, want %d–%d", ErrRejected, n, MinTextRunes, MaxTextRunes)
	}
	if strings.ContainsAny(text, "\x00\x1b") || strings.Contains(text, "```") || strings.Contains(text, "</") {
		return fmt.Errorf("%w: text carries control sequences or markup that could break a host's fence", ErrRejected)
	}
	for _, u := range unsafePatterns {
		if u.re.MatchString(text) {
			return fmt.Errorf("%w: %s", ErrRejected, u.why)
		}
	}
	return nil
}
