package app

import (
	"context"
	"testing"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/render"
)

// stub is a source the test controls. The queries themselves are tested in
// package cache. What is under test here is the screens and the keys.
type stub struct {
	bus, rail  []cache.Route
	stops      []cache.Match
	more       bool
	dirs       []cache.Direction
	routeStops []cache.Stop
	board      map[string][]cache.Departure
}

func (s stub) Routes(_ context.Context, m cache.Mode, _ string) ([]cache.Route, error) {
	if m == cache.Bus {
		return s.bus, nil
	}
	return s.rail, nil
}

func (s stub) SearchStops(_ context.Context, query, _ string) ([]cache.Match, bool, error) {
	if query == "" {
		return nil, false, nil
	}
	return s.stops, s.more, nil
}

func (s stub) Directions(_ context.Context, _, _ string) ([]cache.Direction, error) {
	return s.dirs, nil
}

func (s stub) RouteStops(_ context.Context, _, _, _ string) ([]cache.Stop, error) {
	return s.routeStops, nil
}

func (s stub) Departures(_ context.Context, stopID, _, _ string) ([]cache.Departure, error) {
	return s.board[stopID], nil
}

// boardStub holds two stops, one of which has a departure that has already
// gone and one that has not.
func boardStub() stub {
	return stub{
		stops: []cache.Match{
			{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "FIRST STOP"}, Routes: []string{"44"}, Toward: "Hurdman"},
			{Stop: cache.Stop{ID: "s2", Code: "3035", Name: "SECOND STOP"}, Routes: []string{"48"}, Toward: "Carleton"},
		},
		board: map[string][]cache.Departure{
			"s1": {
				{Route: "44", Headsign: "Hurdman", Scheduled: 22500},
				{Route: "44", Headsign: "Hurdman", Scheduled: 32400},
			},
			"s2": {{Route: "48", Headsign: "Carleton", Scheduled: 31321}},
		},
	}
}

// press applies keys and refreshes after each, which is what one turn of the
// event loop does.
func press(t *testing.T, a *App, keys ...Key) {
	t.Helper()
	for _, k := range keys {
		a.Press(k)
		if err := a.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
	}
}

func busy() stub {
	return stub{
		bus:   []cache.Route{{ShortName: "44"}, {ShortName: "48"}},
		rail:  []cache.Route{{ShortName: "1"}},
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Name: "BILLINGS BRIDGE"}}},
	}
}

func TestTheModeScreenCountsWhatRunsToday(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name           string
		src            stub
		wantBus, wantR string
	}{
		{"a day with service", busy(), "2 routes running today", "1 line · scheduled times only"},
		{"a day with none", stub{}, "0 routes running today", "0 lines · scheduled times only"},
		{
			"one of each",
			stub{bus: []cache.Route{{ShortName: "44"}}, rail: []cache.Route{{ShortName: "1"}}},
			"1 route running today", "1 line · scheduled times only",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := firstScreen(t, newApp(t, tc.src)).choices
			if len(got) != 2 {
				t.Fatalf("the first screen drew %d rows, want two", len(got))
			}
			if got[0].row != (Row{Primary: "Bus", Secondary: tc.wantBus}) {
				t.Errorf("rows[0] = %+v", got[0].row)
			}
			if got[1].row != (Row{Primary: "O-Train", Secondary: tc.wantR}) {
				t.Errorf("rows[1] = %+v", got[1].row)
			}
		})
	}
}

func TestEnterOpensTheListForTheSelectedMode(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		down  int
		label string
		field string
		rows  int
	}{
		{"the bus row", 0, "Which route?", "Bus", 2},
		{"the rail row", 1, "Which line?", "O-Train", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, busy())
			for i := 0; i < tc.down; i++ {
				press(t, a, Key{Kind: Down})
			}
			press(t, a, Key{Kind: Enter})

			got := a.State()
			if got.Screen != "routes" {
				t.Fatalf("Screen = %v, want the route list", got.Screen)
			}
			if n := a.screen.count(); n != tc.rows {
				t.Errorf("the route list holds %d rows, want %d", n, tc.rows)
			}
			bar := a.View(100, 14).Status
			if bar.Label != tc.label || len(bar.Trail) == 0 || bar.Trail[0].Text != tc.field {
				t.Errorf("status = %q %q, want %q then %q", bar.Label, bar.Trail, tc.label, tc.field)
			}
		})
	}
}

