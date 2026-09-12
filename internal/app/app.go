// Package app holds the screens, the selection and the keys. It decides what
// a person sees, and it performs no I/O of its own.
//
// Every input arrives as an argument. The clock is held, the rows come from a
// Source, and nothing here reads the environment, a dotfile or the machine.
//
// One screen is up at a time and it owns its own data. What each screen is
// called, draws and reports lives with it, in screens.go.
package app

import (
	"context"
	"fmt"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/clock"
	"github.com/sina-negarandeh/otransit-go/internal/detour"
	"github.com/sina-negarandeh/otransit-go/internal/render"
)

// Source supplies the rows a screen shows. The cache satisfies it, and a test
// supplies its own.
type Source interface {
	Routes(ctx context.Context, m cache.Mode, date string) ([]cache.Route, error)
	Directions(ctx context.Context, route, date string) ([]cache.Direction, error)
	RouteStops(ctx context.Context, route, headsign, date string) ([]cache.Stop, error)
	SearchStops(ctx context.Context, query, date string) ([]cache.Match, bool, error)
	Stop(ctx context.Context, id string) (cache.Stop, bool, error)
	Departures(ctx context.Context, stopID, day, prev string) ([]cache.Departure, error)
}

// modes are the rows the first screen offers, in the order it draws them, with
// everything each one is called. One table, because the same two names were
// written into the rows, the status label and the status field separately.
var modes = []struct {
	mode cache.Mode
	// name is the row, and the field the list below it puts in its status bar.
	name string
	// one and many follow the count, and only a count of one is singular. The
	// rail row always ends in the note, because the O-Train carries no
	// realtime at all and every rail time is a scheduled one whatever the
	// poller is doing.
	one, many string
	// asking is the status label of the list this row opens, and stops is the
	// label of its list of stops. A rail line calls at stations.
	asking, stops string
}{
	{cache.Bus, "Bus", "route running today", "routes running today", "Which route?", "Which stop?"},
	{cache.Rail, "O-Train", "line · scheduled times only", "lines · scheduled times only", "Which line?", "Which station?"},
}

func describe(m cache.Mode) (name, asking string) {
	return entryFor(m).name, entryFor(m).asking
}

// stopsLabel is what a mode calls its list of stops.
func stopsLabel(m cache.Mode) string { return entryFor(m).stops }

func entryFor(m cache.Mode) struct {
	mode          cache.Mode
	name          string
	one, many     string
	asking, stops string
} {
	for _, entry := range modes {
		if entry.mode == m {
			return entry
		}
	}
	panic(fmt.Sprintf("mode %d is not on the first screen", int(m)))
}

// Row is one entry of a list, named as the artifact names it.
type Row struct {
	Primary   string
	Secondary string
}

// Departure is one row of a board, with every derivation the board makes.
type Departure struct {
	Route    string
	Headsign string
	// Scheduled is seconds on the service day being drawn.
	Scheduled int
	// Expected is when the row is now expected, on the same axis. It is the
	// prediction when the feed made one and the schedule when it did not, and
	// it is what the row shows, counts down to and sorts by.
	Expected int
	// Predicted reports whether the feed named this trip at this stop.
	Predicted bool
	// Wait is whole minutes until Expected, rounded.
	Wait          int
	AfterMidnight bool
	Cancelled     bool
	// Colour is the route's own, for its badge.
	Colour string
}

// A pinKey is what makes two pins the same pin: the stop, and the direction when
// the pin was made by drilling. Not the name, which the feed is free to rewrite,
// and not the code, which follows the name.
type pinKey struct{ stop, route, headsign string }

// Pin is a board somebody kept.
//
// It stores the stop, and the route and headsign as well when the pin was made
// by drilling, because two routes to one terminus are not two routes to one
// place. All three are resolved through the current cache every time: an
// update replaces the whole database, so a stored id can point at nothing and
// a route can stop running. A pin that fails to resolve is hidden and never
// deleted, because the stop and the route can come back.
type Pin struct {
	Stop     string
	Route    string
	Headsign string
	// Code and Name are the stop as it read when the pin was made. Nothing here
	// reads them back: they are written so the file can be opened and edited by
	// a person, and what a screen draws always comes from the current cache.
	Code string
	Name string
}

func (p Pin) key() pinKey { return pinKey{p.Stop, p.Route, p.Headsign} }

// Weather is the conditions the rule carries, and the zero value is a world
// with no weather feed.
type Weather struct {
	// Label is the line drawn at the right of the upper rule. An empty one
	// means the feed said nothing, and the artifact reports null.
	Label string
}

