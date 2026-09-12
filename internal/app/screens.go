// The screens a person moves through, and what each one holds.
//
// Five of the six are lists: the modes, the routes of a mode, the directions of a
// route, the stops of a direction, and the stop search. Every letter typed on one
// of them goes to its filter, which is why a key with another meaning can only
// work on the sixth. That one is in board.go.
package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/render"
)

// screen is what the person is looking at.
//
// A screen owns its own data, and nothing else holds a field that is only
// meaningful while that screen is up. A new screen has to say what it is
// called, what it holds, how it draws and what it reports, and the compiler
// will not let it skip any of them.
//
// That last one is why this is an interface and not four switches. A screen
// missing from the switch in the semantic writer reports the wrong decision
// into the conformance artifact, where it reads as the program having decided
// something else rather than as a screen nobody finished.
type screen interface {
	// name is what the artifact calls this screen.
	name() string
	// refresh brings the screen's contents up to date against the cache.
	refresh(ctx context.Context, a *App) error
	// count is how many entries the screen holds, which bounds the selection.
	count() int
	// view describes the screen for the renderer.
	view(a *App, w, h int) render.View
	// report adds this screen's own keys to the semantic artifact. Every
	// screen writes its own, and none inherits one: a vocabulary is a contract
	// and a screen that inherited the wrong one would report a decision it
	// never made.
	report(out map[string]any)
	// open is what the row at i leads to. It returns nil to stay put.
	open(a *App, i int) screen
	// transient says the screen lasts only as long as what was typed into it.
	// The stop search is the one: an empty query is not a search, so the letter
	// that opened it takes it away again, and it is not a level esc comes back
	// to. Every screen answers for itself, because a screen that inherited the
	// wrong answer would swallow a key on a level it never belonged to.
	transient() bool
	// typed is what a letter does here.
	typed(a *App, r rune)
}

// list is what every screen that is a list of rows has in common.
//
// A filter narrows what the screen draws and not what it holds. The artifact
// reports everything, because the filter is reported beside it and a reader can
// then see both what was available and what was being looked for.
type list struct {
	// rows is everything the screen holds.
	rows []Row
	// shown indexes rows. It is what the screen draws and what the selection
	// moves through.
	shown []int
}

func (l *list) count() int { return len(l.shown) }

// typed sends a letter to the filter. A slash is not a letter: it goes home.
func (l *list) typed(a *App, r rune) {
	if r == home {
		a.home()
		return
	}
	a.filterBy(r)
}

// keep records what the screen holds and which of it the filter lets through.
// fields says what a row is matched on, and nil matches on both of its parts.
func (l *list) keep(rows []Row, filter string, fields func(Row) []string) {
	l.rows, l.shown = rows, nil
	for i, r := range rows {
		against := []string{r.Primary, r.Secondary}
		if fields != nil {
			against = fields(r)
		}
		if matches(filter, against...) {
			l.shown = append(l.shown, i)
		}
	}
}

// visible is the rows the screen draws.
func (l *list) visible() []Row {
	out := make([]Row, 0, len(l.shown))
	for _, i := range l.shown {
		out = append(out, l.rows[i])
	}
	return out
}

// index maps a selection onto what the screen holds.
func (l *list) index(i int) (int, bool) {
	if i < 0 || i >= len(l.shown) {
		return 0, false
	}
	return l.shown[i], true
}

// chosen is the route a person drilled into: which mode it belongs to, what it
// is called, and the colours it draws in.
//
// Every screen below the route list is about one route, so they hold this rather
// than four fields each, and the chain hands it down unchanged. A zero mode
// means none was recorded, which is the board a pin opens.
type chosen struct {
	mode   cache.Mode
	route  string
	colour string
	// detour is what the updates feed says about this route, if anything. It is
	// resolved at refresh rather than read at draw time, because it is a fact
	// about the world and a screen owns the world it draws.
	detour string
}

// modeName is what the first screen calls this route's mode, and nothing at all
// when no mode came down with the route.
func (c chosen) modeName() string {
	if c.mode == 0 {
		return ""
	}
	name, _ := describe(c.mode)
	return name
}

