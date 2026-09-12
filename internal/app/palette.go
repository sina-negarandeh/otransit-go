// The colours this program draws with, and which part of a screen takes each.
//
// Every value here is pinned by the styles artifact the conformance suite
// compares. The names say what the colour is for rather than what it looks like,
// so a change of palette is a change in one place.
package app

import (
	"math"
	"strconv"
	"strings"

	"github.com/sina-negarandeh/otransit-go/internal/render"
)

var (
	// faint is the rules, the hints, and what separates one crumb from the next.
	faint = render.Style{Fg: "#3a3b3d"}
	// muted is a secondary column: a count, a headsign, a time.
	muted = render.Style{Fg: "#6d6e70"}
	// plainText is a primary column, the thing the row is about.
	plainText = render.Style{Fg: "#e6e6e6"}
	// brand is the marker beside the selected row, what was typed, and the stop
	// a board is for.
	brand = render.Style{Fg: "#da3839"}
	// dueNow, dueSoon and comingUp say how soon a bus is, by how many whole
	// minutes are left. Past comingUp a wait is just a number.
	dueNow   = render.Style{Fg: "#ff6b6b", Bold: true}
	dueSoon  = render.Style{Fg: "#ffc107", Bold: true}
	comingUp = render.Style{Fg: "#5cd68a"}
)

// detourPaint is the warning line above a list of a route's directions or
// stops. It is the amber of a bus a few minutes away, because both mean the
// same thing to a person reading the screen: look at this before you go.
var detourPaint = unbold(dueSoon)

// badgeStyle is a route badge: the route's colour behind its number, and black
// or white in front of it.
//
// The feed carries a route_text_color for every route and this does not read it.
// It says white on the mid green of the 2, where white is the harder of the two
// to read, so the colour in front is worked out from the colour behind instead
// of taken on trust.
//
// A route the feed left uncoloured still gets a badge, in the program's own red.
// The alternative is a number with no badge around it, which reads as a
// different kind of row rather than as a missing colour.
func badgeStyle(colour string) render.Style {
	r, g, b, ok := rgb(colour)
	if !ok {
		return render.Style{Fg: ink, Bg: brand.Fg, Bold: true}
	}
	return render.Style{Fg: readableOn(r, g, b), Bg: hex(colour), Bold: true}
}

// ink is the darkest the program draws, and what a badge uses when its colour is
// light. It is not pure black, which reads as a hole on a dark terminal.
const ink = "#0c0c0c"

// readableOn is ink or white, whichever a reader can see against this colour.
//
// The rule is WCAG's contrast ratio, (lighter + 0.05) / (darker + 0.05), worked
// out for both and compared. White is 1.0, so its side is 1.05 / (l + 0.05) and
// ink is near enough 0 to treat as black.
func readableOn(r, g, b uint8) string {
	l := luminance(r, g, b)
	if (l+0.05)/0.05 >= 1.05/(l+0.05) {
		return ink
	}
	return "#ffffff"
}

// luminance is WCAG's relative luminance of an sRGB colour: how bright a person
// sees it, which is not the average of its three channels. Green carries most of
// the weight and blue almost none.
func luminance(r, g, b uint8) float64 {
	linear := func(c uint8) float64 {
		v := float64(c) / 255
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
}

// rgb reads a feed colour, which is six hex digits and no leading hash. A route
// with no colour, or something that is not one, reports false.
func rgb(colour string) (r, g, b uint8, ok bool) {
	if len(colour) != 6 {
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(colour, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return uint8(n >> 16), uint8(n >> 8), uint8(n), true
}

// guideStyle is the line down the left of a screen reached by drilling. It says
// which route the screen belongs to, in that route's colour when the colour can
// carry a line beside this palette's text.
//
// Two colours cannot, and neither is rejected for being dull. White outshines the
// stop names next to it, and the grey a pole number uses would read as another
// column. Everything else keeps its own colour: a rail line's red is darker than
// that grey and is the colour most worth having, so brightness alone is the wrong
// test.
func guideStyle(colour string) render.Style {
	if readsAsARule(colour) {
		return render.Style{Fg: hex(colour)}
	}
	return faint
}

// The text a rule sits beside, as channels: a luminance needs numbers and
// plainText carries the same colour as a string. A test holds the two together.
const textR, textG, textB = 0xe6, 0xe6, 0xe6

// readsAsARule reports whether a line in this colour can sit beside the text.
func readsAsARule(colour string) bool {
	r, g, b, ok := rgb(colour)
	if !ok || hex(colour) == muted.Fg {
		return false
	}
	return luminance(r, g, b) < luminance(textR, textG, textB)
}

// hex writes a feed colour the way the artifact does.
func hex(c string) string {
	if c == "" {
		return ""
	}
	return "#" + strings.ToLower(c)
}

// unbold is the same style with bold turned off.
//
// The note beside a wait shares the wait's colours and not its weight: a late
// note is the amber of a bus a few minutes away, drawn lighter, because the
// note says where the time came from and the wait says how long you have.
func unbold(s render.Style) render.Style {
	s.Bold = false
	return s
}

// bold is the same style with bold turned on, which is how a selected row is
// drawn from end to end.
func bold(s render.Style) render.Style {
	s.Bold = true
	return s
}

// statusPaint is how every status bar is drawn.
var statusPaint = render.Paint{
	Label:     bold(plainText),
	Separator: faint,
	Extra:     bold(brand),
	Hints:     faint,
}

// trailOf styles a trail. The last crumb is what the screen is about and takes
// the brand colour, and the rest are muted.
func trailOf(parts ...render.Crumb) []render.Crumb {
	for i := range parts {
		if parts[i].Style != (render.Style{}) {
			continue
		}
		parts[i].Style = muted
		if i == len(parts)-1 {
			parts[i].Style = brand
		}
	}
	return parts
}

// plain is a crumb that takes whatever its position gives it.
func plainCrumb(text string) render.Crumb { return render.Crumb{Text: text} }

// badgeCrumb is a route in the trail, which keeps its own colours wherever it
// sits.
func badgeCrumb(route, colour string) render.Crumb {
	return render.Crumb{Text: badge(route), Style: badgeStyle(colour)}
}

// waitPaint is how soon a bus is, as a colour.
//
// The thresholds read the rounded minute and not the seconds behind it, so a
// bus 121 seconds away is drawn the same as one 120 seconds away. Both say two
// minutes, and a column that disagreed with its own number would be worse than
// one that rounds.
func waitPaint(mins int) render.Style {
	switch {
	case mins < 0:
		// A bus that has gone. Only a realtime prediction can put one here, so
		// this comes from conformance/README.md and not from a diff.
		return strikeOut(muted)
	case mins <= 2:
		return dueNow
	case mins <= 6:
		return dueSoon
	case mins <= 15:
		return comingUp
	default:
		return muted
	}
}

// strikeOut is the same style struck through.
func strikeOut(s render.Style) render.Style {
	s.Strike = true
	return s
}
