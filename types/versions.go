package types

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Tag names a version of the metabolome (D-028). The metabolome is a fold of
// the journal, so a version is a journal position: folding every record up
// to Seq gives the lessons as they were. A tag adds a name, a note and a
// fingerprint of that fold.
type Tag struct {
	ID   ID        `json:"id"`
	At   time.Time `json:"at"`
	Name string    `json:"name"`
	Note string    `json:"note,omitempty"`
	// Seq is the last journal record the version includes.
	Seq int64 `json:"seq"`
	// Fingerprint is a hash of every lesson's ID and state at Seq, so a later
	// fold can show it reproduces the version (or, after a forget, that it
	// no longer can).
	Fingerprint string `json:"fingerprint"`
}

// Validate reports every problem with t at once.
func (t Tag) Validate() error {
	var errs []error
	if !t.ID.Valid() {
		errs = append(errs, fmt.Errorf("id %q is not a valid ID", t.ID))
	}
	if t.At.IsZero() {
		errs = append(errs, errors.New("at is zero"))
	}
	if strings.TrimSpace(t.Name) == "" {
		errs = append(errs, errors.New("a tag needs a name"))
	}
	if t.Seq < 0 {
		errs = append(errs, errors.New("negative seq"))
	}
	return errors.Join(errs...)
}

// Forget is the audit record of a forget-with-proof (D-028): which records
// were deleted, when, why, and a SHA-256 of each one's stored bytes. The
// content is gone; the record proves what was removed without keeping it.
// It is append-only and can never itself be deleted.
type Forget struct {
	ID     ID        `json:"id"`
	At     time.Time `json:"at"`
	Reason string    `json:"reason"`
	// By says who asked: "user" from the CLI, "helix" from /lessons forget
	// --hard.
	By      string         `json:"by"`
	Targets []ForgetTarget `json:"targets"`
}

// ForgetTarget is one deleted record.
type ForgetTarget struct {
	Kind RecordKind `json:"kind"`
	ID   ID         `json:"id"`
	// At is the record's own time; SHA256 is of its stored bytes. Both are
	// filled by the journal as it deletes.
	At     time.Time `json:"at"`
	SHA256 string    `json:"sha256"`
	// TextSHA256, for a lesson, is TextDigest of its text, so the same
	// lesson proposed again can be refused without keeping its words.
	TextSHA256 string `json:"text_sha256,omitempty"`
}

// Validate reports every problem with f at once.
func (f Forget) Validate() error {
	var errs []error
	if !f.ID.Valid() {
		errs = append(errs, fmt.Errorf("id %q is not a valid ID", f.ID))
	}
	if f.At.IsZero() {
		errs = append(errs, errors.New("at is zero"))
	}
	if strings.TrimSpace(f.Reason) == "" {
		errs = append(errs, errors.New("a forget needs a reason"))
	}
	if strings.TrimSpace(f.By) == "" {
		errs = append(errs, errors.New("a forget names who asked"))
	}
	if len(f.Targets) == 0 {
		errs = append(errs, errors.New("a forget names the records it deletes"))
	}
	seen := map[ID]bool{}
	for _, t := range f.Targets {
		switch {
		case !t.ID.Valid():
			errs = append(errs, fmt.Errorf("target id %q is not a valid ID", t.ID))
		case t.Kind == RecordForget:
			errs = append(errs, fmt.Errorf("target %s: a forget record is never deleted", t.ID))
		case seen[t.ID]:
			errs = append(errs, fmt.Errorf("target %s is named twice", t.ID))
		}
		seen[t.ID] = true
	}
	return errors.Join(errs...)
}

// NormalizeText reduces a lesson's text to lower-case words, for comparing
// lessons that differ only in punctuation or case.
func NormalizeText(s string) string {
	return strings.Join(strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}), " ")
}

// TextDigest is the SHA-256 of NormalizeText(s), in hex.
func TextDigest(s string) string {
	sum := sha256.Sum256([]byte(NormalizeText(s)))
	return hex.EncodeToString(sum[:])
}