func TestEscapeReturnsToTheModeScreen(t *testing.T) {
	t.Parallel()
	a := newApp(t, busy())
	press(t, a, Key{Kind: Enter}, Key{Kind: Esc})
	if got := a.State().Screen; got != "mode" {
		t.Errorf("Screen = %v, want the mode screen", got)
	}
}

// A screen is entered at its top, whichever direction you came from. Pressing
// down and then entering and leaving must not bring the old row back.
//
// Every list here needs at least two rows. Refresh clamps a selection that is
// past the end of its list, so a one-row list would land on row 0 whether the
// rule holds or not. The first version of this test did that and passed with
// the rule taken out.
func TestEveryScreenIsEnteredAtItsTopRow(t *testing.T) {
	t.Parallel()
	a := newApp(t, stub{
		bus:  []cache.Route{{ShortName: "44"}, {ShortName: "48"}},
		rail: []cache.Route{{ShortName: "1"}, {ShortName: "2"}},
	})

	press(t, a, Key{Kind: Down})
	if got := a.State().Selected; got != 1 {
		t.Fatalf("after down Selected = %d, want 1", got)
	}
	press(t, a, Key{Kind: Enter})
	if got := a.State().Selected; got != 0 {
		t.Errorf("entering the route list Selected = %d, want 0", got)
	}

	press(t, a, Key{Kind: Down})
	if got := a.State().Selected; got != 1 {
		t.Fatalf("after down Selected = %d, want 1", got)
	}
	press(t, a, Key{Kind: Esc})
	if got := a.State().Selected; got != 0 {
		t.Errorf("back on the mode screen Selected = %d, want 0", got)
	}
}

func TestTheSelectionStopsAtEachEndOfTheList(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		keys []Key
		want int
	}{
		{"up at the top stays", []Key{{Kind: Up}}, 0},
		{"down moves", []Key{{Kind: Down}}, 1},
		{"down at the bottom stays", []Key{{Kind: Down}, {Kind: Down}, {Kind: Down}}, 1},
		{"down then up returns", []Key{{Kind: Down}, {Kind: Up}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, busy())
			press(t, a, tc.keys...)
			if got := a.State().Selected; got != tc.want {
				t.Errorf("Selected = %d, want %d", got, tc.want)
			}
		})
	}
}

// On the mode screen every letter goes to the stop search. The status bar
// there says so, and the key must not be guarded on the filter being empty.
func TestALetterOnTheModeScreenOpensTheStopSearch(t *testing.T) {
	t.Parallel()
	a := newApp(t, busy())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Char, Rune: 'i'})

	got := a.State()
	if got.Screen != "search" {
		t.Fatalf("Screen = %v, want the search screen", got.Screen)
	}
	if got.Filter != "bi" {
		t.Errorf("Filter = %q, want %q", got.Filter, "bi")
	}
}

func TestBackspaceRemovesTheLastLetterOfTheFilter(t *testing.T) {
	t.Parallel()
	a := newApp(t, busy())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Char, Rune: 'i'}, Key{Kind: Backspace})
	if got := a.State().Filter; got != "b" {
		t.Errorf("Filter = %q, want %q", got, "b")
	}
}

// Esc on the first screen has nowhere to go, so it stays where it is and keeps
// the cursor where it was. Going back to a screen puts the cursor at the top,
// and the first screen is not somewhere you can go back to.
//
// Measured against the reference, which no fixture reaches: down then esc there
// reports the second row still chosen.
func TestEscOnTheFirstScreenKeepsTheCursorWhereItIs(t *testing.T) {
	t.Parallel()
	a := newApp(t, busy())
	press(t, a, Key{Kind: Down}, Key{Kind: Esc})

	got := a.State()
	if got.Screen != "mode" {
		t.Fatalf("Screen = %v, want mode", got.Screen)
	}
	if got.Selected != 1 {
		t.Errorf("Selected = %d, want 1", got.Selected)
	}
}

