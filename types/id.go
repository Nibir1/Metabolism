package types

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// ID identifies any record: an episode, outcome, lesson, evidence or event.
//
// The first 12 hex digits are the creation time in Unix milliseconds, so IDs
// sort roughly by creation order. That keeps a raw journal dump readable and
// makes "which came first" answerable without a join. The remaining 16 hex
// digits are random, so two IDs minted in the same millisecond still differ.
type ID string

// NewID mints an ID for a record created at now.
func NewID(now time.Time) ID {
	var r [8]byte
	// crypto/rand.Read never returns an error on supported platforms (Go 1.24+).
	_, _ = rand.Read(r[:])
	return ID(fmt.Sprintf("%012x%s", now.UnixMilli()&0xffffffffffff, hex.EncodeToString(r[:])))
}

// Valid reports whether id has the shape NewID produces. It accepts any
// 28-digit lowercase hex string so IDs minted by a host (or a test) with its
// own clock are still accepted.
func (id ID) Valid() bool {
	if len(id) != 28 {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
