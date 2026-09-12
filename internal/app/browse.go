// What the screens ask of the world.
//
// A screen owns what it holds and draws, and these are the questions several of
// them put to the cache and to the feed on the way: which routes run today, what
// a pin resolves to, and what is still to come at a stop.
//
// They live together because they are the only places a screen reaches past
// itself, and because each one has a rule that is easy to get wrong from inside a
// screen: a route list that is read once a turn rather than once a pin, a pin
// resolved through the current cache every time, and a board whose order is what
// the feed now expects rather than what the timetable said.
package app

import (
	"context"
	"sort"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
)

// lists is every mode's routes for today, read once.
//
// The first screen needs all of them to count its rows, and each pin needs them
// to say which list its route is on. Asking per pin meant two queries a pin on
// every turn of the loop, for an answer that cannot differ between them.
type lists map[cache.Mode][]cache.Route

func (a *App) routeLists(ctx context.Context) (lists, error) {
	out := make(lists, len(modes))
	for _, entry := range modes {
		routes, err := a.src.Routes(ctx, entry.mode, a.clock.Date())
		if err != nil {
			return nil, err
		}
		out[entry.mode] = routes
	}
	return out, nil
}

// modeOf is which list a route is on today, and a zero mode when no list holds
// it. A route that stopped running is on none of them, and so is the empty
// route a pin on a whole stop carries.
func (l lists) modeOf(route string) cache.Mode {
	if route == "" {
		return 0
	}
	for _, entry := range modes {
		for _, r := range l[entry.mode] {
			if r.ShortName == route {
				return entry.mode
			}
		}
	}
	return 0
}

// pinChoice resolves one pin against the current cache and builds its row.
//
// A pin holds a stop id and, when it was made by drilling, a route. An update
// replaces the whole database, so a stop can stop existing and a route can stop
// running. Either failing to resolve hides the row and keeps the pin, because
// both can come back. A stop that resolves but has nothing left today is a
// different thing, and it still draws.
func (a *App) pinChoice(ctx context.Context, p Pin, today lists) (choice, bool, error) {
	stop, ok, err := a.src.Stop(ctx, p.Stop)
	if err != nil || !ok {
		return choice{}, false, err
	}

	if p.Route != "" {
		runs, err := a.src.Directions(ctx, p.Route, a.clock.Date())
		if err != nil {
			return choice{}, false, err
		}
		if len(runs) == 0 {
			return choice{}, false, nil
		}
	}

	board, err := a.board(ctx, stop.ID, p.Route, p.Headsign, pinFetch)
	if err != nil {
		return choice{}, false, err
	}

	// Which list the route belongs to is looked up rather than stored. A pin
	// holds a stop and a route, and the board it opens still names the mode in
	// its trail, so the mode is resolved through the current cache like
	// everything else a pin carries.
	kept := p
	opens := &boardScreen{
		stop: stop, headsign: p.Headsign,
		chosen: chosen{mode: today.modeOf(p.Route), route: p.Route},
	}
	c := choice{
		row:   Row{Primary: station(stop.Name), Secondary: "#" + stop.Code},
		opens: opens,
		pin:   &kept,
	}
	if len(board) > 0 {
		c.next = &board[0]
		// A pin stores a stop and a route, and never a colour. The rows it is
		// about carry the route's own, so the badge in that board's trail is
		// painted from the departure the pin is already showing.
		opens.colour = board[0].Colour
	}
	return c, true, nil
}

// board is what has not gone yet at a stop, in order.
//
// A route and a headsign narrow it to one direction, which is what a board
// reached by drilling shows and what a pin carrying a route draws. Without
// them it is every route calling at the stop.
//
// fetch is how many rows to take, counted in schedule order and before any
// prediction moves one. A stop in the middle of the city has hundreds of
// departures left today and no screen has room for them.
func (a *App) board(ctx context.Context, stopID, route, headsign string, fetch int) ([]Departure, error) {
	calls, err := a.src.Departures(ctx, stopID, a.clock.Date(), a.clock.Yesterday())
	if err != nil {
		return nil, err
	}

	now := a.clock.Now()
	out := make([]Departure, 0, min(fetch, len(calls)))
	for _, c := range calls {
		if len(out) == fetch {
			break
		}
		// What has gone is decided by the schedule and not by the prediction. A
		// bus the feed has already put in the past is still on the board, dim
		// and struck through, because a person standing there wants to know it
		// went without them.
		if c.Scheduled < now {
			continue
		}
		if route != "" && (c.Route != route || c.Headsign != headsign) {
			continue
		}
		out = append(out, a.expect(c, stopID, now))
	}

	// A prediction moves a row along the axis, so the order is read off where
	// each row is now expected rather than off the table. Stable: two rows can
	// expect the same second, and the query behind calls ordered those totally.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Expected < out[j].Expected })
	return out, nil
}

// expect is one call with whatever the feed has to say about it.
//
// A trip is named by its id and a prediction is about one trip at one stop, so
// a prediction for the same trip somewhere else says nothing here.
func (a *App) expect(c cache.Departure, stopID string, now int) Departure {
	d := Departure{
		Route:         c.Route,
		Headsign:      c.Headsign,
		Scheduled:     c.Scheduled,
		Expected:      c.Scheduled,
		AfterMidnight: c.AfterMidnight,
		Colour:        c.Colour,
	}
	if live := a.feed.Live; live != nil {
		d.Cancelled = live.Cancelled(c.Trip)
		// A prediction is read even for a cancelled trip, which moves where the
		// row sits and which time it shows. No feed in the suite does both at
		// once, so the artifact cannot settle it: one rule for every row is the
		// choice here rather than something that fell out of the order.
		if at, ok := live.Arrival(c.Trip, stopID); ok {
			// The feed stamps absolute time and a schedule is written in
			// seconds on a service day. The clock is what turns one into the
			// other, and it is the only thing that knows where the day began.
			d.Expected, d.Predicted = a.clock.Second(at), true
		}
	}
	d.Wait = waitMinutes(d.Expected - now)
	return d
}