// The query is the search. There is no such screen as a search for nothing, so
// the letter that opened it takes it away again, and what is behind it is the
// screen the letter was typed on.
//
// Measured against the reference, which no fixture reaches: on a list the same
// two keys leave the screen alone and only undo the filter.
func TestAnEmptyQueryIsNotASearch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		keys   []Key
		screen string
		filter string
	}{
		{"the last letter goes", []Key{{Kind: Char, Rune: 'b'}, {Kind: Backspace}}, "mode", ""},
		{"a letter is left", []Key{{Kind: Char, Rune: 'b'}, {Kind: Char, Rune: 'i'}, {Kind: Backspace}}, "search", "b"},
		{"esc leaves the search", []Key{{Kind: Char, Rune: 'b'}, {Kind: Esc}}, "mode", ""},
		{"esc undoes a list filter", []Key{{Kind: Enter}, {Kind: Char, Rune: 'b'}, {Kind: Esc}}, "routes", ""},
		{"backspace keeps a list", []Key{{Kind: Enter}, {Kind: Char, Rune: 'b'}, {Kind: Backspace}}, "routes", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, busy())
			press(t, a, tc.keys...)

			got := a.State()
			if got.Screen != tc.screen {
				t.Errorf("Screen = %v, want %v", got.Screen, tc.screen)
			}
			if got.Filter != tc.filter {
				t.Errorf("Filter = %q, want %q", got.Filter, tc.filter)
			}
		})
	}
}

func TestASearchWithNoMatchesSaysSoAndCountsNone(t *testing.T) {
	t.Parallel()
	a := newApp(t, stub{})
	press(t, a, Key{Kind: Char, Rune: 'b'})

	v := a.View(100, 14)
	if v.Empty != "no matches" {
		t.Errorf("Empty = %q, want %q", v.Empty, "no matches")
	}
	if v.Status.Hints != "0 found · ↑↓ · ↵ · esc" {
		t.Errorf("Hints = %q", v.Status.Hints)
	}
	if got := v.Status.Extra; got != "/b" {
		t.Errorf("Extra = %q, want the filter", got)
	}
}

func TestARouteListWithNothingOnItSaysNothingHere(t *testing.T) {
	t.Parallel()
	a := newApp(t, stub{})
	press(t, a, Key{Kind: Enter})
	if got := a.View(100, 14).Empty; got != "nothing here" {
		t.Errorf("Empty = %q, want %q", got, "nothing here")
	}
}

func TestWithNoKeyTheFeedSaysScheduledOnly(t *testing.T) {
	t.Parallel()
	if got := newApp(t, busy()).State().Feed; got != "no key · scheduled only" {
		t.Errorf("Feed.Note = %q", got)
	}
}

func TestNowComesFromTheClockAndNotTheMachine(t *testing.T) {
	t.Parallel()
	if got := newApp(t, busy()).State().Now; got != 8*3600 {
		t.Errorf("Now = %d, want %d", got, 8*3600)
	}
}

// What a row opens must follow the row and not its index. conformance/README.md
// says row 0 of the first screen is a pin whenever a fixture has one, and an
// index-based mapping would then open the rail list from the Bus row while
// still drawing a frame that looks entirely reasonable.
func TestEnterOpensTheListTheSelectedRowNames(t *testing.T) {
	t.Parallel()
	for i, want := range []string{"Bus", "O-Train"} {
		t.Run(want, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, busy())
			for j := 0; j < i; j++ {
				press(t, a, Key{Kind: Down})
			}
			if got := firstScreen(t, a).choices[i].row.Primary; got != want {
				t.Fatalf("row %d reads %q, want %q", i, got, want)
			}
			press(t, a, Key{Kind: Enter})
			trail := a.View(100, 14).Status.Trail
			if len(trail) == 0 || trail[0].Text != want {
				t.Errorf("entering the %q row opened %q", want, trail)
			}
		})
	}
}

func TestEveryScreenHasAName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		screen screen
		want   string
	}{
		{&modeScreen{}, "mode"},
		{&routeScreen{}, "routes"},
		{&searchScreen{}, "search"},
		{&boardScreen{}, "departures"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := tc.screen.name(); got != tc.want {
				t.Errorf("name() = %q, want %q", got, tc.want)
			}
		})
	}
}