// Notices is what the updates feed said. A nil one is a program that never
// asked, which is what a replay with no updates file is.
type Notices struct {
	Live *detour.Feed
}

// KeyKind is a key the program understands.
type KeyKind int

// The keys. Char carries the rune that was typed.
const (
	Up KeyKind = iota
	Down
	Enter
	Esc
	Backspace
	Char
)

// Key is one keypress.
type Key struct {
	Kind KeyKind
	Rune rune
}

// App is the whole state of the program.
type App struct {
	src   Source
	clock clock.Clock
	feed  Feed

	weather  Weather
	screen   screen
	selected int
	filter   string
	// stack is what esc goes back to, innermost last. A search is never on it,
	// because a search is not a level: conformance/README.md puts the board's
	// parent at the stop.
	stack []screen
	// kept outlives every screen. In a replay the pins start from the fixture
	// and are never written back to it.
	kept kept
	// notices is the updates feed, which every screen about a route reads.
	notices Notices
	// leaving records that a key asked to end the program. The first screen is
	// the floor, so esc there has nowhere to go back to and goes out instead.
	leaving bool
}

// New returns the program on its first screen. Call Refresh before reading it.
func New(src Source, k clock.Clock, feed Feed) *App {
	return &App{src: src, clock: k, feed: feed, screen: &modeScreen{}}
}

// State is what the program holds whichever screen is up.
//
// It carries nothing a screen owns. The rows and the departures live on the
// screen that holds them and reach the outside through Semantic, so there is
// no second view of them to drift from the first.
type State struct {
	// Screen is the name the artifact uses.
	Screen   string
	Now      int
	Selected int
	Filter   string
	// Feed is what the poller says for itself, in the words the bar shows.
	Feed string
}

// State reports what the program holds.
func (a *App) State() State {
	return State{
		Screen:   a.screen.name(),
		Now:      a.clock.Now(),
		Selected: a.selected,
		Filter:   a.filter,
		Feed:     a.note(),
	}
}

// Semantic is the decisions the program reached, in the vocabulary the
// conformance artifact is written in. Every screen reports these keys, and
// each adds its own.
func (a *App) Semantic() map[string]any {
	out := map[string]any{
		"screen":   a.screen.name(),
		"now":      a.clock.Now(),
		"selected": a.selected,
		"filter":   a.filter,
		"feed":     a.feed.report(a.note(), a.clock.Now()),

		// The fixtures that carry a weather file and a detour feed fill these
		// in. They are in the vocabulary from the start because a missing key
		// and a null one are not the same artifact.
		"weather": nullable(a.weather.Label),
		"detour":  nil,
	}
	a.screen.report(out)
	return out
}

// View describes the screen for the renderer.
func (a *App) View(w, h int) render.View {
	return a.screen.view(a, w, h)
}

// canvas is the frame every screen draws in: the size it was handed, where the
// selection sits, and the parts that belong to the world rather than to the
// screen. A screen fills in what is its own and leaves the rest.
//
// One method and not a literal in each view. A screen that forgot the weather
// would draw a rule with nothing on it, which is exactly what a missing feed
// draws, and there is no diff that tells those apart.
func (a *App) canvas(w, h int) render.View {
	return render.View{
		Width: w, Height: h,
		Selected:    a.selected,
		Cursor:      true,
		Rule:        faint,
		Banner:      a.weather.Label,
		BannerStyle: muted,
		EmptyStyle:  emptyPaint,
		NoticeStyle: detourPaint,
	}
}

// Retime moves the clock to an instant somebody else read.
//
// It is the other half of what a turn of the event loop can do: a person presses
// a key, or time has passed and the program notices. A running program hands in
// the machine's clock and a replay hands in one it computed from a script.
func (a *App) Retime(k clock.Clock) {
	was := a.clock.Date()
	a.clock = k
	if k.Date() != was {
		// A new service day starts its clock at zero, so an attempt owed at half
		// past eight would never come due again. A day that has just started has
		// never been asked about.
		a.feed.due = k.Now()
	}
}

// Wait moves the clock on by secs, which is what a replay's wait step does. The
// service day does not move, because a replay never crosses one.
func (a *App) Wait(secs int) { a.Retime(a.clock.Advance(secs)) }

