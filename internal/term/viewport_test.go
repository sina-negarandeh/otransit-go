package term

import (
	"fmt"
	"strings"
	"testing"
)

// The band is taken by moving down through it and drawn by going back up the
// same distance. Those two numbers are written separately and have to agree: one
// newline too many and every frame lands a row below its own band, over the line
// the program was started from.
//
// This is the bug that was here. Nothing in the conformance suite could see it,
// because a fixture is the inside of the viewport and this is the edge of it.
func TestTakingTheBandAndGoingBackUpAgree(t *testing.T) {
	t.Parallel()
	down := strings.Count(claim, "\n")
	if down != Rows-1 {
		t.Errorf("the band is taken with %d newlines, want %d: one for every row but the first", down, Rows-1)
	}
	if want := fmt.Sprintf(up, down); !strings.Contains(toFirstRow, want) {
		t.Errorf("a draw begins with %q, want it to go up %d rows, which is how far down the band was taken",
			toFirstRow, down)
	}
	if !strings.HasPrefix(toFirstRow, "\r") {
		t.Errorf("a draw begins with %q, want it to return to the first column too", toFirstRow)
	}
}

// The viewport is the fixtures' own eleven rows: eight of content, two rules and
// the status bar. A screen a person sees and a screen a fixture pins are the same
// screen, which is the whole reason the port was worth doing.
func TestTheViewportIsTheHeightTheFixturesAreDrawnIn(t *testing.T) {
	t.Parallel()
	const content, rules, status = 8, 2, 1
	if Rows != content+rules+status {
		t.Errorf("the viewport is %d rows, want %d", Rows, content+rules+status)
	}
}

// Not one escape this package writes takes the screen away from what was already
// on it.
func TestNoEscapeHereTakesTheScreen(t *testing.T) {
	t.Parallel()
	for _, escape := range []string{hide, show, up, clearRow, claim, toFirstRow} {
		for _, banned := range []string{"?1049", "?47", "[2J", "smcup"} {
			if strings.Contains(escape, banned) {
				t.Errorf("%q holds %q", escape, banned)
			}
		}
	}
}