// opens and the rows of the first screen must stay parallel. The pin case
// itself cannot be exercised until pins exist, so this holds the invariant
// that would break first: a row added to the first screen with no matching
// entry saying where it leads.
func TestEveryRowOfTheFirstScreenKnowsWhatItOpens(t *testing.T) {
	t.Parallel()
	a := newApp(t, busy())
	for i, c := range firstScreen(t, a).choices {
		if c.opens == nil {
			t.Fatalf("row %d (%q) leads nowhere", i, c.row.Primary)
		}
		opens, ok := c.opens.(*routeScreen)
		if !ok {
			// A pin row opens a board rather than a list of routes.
			continue
		}
		name, _ := describe(opens.mode)
		if name != c.row.Primary {
			t.Errorf("row %d reads %q and opens the %q list", i, c.row.Primary, name)
		}
	}
}

func TestEnterOnASearchResultOpensThatStopsBoard(t *testing.T) {
	t.Parallel()
	a := newApp(t, boardStub())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Down}, Key{Kind: Enter})

	got := a.State()
	if got.Screen != "departures" {
		t.Fatalf("Screen = %v, want the board", got.Screen)
	}
	if n := a.screen.count(); n != 1 {
		t.Fatalf("the board holds %d departures, want 1", n)
	}
	if got := a.View(100, 14).Status.Trail; len(got) != 2 || got[0].Text != "#3035" || got[1].Text != "SECOND STOP" {
		t.Errorf("Trail = %q, want the pole number then the stop", got)
	}
}

// A board reached from a search goes back to the first screen, not into the
// results. The board's parent is the stop, and a search is not a level.
func TestEscapeFromABoardReturnsToTheFirstScreenAndNotTheSearch(t *testing.T) {
	t.Parallel()
	a := newApp(t, boardStub())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Enter}, Key{Kind: Esc})
	if got := a.State().Screen; got != "mode" {
		t.Errorf("Screen = %v, want the first screen", got)
	}
}

func TestABoardShowsOnlyWhatHasNotGoneYet(t *testing.T) {
	t.Parallel()
	a := newApp(t, boardStub())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Enter})
	for _, d := range a.screen.(*boardScreen).departures {
		if d.Scheduled < 8*3600 {
			t.Errorf("board holds a departure at %d, which is before now", d.Scheduled)
		}
	}
}

func (s stub) Stop(_ context.Context, id string) (cache.Stop, bool, error) {
	for _, match := range s.stops {
		if match.ID == id {
			return match.Stop, true, nil
		}
	}
	return cache.Stop{}, false, nil
}

// A filter is undone before the screen is. Two escapes leave a filtered list:
// one clears what was typed and the next goes back a level.
func TestEscapeClearsAFilterBeforeItLeavesTheScreen(t *testing.T) {
	t.Parallel()
	a := newApp(t, busy())
	press(t, a, Key{Kind: Enter}, Key{Kind: Char, Rune: '4'})
	if got := a.State(); got.Screen != "routes" || got.Filter != "4" {
		t.Fatalf("setup: screen %q filter %q", got.Screen, got.Filter)
	}

	press(t, a, Key{Kind: Esc})
	if got := a.State(); got.Screen != "routes" || got.Filter != "" {
		t.Errorf("the first escape gave screen %q filter %q, want routes and no filter", got.Screen, got.Filter)
	}

	press(t, a, Key{Kind: Esc})
	if got := a.State().Screen; got != "mode" {
		t.Errorf("the second escape gave %q, want the first screen", got)
	}
}

// A list that a filter emptied is not a list that was empty. The two say
// different things, because one of them means the filter is wrong and the
// other means there is nothing to find.
func TestAFilteredEmptyListAndAnEmptyListSayDifferentThings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   stub
		typed string
		want  string
	}{
		{"nothing runs today", stub{}, "", "nothing here"},
		{"the filter matched none of them", busy(), "zzz", "no matches"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, tc.src)
			press(t, a, Key{Kind: Enter})
			for _, r := range tc.typed {
				press(t, a, Key{Kind: Char, Rune: r})
			}
			if got := a.View(100, 14).Empty; got != tc.want {
				t.Errorf("Empty = %q, want %q", got, tc.want)
			}
		})
	}
}

