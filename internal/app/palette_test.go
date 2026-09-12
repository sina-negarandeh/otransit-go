package app

import (
	"fmt"
	"testing"

	"github.com/sina-negarandeh/otransit-go/internal/render"
)

func TestABadgeTakesBlackOrWhiteWhicheverAReaderCanSee(t *testing.T) {
	t.Parallel()
	// The feed publishes a route_text_color for every route and this program does
	// not read it. It gives route 4 white on the teal 0980A5, which is the harder
	// of the two to read, and 103 of the 175 routes disagree with the rule below
	// in the same direction. So the colour behind decides the colour in front.
	for _, tc := range []struct {
		name, colour, want string
	}{
		{"the teal of the 4 takes black", "0980A5", "#0c0c0c"},
		{"a dark blue takes white", "0057B8", "#ffffff"},
		{"a mid green takes white", "508128", "#ffffff"},
		{"a light yellow takes black", "FFDD00", "#0c0c0c"},
		{"black takes white", "000000", "#ffffff"},
		{"white takes black", "FFFFFF", "#0c0c0c"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := badgeStyle(tc.colour)
			if got.Fg != tc.want {
				t.Errorf("badgeStyle(%q).Fg = %q, want %q", tc.colour, got.Fg, tc.want)
			}
			if want := hex(tc.colour); got.Bg != want {
				t.Errorf("badgeStyle(%q).Bg = %q, want %q", tc.colour, got.Bg, want)
			}
		})
	}
}

func TestARouteWithNoColourOfItsOwnStillDrawsABadge(t *testing.T) {
	t.Parallel()
	// A badge with no background is a route number with no badge around it, and
	// a route the feed forgot to colour is still a route. The program's own red
	// stands in, with black on it.
	for _, colour := range []string{"", "not a colour", "12345"} {
		got := badgeStyle(colour)
		if got.Bg != brand.Fg || got.Fg != "#0c0c0c" {
			t.Errorf("badgeStyle(%q) = %q on %q, want #0c0c0c on %q", colour, got.Fg, got.Bg, brand.Fg)
		}
	}
}

func TestABadgeIsBoldWhateverItsColours(t *testing.T) {
	t.Parallel()
	// The number is the thing a person scans a list for.
	for _, colour := range []string{"0057B8", ""} {
		if !badgeStyle(colour).Bold {
			t.Errorf("badgeStyle(%q) is not bold", colour)
		}
	}
}

// The contrast rule is WCAG's, so the threshold is where a reader's eye stops
// preferring one over the other. Pinned at the crossing rather than at two
// colours a long way from it, because the arithmetic is what can go wrong.
func TestTheContrastThresholdIsWhereWCAGPutsIt(t *testing.T) {
	t.Parallel()
	// The two ratios cross where the luminance behind reaches 0.179129, which is
	// between these two greys: one step of the middle channel apart, and on
	// opposite sides of the answer.
	for _, tc := range []struct{ colour, want string }{
		{"757575", "#ffffff"},
		{"767676", "#0c0c0c"},
	} {
		if got := badgeStyle(tc.colour).Fg; got != tc.want {
			t.Errorf("badgeStyle(%q).Fg = %q, want %q", tc.colour, got, tc.want)
		}
	}
}

func TestTheGuideTakesTheRoutesColourOnlyWhenItCanCarryALine(t *testing.T) {
	t.Parallel()
	// The line down the left of a drilled screen says which route the screen
	// belongs to. Two colours cannot carry it, and neither is rejected for being
	// dull: white outshines the stop names beside it, and #6d6e70 is exactly the
	// grey the pole numbers use, so a line in it reads as another column.
	//
	// Brightness alone is the wrong test. A rail line's red is darker than that
	// grey and is the colour most worth having.
	for _, tc := range []struct {
		name, colour, want string
	}{
		{"a bus blue carries it", "0057B8", "#0057b8"},
		{"a rail red carries it", "D30F1D", "#d30f1d"},
		{"the grey a pole number uses does not", "6D6E70", faint.Fg},
		{"white does not", "FFFFFF", faint.Fg},
		{"a colour lighter than the names does not", "E7E7E7", faint.Fg},
		{"no colour at all does not", "", faint.Fg},
		{"something that is not a colour does not", "nonsense", faint.Fg},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := guideStyle(tc.colour); got != (render.Style{Fg: tc.want}) {
				t.Errorf("guideStyle(%q) = %+v, want %q", tc.colour, got, tc.want)
			}
		})
	}
}

func TestTheTextALineSitsBesideIsTheColourTheRuleMeasuresAgainst(t *testing.T) {
	t.Parallel()
	// The rule compares a route colour against the text beside it, and a
	// luminance needs channels where the palette keeps a string. The two must not
	// drift apart.
	if got := hex(fmt.Sprintf("%02x%02x%02x", textR, textG, textB)); got != plainText.Fg {
		t.Errorf("the rule measures against %s, and the text is %s", got, plainText.Fg)
	}
}
