// The departures board: the one screen that is not a list.
//
// It draws no cursor, because there is nothing to move through, and it takes no
// filter. That is what makes it the only screen where a letter can mean something
// else, and both letters that do live here: p keeps the board, and q leaves.
package app

import (
	"context"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/render"
)

type boardScreen struct {
	stop cache.Stop
	// chosen and headsign are set when the board was reached through a
	// direction, or through a pin that carries one. Such a board shows only
	// that route and leaves the headsign column out, because the direction
	// already chose it.
	//
	// They are fields and not something read back out of the status bar. The
	// trail below is built from them, and a display string is never parsed to
	// recover a value the screen already had.
	chosen
	headsign   string
	departures []Departure
	// state is what the artifact calls this board's pin, recorded at refresh.
	state string
}

// drilled reports whether the board is about one route rather than a stop.
func (b *boardScreen) drilled() bool { return b.route != "" }

// pin is what keeping this board would store. A board about one route carries
// it, because two routes to one terminus are not two routes to one place: 44
// and 48 both end at Billings Bridge by roads that do not meet.
func (b *boardScreen) pin() Pin {
	return Pin{
		Stop: b.stop.ID, Route: b.route, Headsign: b.headsign,
		Code: b.stop.Code, Name: b.stop.Name,
	}
}

// trail is what the status bar says before the stop.
//
// A board about one route names the mode, the route and the direction, and the
// renderer drops the leading crumbs when a long stop name leaves no room. A
// board opened from a pin names no mode, because a pin does not record one. A
// board about a stop names its pole.
func (b *boardScreen) trail() []render.Crumb {
	if !b.drilled() {
		return trailOf(plainCrumb("#"+b.stop.Code), plainCrumb(station(b.stop.Name)))
	}
	out := []render.Crumb{}
	if name := b.modeName(); name != "" {
		out = append(out, plainCrumb(name))
	}
	out = append(out,
		b.badge(),
		plainCrumb("toward "+b.headsign),
		b.subject(station(b.stop.Name)))
	return trailOf(out...)
}

// typed gives p and q another meaning. They can only have one here, because on
// a list every letter goes to the filter, and a letter this screen does not know
// is not typed anywhere.
func (b *boardScreen) typed(a *App, r rune) {
	switch r {
	case 'p':
		a.kept.toggle(b.pin())
	case 'q':
		a.quit()
	case home:
		a.home()
	}
}

func (*boardScreen) name() string            { return "departures" }
func (*boardScreen) transient() bool         { return false }
func (b *boardScreen) count() int            { return len(b.departures) }
func (b *boardScreen) open(*App, int) screen { return nil }

func (b *boardScreen) refresh(ctx context.Context, a *App) error {
	out, err := a.board(ctx, b.stop.ID, b.route, b.headsign, boardFetch)
	if err != nil {
		return err
	}
	b.departures = out

	b.state = "unpinned"
	if a.kept.has(b.pin()) {
		b.state = "pinned"
	}
	return nil
}

func (b *boardScreen) view(a *App, w, h int) render.View {
	v := a.canvas(w, h)
	// A board is not a list anyone moves through, so it draws no cursor.
	v.Cursor = false
	v.Empty = "no more departures today"
	v.Status = render.Status{
		Label: "Departures",
		Trail: b.trail(),
		Hints: a.note() + " · " + b.keepHint(a) + "esc · q",
		Paint: statusPaint,
	}
	cols := boardLayout(w, b.drilled())
	for _, d := range b.departures {
		v.Rows = append(v.Rows, boardRow(d, cols))
	}
	return v
}

// report carries every derivation a board makes from a prediction, because
// those derivations are what is under test. The prediction itself is not here.
// keepHint is the part of a board's hints that the pin key earns.
//
// It names the gesture and what it would do, so the key is discoverable, and a
// refusal at the cap reads as a state rather than as a key that does nothing.
func (b *boardScreen) keepHint(a *App) string {
	switch a.kept.state(b.pin()) {
	case pinned:
		return "p unpin · "
	case full:
		return "pins full · "
	}
	return "p pin · "
}

func (b *boardScreen) report(out map[string]any) {
	rows := []any{}
	for _, d := range b.departures {
		live, late := said(d)
		rows = append(rows, map[string]any{
			"route":          d.Route,
			"headsign":       d.Headsign,
			"scheduled":      d.Scheduled,
			"wait":           d.Wait,
			"after_midnight": d.AfterMidnight,
			"cancelled":      d.Cancelled,

			// What the feed said, and how far off the table it put this trip.
			// Both are null on a row the feed never mentioned, which is not the
			// same thing as a row it said was on time.
			"live": live,
			"late": late,
		})
	}
	out["departures"] = rows
	out["pinned"] = b.state
}