// A filter narrows what a screen draws and not what it holds. The artifact
// reports every row and the filter beside them, so a reader sees both what was
// there and what was being looked for.
func TestAFilterNarrowsTheScreenAndNotTheArtifact(t *testing.T) {
	t.Parallel()
	a := newApp(t, busy())
	press(t, a, Key{Kind: Enter}, Key{Kind: Char, Rune: '4'}, Key{Kind: Char, Rune: '4'})

	rows, ok := a.Semantic()["rows"].([]any)
	if !ok {
		t.Fatalf("rows = %T, want a list", a.Semantic()["rows"])
	}
	if len(rows) != 2 {
		t.Errorf("the artifact reports %d rows, want both of them", len(rows))
	}
	if drawn := len(a.View(100, 14).Rows); drawn != 1 {
		t.Errorf("the screen drew %d rows, want the one that matched", drawn)
	}
	if got := a.Semantic()["filter"]; got != "44" {
		t.Errorf("filter = %v, want it reported beside the rows", got)
	}
}

// A board whose last bus has gone says so. Drawing nothing is what a board
// that failed to load looks like.
func TestABoardWithNothingLeftSaysSo(t *testing.T) {
	t.Parallel()
	a := newApp(t, stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "LAST STOP"}}},
		board: map[string][]cache.Departure{
			"s1": {{Route: "44", Headsign: "Hurdman", Scheduled: 100}},
		},
	})
	press(t, a, Key{Kind: Char, Rune: 'l'}, Key{Kind: Enter})

	v := a.View(100, 14)
	if len(v.Rows) != 0 {
		t.Fatalf("the board drew %d rows, want none", len(v.Rows))
	}
	if v.Empty != "no more departures today" {
		t.Errorf("Empty = %q, want %q", v.Empty, "no more departures today")
	}
}

// firstScreen is the first screen, refreshed. A test that reads a screen's own
// data asks that screen for it: there is no second view of it to go stale.
func firstScreen(t *testing.T, a *App) *modeScreen {
	t.Helper()
	m, ok := a.screen.(*modeScreen)
	if !ok {
		t.Fatalf("the program is on %T, want the first screen", a.screen)
	}
	return m
}

func TestARailLineCallsAtStationsAndABusRouteAtStops(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mode cache.Mode
		want string
	}{
		{cache.Bus, "Which stop?"},
		{cache.Rail, "Which station?"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := stopsLabel(tc.mode); got != tc.want {
				t.Errorf("stopsLabel = %q, want %q", got, tc.want)
			}
		})
	}
}

// A board reached through a direction shows that route and no other. The stop
// may be served by several, and the board leaves the headsign column out
// precisely because the direction already chose it.
func TestADrilledBoardShowsOnlyItsOwnRoute(t *testing.T) {
	t.Parallel()
	src := stub{
		dirs:       []cache.Direction{{Headsign: "Hurdman", Trips: 7}},
		routeStops: []cache.Stop{{ID: "s1", Code: "1801", Name: "SHARED STOP"}},
		bus:        []cache.Route{{ShortName: "44"}},
		board: map[string][]cache.Departure{
			"s1": {
				{Route: "44", Headsign: "Hurdman", Scheduled: 32400},
				{Route: "48", Headsign: "Hurdman", Scheduled: 33000},
				{Route: "44", Headsign: "Billings Bridge", Scheduled: 34000},
			},
		},
	}
	a := newApp(t, src)
	press(t, a, Key{Kind: Enter}, Key{Kind: Enter}, Key{Kind: Enter}, Key{Kind: Enter})

	b, ok := a.screen.(*boardScreen)
	if !ok {
		t.Fatalf("drilling four levels landed on %T, want a board", a.screen)
	}
	if len(b.departures) != 1 {
		t.Fatalf("the board holds %d departures, want only route 44 toward Hurdman", len(b.departures))
	}
	if b.departures[0].Route != "44" || b.departures[0].Headsign != "Hurdman" {
		t.Errorf("board holds %+v", b.departures[0])
	}
}

// A slash goes back to the first screen, and clears the way back with it. It is
// not a letter, so it can mean something on a list where every letter goes to
// the filter. The exception is the stop search, where a stop name can carry one:
// BILLINGS BRIDGE / BANK.
func TestASlashGoesHomeExceptWhereAStopNameCanHoldOne(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		walk       []Key
		wantScreen string
		wantFilter string
	}{
		{"from a route list", []Key{{Kind: Enter}}, "mode", ""},
		{"from a board", []Key{{Kind: Char, Rune: 'b'}, {Kind: Enter}}, "mode", ""},
		{"from the first screen", nil, "mode", ""},
		{"from the stop search it is a character", []Key{{Kind: Char, Rune: 'b'}}, "search", "b/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, boardStub())
			press(t, a, tc.walk...)
			press(t, a, Key{Kind: Char, Rune: '/'})

			got := a.State()
			if got.Screen != tc.wantScreen || got.Filter != tc.wantFilter {
				t.Errorf("screen %q filter %q, want %q and %q", got.Screen, got.Filter, tc.wantScreen, tc.wantFilter)
			}
		})
	}
}