// Press applies one keypress. It changes what the program has decided and it
// reads nothing. Refresh brings the rows up to date afterwards.
func (a *App) Press(k Key) {
	switch k.Kind {
	case Up:
		a.move(-1)
	case Down:
		a.move(1)
	case Enter:
		a.enter()
	case Esc:
		a.escape()
	case Backspace:
		a.backspace()
	case Char:
		a.typed(k.Rune)
	}
}

func (a *App) move(by int) {
	next := a.selected + by
	if next < 0 || next >= a.screen.count() {
		return
	}
	a.selected = next
}

func (a *App) enter() {
	next := a.screen.open(a, a.selected)
	if next == nil {
		return
	}
	if !a.screen.transient() {
		a.stack = append(a.stack, a.screen)
	}
	a.show(next)
}

// escape goes back one level.
func (a *App) escape() {
	// A filter is undone before the screen is. Two escapes leave a filtered
	// list: one clears what was typed and the next goes back a level. Where the
	// filter is the screen there is nothing to undo, so the screen goes.
	if a.filter != "" && !a.screen.transient() {
		a.filter = ""
		a.selected = 0
		return
	}
	a.back()
}

// back leaves the screen for the one behind it. A board opened from a search
// has none, so it lands on the first screen: the board's parent is the stop,
// and a search is not a level.
func (a *App) back() {
	if n := len(a.stack); n > 0 {
		behind := a.stack[n-1]
		a.stack = a.stack[:n-1]
		a.show(behind)
		return
	}
	if _, first := a.screen.(*modeScreen); first {
		// Nowhere left to go back to. A person who presses esc on the first
		// screen has asked to be done, and the cursor stays where it was: the
		// screen they are leaving is the last thing they see.
		a.quit()
		return
	}
	a.show(&modeScreen{})
}

// status builds a status bar: the label, the trail of what the screen is
// about, and the filter when there is one.
func (a *App) status(label string, trail []render.Crumb) render.Status {
	s := render.Status{Label: label, Trail: trail, Hints: listHints, Paint: statusPaint}
	if a.filter != "" {
		s.Extra = "/" + a.filter
	}
	return s
}

func (a *App) backspace() {
	if a.filter == "" {
		// Nothing typed, so nothing to undo, and the key means the level. It is
		// the same journey esc makes: back to where you came from, and off the
		// first screen there is nowhere back to.
		a.back()
		return
	}
	runes := []rune(a.filter)
	a.filter = string(runes[:len(runes)-1])
	a.selected = 0
	// The last letter of a query takes the screen with it.
	if a.filter == "" && a.screen.transient() {
		a.back()
	}
}

// home goes back to the first screen and forgets the way back, because the way
// back is where you came from and you are no longer there.
func (a *App) home() {
	a.stack = nil
	a.show(&modeScreen{})
}

// typed hands a letter to the screen. On a list every letter goes to the
// filter, so a key with another meaning can only work where typing does
// nothing, which is the departures board.
func (a *App) typed(r rune) {
	a.screen.typed(a, r)
}

// filterBy adds a letter to the filter. Nothing here asks whether the filter
// is empty: that is true at the start of every search, so a key guarded on it
// would swallow the first letter a person types.
func (a *App) filterBy(r rune) {
	a.filter += string(r)
	a.selected = 0
}

// quit ends the program, and leaves the screen as it is: whatever was drawn last
// is the last thing a person sees.
//
// Two keys reach it. Esc on the first screen, which is the floor everywhere, and
// q on a departures board, which is the only screen where a letter is not part
// of a filter.
func (a *App) quit() { a.leaving = true }

// show moves to a screen. A screen is entered at its top with no filter, in
// whichever direction it was entered from.
func (a *App) show(s screen) {
	a.screen = s
	a.selected = 0
	a.filter = ""
}

// Leaving reports that a key asked to end the program.
//
// The decision is here and not in the loop, because which screen is the floor is
// something only this package knows: a loop that checked for esc on a screen
// called mode would be reading the vocabulary back out of the artifact.
func (a *App) Leaving() bool { return a.leaving }

// Refresh brings the screen up to date against the cache. It is the query half
// of one turn of the event loop, and Press is the decision half.
func (a *App) Refresh(ctx context.Context) error {
	// The poller first: a screen built after it sees what this turn's attempts
	// brought back, and a refusal leaves the last answer where it was.
	a.feed.poll(a.clock.Now())
	if err := a.screen.refresh(ctx, a); err != nil {
		return err
	}
	if a.selected >= a.screen.count() {
		a.selected = 0
	}
	return nil
}
