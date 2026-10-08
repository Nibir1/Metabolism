package escape

import "testing"

// The sandbox must refuse both: a write outside the exercise copy, and any
// network connection.
func TestEscapeIsRefused(t *testing.T) {
	w, n := Escape()
	if w == nil {
		t.Errorf("wrote outside the copy")
	}
	if n == nil {
		t.Errorf("reached the network")
	}
}