// Going home clears the way back. Otherwise an escape from four levels down
// jumps to the first screen instead of going up one, which is what pin-onward
// exists to catch.
func TestGoingHomeClearsTheWayBack(t *testing.T) {
	t.Parallel()
	src := boardStub()
	src.bus = []cache.Route{{ShortName: "44"}}
	src.dirs = []cache.Direction{{Headsign: "Hurdman", Trips: 7}}
	src.routeStops = []cache.Stop{{ID: "s1", Code: "1801", Name: "A STOP"}}
	a := newApp(t, src)

	// Two levels down, so the way back is not empty, then home. A stale way
	// back would send the next escape into the screens this walk left.
	press(t, a, Key{Kind: Enter}, Key{Kind: Enter})
	if got := a.State().Screen; got != "directions" {
		t.Fatalf("two levels down is %q, want the directions", got)
	}
	press(t, a, Key{Kind: Char, Rune: '/'})
	if got := a.State().Screen; got != "mode" {
		t.Fatalf("a slash gave %q, want the first screen", got)
	}

	// One level down from home, then back up. It must land on the first screen
	// and not in the route list this walk already left.
	press(t, a, Key{Kind: Enter})
	if got := a.State().Screen; got != "routes" {
		t.Fatalf("one level down is %q, want the routes", got)
	}
	press(t, a, Key{Kind: Esc})
	if got := a.State().Screen; got != "mode" {
		t.Errorf("the escape gave %q, want the first screen", got)
	}
	if len(a.stack) != 0 {
		t.Errorf("the way back holds %d screens, want none", len(a.stack))
	}
}

// cellAt is the cell a row draws in one column. A column is where a field is,
// so naming the column is how a test says which field it means.
func cellAt(t *testing.T, row render.Row, col int) render.Cell {
	t.Helper()
	for _, c := range row.Cells {
		if c.At == col {
			return c
		}
	}
	t.Fatalf("no cell at column %d: %+v", col, row.Cells)
	return render.Cell{}
}

// countdownIn is the countdown cell of a board row. It is the one column named
// by where it ends, so it is found by the span it paints rather than by the cell
// its text happens to start in, which moves with the length of the text.
func countdownIn(t *testing.T, row render.Row) render.Cell {
	t.Helper()
	for _, c := range row.Cells {
		if c.Paints.To == boardWait {
			return c
		}
	}
	t.Fatalf("row has no countdown ending at %d: %+v", boardWait, row.Cells)
	return render.Cell{}
}

// waitCellOf is the style of the countdown on a board a hundred cells wide. The
// countdown is the one column named by where it ends, so it is found by the span
// it paints rather than by the cell its text happens to start in.
func waitCellOf(t *testing.T, row render.Row) render.Style {
	t.Helper()
	return countdownIn(t, row).Style
}

// A route on no list today has no mode. A pin whose route stopped running is
// hidden before this is asked, so nothing draws it, and the answer still has to
// be a mode nobody mistakes for the first one on the screen.
func TestARouteOnNoListHasNoMode(t *testing.T) {
	t.Parallel()
	today := lists{
		cache.Bus:  {{ShortName: "44"}, {ShortName: "48"}},
		cache.Rail: {{ShortName: "1"}},
	}
	for _, tc := range []struct {
		route string
		want  cache.Mode
	}{
		{"44", cache.Bus},
		{"1", cache.Rail},
		{"7", 0},
		// A pin on a whole stop carries no route at all.
		{"", 0},
	} {
		t.Run("route "+tc.route, func(t *testing.T) {
			t.Parallel()
			if got := today.modeOf(tc.route); got != tc.want {
				t.Errorf("modeOf(%q) = %d, want %d", tc.route, got, tc.want)
			}
		})
	}
	// The zero is not the first mode on the screen, which is what makes it safe
	// to mean "none": a board given it draws no mode crumb rather than "Bus".
	if cache.Bus == 0 || cache.Rail == 0 {
		t.Error("a mode is zero, so no mode cannot mean none")
	}
}