// badge is the route's crumb, in its own colours wherever it sits.
func (c chosen) badge() render.Crumb {
	return badgeCrumb(c.route, c.colour)
}

// subject is the crumb naming what a screen is about, which is the last one in
// its trail.
//
// A rail screen paints it in the line's own colour and a bus screen leaves it to
// take the app's red, whatever colour the route has: route 48 is grey in the feed
// and its screens still end in red. That is measured off the artifact and not
// derived, and it reads like two paths in the other implementation rather than
// one rule. The drilldown fixture holds it either way.
func (c chosen) subject(text string) render.Crumb {
	if c.mode != cache.Rail {
		return plainCrumb(text)
	}
	return render.Crumb{Text: text, Style: guideStyle(c.colour)}
}

// findDetour resolves what the updates feed says about this route.
func (c *chosen) findDetour(a *App) { c.detour = a.detourFor(c.route) }

// ---- the first screen ----------------------------------------------------

// choice is one row of the first screen: what it draws, what it opens, and the
// pin behind it when it is one.
//
// One structure, because these were five slices that had to stay in step and
// twice did not. A pin row indexed its departure out of a slice on the App, and
// the modes were sliced off the front of the rows by a count.
type choice struct {
	row   Row
	opens screen
	// pin is set when the row is a pin rather than a mode.
	pin *Pin
	// next is what a pin row draws. A pin with nothing left today draws a note
	// in its place and is not hidden.
	next *Departure
}

// modeScreen offers one row per mode, under any pins that resolve.
type modeScreen struct {
	choices []choice
}

func (*modeScreen) name() string    { return "mode" }
func (*modeScreen) transient() bool { return false }
func (m *modeScreen) count() int    { return len(m.choices) }

// typed opens the stop search. The first screen has no filter of its own, so a
// letter has somewhere else to go. A slash asks for the screen already up.
func (*modeScreen) typed(a *App, r rune) {
	if r == home {
		return
	}
	a.show(&searchScreen{})
	a.filterBy(r)
}

func (m *modeScreen) refresh(ctx context.Context, a *App) error {
	// Read once. The rows below count these, and every pin asks them which list
	// its route is on.
	today, err := a.routeLists(ctx)
	if err != nil {
		return err
	}

	var out []choice
	var live []Pin
	for _, p := range a.kept.all() {
		c, ok, err := a.pinChoice(ctx, p, today)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		out = append(out, c)
		live = append(live, p)
	}
	// This is the screen that finds out which pins the cache can still draw, and
	// the cap counts those.
	a.kept.resolved(live)

	for _, entry := range modes {
		routes := today[entry.mode]
		suffix := entry.many
		if len(routes) == 1 {
			suffix = entry.one
		}
		out = append(out, choice{
			row:   Row{Primary: entry.name, Secondary: strconv.Itoa(len(routes)) + " " + suffix},
			opens: &routeScreen{mode: entry.mode},
		})
	}

	m.choices = out
	return nil
}

// open follows the row. Reading the mode off the selection index instead breaks
// the day a pin takes row 0, which is what happens as soon as there is one.
func (m *modeScreen) open(_ *App, i int) screen {
	if i < 0 || i >= len(m.choices) {
		return nil
	}
	return m.choices[i].opens
}

// kept is how many rows are pins. They come first, so this is also the point
// the screen splits at.
func (m *modeScreen) kept() int {
	n := 0
	for _, c := range m.choices {
		if c.pin != nil {
			n++
		}
	}
	return n
}

func (m *modeScreen) report(out map[string]any) {
	rows, pins := []any{}, []any{}
	for _, c := range m.choices {
		rows = append(rows, rowJSON(c.row))
		if c.pin != nil {
			pins = append(pins, map[string]any{
				"stop":     c.pin.Stop,
				"route":    nullable(c.pin.Route),
				"headsign": nullable(c.pin.Headsign),
			})
		}
	}
	out["rows"] = rows
	out["pins"] = pins
}

func (m *modeScreen) view(a *App, w, h int) render.View {
	v := a.canvas(w, h)
	v.Split = m.kept()
	v.Status = render.Status{
		// The question goes in the trail and not in the label, because on
		// this screen it is the part next to the hints, and that is the
		// part a narrowing terminal takes away. It keeps the label's own
		// colour: where it sits is a fact about the bar, and what it is
		// called is a fact about the screen.
		Trail: []render.Crumb{{Text: "What are you taking?", Style: bold(plainText)}},
		// esc, not q. Every letter on this screen opens the search and goes
		// into the filter, and 58 stops begin with a Q.
		Hints: "type to find a stop · ↑↓ · ↵ · esc",
		Paint: statusPaint,
	}
	pins := pinLayout(w)
	for i, c := range m.choices {
		if c.pin != nil {
			v.Rows = append(v.Rows, pinRow(c, a.selected == i, pins))
			continue
		}
		row := render.Row{Cells: []render.Cell{
			fitted(modeLabel, c.row.Primary, plainText),
			fitted(modeCount, c.row.Secondary, muted),
		}}
		markAndSelect(&row, a.selected == i)
		v.Rows = append(v.Rows, row)
	}
	return v
}

// ---- the routes of one mode ----------------------------------------------

type routeScreen struct {
	list
	mode cache.Mode
	// routes is the route behind each row, for its badge.
	routes []cache.Route
}

func (*routeScreen) name() string    { return "routes" }
func (*routeScreen) transient() bool { return false }

func (r *routeScreen) refresh(ctx context.Context, a *App) error {
	routes, err := a.src.Routes(ctx, r.mode, a.clock.Date())
	if err != nil {
		return err
	}
	out := make([]Row, 0, len(routes))
	for _, route := range routes {
		// A route is found by its number or by a word from its long name.
		out = append(out, Row{Primary: route.ShortName, Secondary: route.LongName})
	}
	r.keep(out, a.filter, nil)

	r.routes = nil
	for _, i := range r.shown {
		r.routes = append(r.routes, routes[i])
	}
	return nil
}

func (r *routeScreen) open(_ *App, i int) screen {
	j, ok := r.index(i)
	if !ok {
		return nil
	}
	found := routeOf(r.routes, r.rows[j].Primary)
	return &directionScreen{chosen: chosen{
		mode: r.mode, route: found.ShortName,
		colour: found.Colour,
	}}
}

func (r *routeScreen) report(out map[string]any) { listVocabulary(out, r.rows) }

func (r *routeScreen) view(a *App, w, h int) render.View {
	name, label := describe(r.mode)
	v := a.canvas(w, h)
	v.Rows = routeRows(r.routes, a.selected)
	v.Empty = nothing(a.filter)
	v.Status = a.status(label, trailOf(plainCrumb(name)))
	return v
}

// ---- the directions of one route -----------------------------------------

type directionScreen struct {
	list
	chosen
	// dirs is the direction behind each row.
	dirs []cache.Direction
}

func (*directionScreen) name() string    { return "directions" }
func (*directionScreen) transient() bool { return false }

func (d *directionScreen) refresh(ctx context.Context, a *App) error {
	dirs, err := a.src.Directions(ctx, d.route, a.clock.Date())
	if err != nil {
		return err
	}

	rows := make([]Row, 0, len(dirs))
	for _, dir := range dirs {
		rows = append(rows, Row{Primary: "toward " + dir.Headsign, Secondary: trips(dir.Trips)})
	}
	// A direction is found by where it is going, and not by its trip count.
	d.keep(rows, a.filter, func(r Row) []string { return []string{r.Primary} })
	d.dirs = dirs
	d.findDetour(a)
	return nil
}

func (d *directionScreen) open(_ *App, i int) screen {
	j, ok := d.index(i)
	if !ok || j >= len(d.dirs) {
		return nil
	}
	return &stopScreen{chosen: d.chosen, headsign: d.dirs[j].Headsign}
}

func (d *directionScreen) report(out map[string]any) {
	out["detour"] = nullable(d.detour)
	listVocabulary(out, d.rows)
}

func (d *directionScreen) view(a *App, w, h int) render.View {
	v := a.canvas(w, h)
	v.Notice = warning(d.detour)
	v.Rows = treeRows(d.visible(), a.selected, d.colour)
	v.Empty = nothing(a.filter)
	v.Status = a.status("Which way?", trailOf(plainCrumb(d.modeName()), d.badge()))
	return v
}

// ---- the stops of one direction ------------------------------------------

type stopScreen struct {
	list
	chosen
	headsign string
	stops    []cache.Stop
}

func (*stopScreen) name() string    { return "stops" }
func (*stopScreen) transient() bool { return false }

func (s *stopScreen) refresh(ctx context.Context, a *App) error {
	stops, err := a.src.RouteStops(ctx, s.route, s.headsign, a.clock.Date())
	if err != nil {
		return err
	}

	rows := make([]Row, 0, len(stops))
	for _, stop := range stops {
		// The station, platform and all where the name carries one. A search
		// reports the platform separately, and the two are not one screen.
		rows = append(rows, Row{Primary: station(stop.Name), Secondary: "#" + stop.Code})
	}
	s.keep(rows, a.filter, nil)
	s.stops = stops
	s.findDetour(a)
	return nil
}

func (s *stopScreen) open(_ *App, i int) screen {
	j, ok := s.index(i)
	if !ok || j >= len(s.stops) {
		return nil
	}
	return &boardScreen{stop: s.stops[j], chosen: s.chosen, headsign: s.headsign}
}

func (s *stopScreen) report(out map[string]any) {
	out["detour"] = nullable(s.detour)
	listVocabulary(out, s.rows)
}

func (s *stopScreen) view(a *App, w, h int) render.View {
	v := a.canvas(w, h)
	v.Notice = warning(s.detour)
	v.Rows = treeRows(s.visible(), a.selected, s.colour)
	v.Empty = nothing(a.filter)
	v.Status = a.status(stopsLabel(s.mode), trailOf(
		plainCrumb(s.modeName()),
		s.badge(),
		s.subject("toward "+s.headsign),
	))
	return v
}

// ---- the stop search -----------------------------------------------------

type searchScreen struct {
	list
	// found is the match behind each row, kept beside the rows for the same
	// reason the first screen keeps what its rows open.
	found []cache.Match
	// more says the search stopped looking rather than found everything.
	more bool
}

func (*searchScreen) name() string    { return "search" }
func (*searchScreen) transient() bool { return true }

// typed sends every character to the filter, a slash included. A stop name can
// carry one: BILLINGS BRIDGE / BANK.
func (*searchScreen) typed(a *App, r rune) { a.filterBy(r) }

func (s *searchScreen) refresh(ctx context.Context, a *App) error {
	stops, more, err := a.src.SearchStops(ctx, a.filter, a.clock.Date())
	if err != nil {
		return err
	}
	s.found, s.more = stops, more

	out := make([]Row, 0, len(stops))
	for _, stop := range stops {
		// The artifact names the stop without its platform. The name in the
		// feed carries the platform already, so it comes back off. It is never
		// read out of the name in the other direction: CANTERBURY / AD. 860
		// holds a municipal address and no platform at all.
		out = append(out, Row{
			Primary:   withoutPlatform(stop.Name, stop.Platform),
			Secondary: strings.Join(stop.Routes, ", "),
		})
	}
	s.keep(out, "", nil)
	return nil
}

func (s *searchScreen) open(_ *App, i int) screen {
	if i < 0 || i >= len(s.found) {
		return nil
	}
	return &boardScreen{stop: s.found[i].Stop}
}

// foundText is how many stops the search offers. Past the limit it reads 25+,
// because the query stopped looking rather than found exactly that many.
func (s *searchScreen) foundText() string {
	if s.more {
		return strconv.Itoa(len(s.rows)) + "+"
	}
	return strconv.Itoa(len(s.rows))
}

func (s *searchScreen) report(out map[string]any) { listVocabulary(out, s.rows) }

func (s *searchScreen) view(a *App, w, h int) render.View {
	v := a.canvas(w, h)
	v.Empty = "no matches"
	v.Status = render.Status{
		Label: "Transit stops",
		Extra: "/" + a.filter,
		Hints: s.foundText() + " found · ↑↓ · ↵ · esc",
		Paint: statusPaint,
	}
	cols := searchLayout(w)
	for i, stop := range s.found {
		v.Rows = append(v.Rows, searchRow(stop, a.selected == i, cols))
	}
	return v
}
