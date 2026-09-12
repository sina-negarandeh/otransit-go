package app

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
)

// The pins file is the only thing this program leaves behind, and a person is
// meant to be able to open it. So it is read and written in one form, and a line
// somebody typed by hand cannot cost them the lines above it.

func TestPinsAreWrittenInTheFormTheyAreRead(t *testing.T) {
	t.Parallel()
	// A pin from a search writes three fields and one made by drilling writes
	// five, which is what every pin wrote before a route was part of one.
	file := "7581\t1869\tTRANSITWAY / TERMINAL\n" +
		"9030\t8544\tALTA VISTA / CALEDON\t44\tBillings Bridge\n"

	read, err := ParsePins(strings.NewReader(file))
	if err != nil {
		t.Fatalf("ParsePins: %v", err)
	}
	if got := RenderPins(read); got != file {
		t.Errorf("the file came back as\n%q\nwant\n%q", got, file)
	}
}

func TestABadLineDoesNotCostThePinsAroundIt(t *testing.T) {
	t.Parallel()
	// One line with four fields: a route and no headsign. It is reported, and the
	// pin above it is still a pin. A fixture treats the error as fatal, and a
	// person's own file does not.
	read, err := ParsePins(strings.NewReader(
		"7581\t1869\tTRANSITWAY / TERMINAL\n" +
			"9030\t8544\tALTA VISTA / CALEDON\t44\n"))
	if err == nil {
		t.Fatal("a line with a route and no headsign was accepted")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error is %q, and it has to say which line", err)
	}
	if len(read) != 1 || read[0].Stop != "7581" {
		t.Errorf("the pins that parsed are %+v, want the first one", read)
	}
}

func TestAPinKeepsItsPlaceWhenTheFeedRenamesTheStop(t *testing.T) {
	t.Parallel()
	// The name in the file is what the stop was called when it was pinned. The
	// feed rewrites names, and a pin is not a name: it is a stop and a direction.
	stored := Pin{Stop: "s1", Code: "3034", Name: "BILLINGS BRIDGE", Route: "44", Headsign: "Hurdman"}
	renamed := Pin{Stop: "s1", Code: "3034", Name: "BILLINGS BRIDGE 3B", Route: "44", Headsign: "Hurdman"}

	a := newApp(t, stub{})
	a.Pinned([]Pin{stored})

	if !a.kept.has(renamed) {
		t.Error("the renamed stop reads as a different pin")
	}
	a.kept.toggle(renamed)
	if got := a.Pins(); len(got) != 0 {
		t.Errorf("unpinning the renamed stop left %+v", got)
	}
}

func TestAnotherDirectionAtOneStopIsAnotherPin(t *testing.T) {
	t.Parallel()
	// 44 and 48 both end at Billings Bridge and the 48 runs a corridor the 44
	// never touches, so the route belongs to the pin's identity.
	a := newApp(t, stub{})
	a.Pinned([]Pin{{Stop: "s1", Route: "44", Headsign: "Hurdman"}})

	other := Pin{Stop: "s1", Route: "48", Headsign: "Hurdman"}
	if a.kept.has(other) {
		t.Error("another route at the same stop reads as the same pin")
	}
	a.kept.toggle(other)
	if got := a.Pins(); len(got) != 2 {
		t.Errorf("pinning the other route gave %+v, want both", got)
	}
}

// sixStops is a world with six stops a pin can resolve against, which is one
// more than the cap allows.
func sixStops() stub {
	out := stub{board: map[string][]cache.Departure{}}
	for i := 1; i <= 6; i++ {
		id := "s" + strconv.Itoa(i)
		out.stops = append(out.stops, cache.Match{
			Stop: cache.Stop{ID: id, Code: "300" + strconv.Itoa(i), Name: "A STOP"},
		})
		out.board[id] = []cache.Departure{{Route: "44", Headsign: "Hurdman", Scheduled: 32400}}
	}
	return out
}

func TestTheFirstScreenKeepsFiveBoardsAndSaysSoAtTheSixth(t *testing.T) {
	t.Parallel()
	// The cap is the first screen's arithmetic: eight rows, two for the modes,
	// and one for the blank that says the two groups are different kinds of
	// thing. A sixth pin would eat the blank.
	a := newApp(t, sixStops())

	var kept []Pin
	for i := 2; i <= 6; i++ {
		id := "s" + strconv.Itoa(i)
		kept = append(kept, Pin{Stop: id, Code: "300" + strconv.Itoa(i), Name: "A STOP"})
	}
	a.Pinned(kept)
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	// The search opens the one stop that is not kept.
	press(t, a, Key{Kind: Char, Rune: 'a'}, Key{Kind: Enter})
	b, ok := a.screen.(*boardScreen)
	if !ok {
		t.Fatalf("the walk reached %T, want a board", a.screen)
	}
	if got := b.keepHint(a); got != "pins full · " {
		t.Errorf("the board offers %q at the cap, want pins full", got)
	}

	press(t, a, Key{Kind: Char, Rune: 'p'})
	if got := a.Pins(); len(got) != 5 {
		t.Errorf("a sixth pin was kept: %d pins", len(got))
	}
}

func TestAPinTheCacheCannotFindTakesNoRoomFromTheCap(t *testing.T) {
	t.Parallel()
	// A stop that vanished from the export keeps its pin and draws no row, so it
	// costs nothing: the cap counts what is on the screen. Both the stop and the
	// route can come back with the next export, which is why the line is kept.
	a := newApp(t, sixStops())

	var kept []Pin
	for i := range 5 {
		kept = append(kept, Pin{Stop: "gone" + strconv.Itoa(i)})
	}
	a.Pinned(kept)
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	press(t, a, Key{Kind: Char, Rune: 'a'}, Key{Kind: Enter})
	b := a.screen.(*boardScreen)
	if got := b.keepHint(a); got != "p pin · " {
		t.Errorf("the board offers %q, want p pin: five pins nothing can find take no room", got)
	}

	press(t, a, Key{Kind: Char, Rune: 'p'})
	if got := a.Pins(); len(got) != 6 {
		t.Errorf("the sixth pin was refused: %d pins", len(got))
	}
}

// p keeps a board. It can only mean that here, because on a list every letter
// goes to the filter.
func TestPKeepsABoardAndIsALetterEverywhereElse(t *testing.T) {
	t.Parallel()
	a := newApp(t, boardStub())

	press(t, a, Key{Kind: Char, Rune: 'p'})
	if got := a.State().Filter; got != "p" {
		t.Fatalf("on the search screen p gave filter %q, want %q", got, "p")
	}
	if len(a.kept.stored) != 0 {
		t.Errorf("typing p on a list kept something: %v", a.kept.stored)
	}

	press(t, a, Key{Kind: Esc}, Key{Kind: Char, Rune: 'b'}, Key{Kind: Enter})
	press(t, a, Key{Kind: Char, Rune: 'p'})
	if len(a.kept.stored) != 1 {
		t.Fatalf("pins = %v, want one", a.kept.stored)
	}
	if a.kept.stored[0].Stop != "s1" {
		t.Errorf("pins[0] = %+v, want the stop the board was for", a.kept.stored[0])
	}
}
func TestPressingPTwiceStopsKeepingTheBoard(t *testing.T) {
	t.Parallel()
	a := newApp(t, boardStub())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Enter})
	press(t, a, Key{Kind: Char, Rune: 'p'}, Key{Kind: Char, Rune: 'p'})
	if len(a.kept.stored) != 0 {
		t.Errorf("pins = %v, want none after a second p", a.kept.stored)
	}
}
func TestAKeptBoardSaysUnpinAndReportsItself(t *testing.T) {
	t.Parallel()
	a := newApp(t, boardStub())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Enter})

	if got := a.Semantic()["pinned"]; got != "unpinned" {
		t.Errorf("pinned = %v, want unpinned", got)
	}
	if hints := a.View(100, 14).Status.Hints; !strings.Contains(hints, "p pin ") {
		t.Errorf("hints = %q, want them to offer a pin", hints)
	}

	press(t, a, Key{Kind: Char, Rune: 'p'})
	if got := a.Semantic()["pinned"]; got != "pinned" {
		t.Errorf("pinned = %v, want pinned", got)
	}
	if hints := a.View(100, 14).Status.Hints; !strings.Contains(hints, "p unpin ") {
		t.Errorf("hints = %q, want them to offer an unpin", hints)
	}
}

// A pin made by drilling carries its route and direction. 44 and 48 both end
// at Billings Bridge by roads that do not meet, so a pin on one must not offer
// the other.
func TestAPinMadeByDrillingCarriesItsRouteAndDirection(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		board *boardScreen
		want  Pin
	}{
		{
			"reached by drilling",
			&boardScreen{stop: cache.Stop{ID: "s1"}, route: "44", headsign: "Hurdman"},
			Pin{Stop: "s1", Route: "44", Headsign: "Hurdman"},
		},
		{
			"reached by searching",
			&boardScreen{stop: cache.Stop{ID: "s1"}},
			Pin{Stop: "s1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.board.pin(); got != tc.want {
				t.Errorf("pin() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A pin stores an id, and an update replaces the whole database. A stop that
// no longer resolves is left off the screen and kept in the store, because it
// can come back.
func TestAPinThatNoLongerResolvesIsHiddenAndNotDeleted(t *testing.T) {
	t.Parallel()
	a := newApp(t, boardStub())
	a.kept.stored = []Pin{{Stop: "gone"}, {Stop: "s1"}}
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	rows := firstScreen(t, a).choices
	if len(rows) != 3 {
		t.Fatalf("the first screen drew %d rows, want one pin and two modes", len(rows))
	}
	if rows[0].row.Primary != "FIRST STOP" {
		t.Errorf("rows[0] = %q, want the pin that still resolves", rows[0].row.Primary)
	}
	if len(a.kept.stored) != 2 {
		t.Errorf("pins = %v, want both kept", a.kept.stored)
	}
}

// A pin opened from its own row is the same pin. It reported itself unpinned,
// offered to pin again, and a second press left two pins for one stop.
func TestAPinOpenedFromItsRowIsStillThatPin(t *testing.T) {
	t.Parallel()
	src := boardStub()
	// The pin carries a route, so that route has to still run for the row to
	// show at all.
	src.dirs = []cache.Direction{{Headsign: "Hurdman", Trips: 7}}
	a := newApp(t, src)
	a.kept.stored = []Pin{{Stop: "s1", Route: "44", Headsign: "Hurdman"}}
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	press(t, a, Key{Kind: Enter})
	b, ok := a.screen.(*boardScreen)
	if !ok {
		t.Fatalf("entering the pin row opened %T", a.screen)
	}
	if got := b.pin(); got.key() != a.kept.stored[0].key() {
		t.Errorf("the board's pin is %+v, want the stored %+v", got, a.kept.stored[0])
	}
	if got := b.keepHint(a); got != "p unpin · " {
		t.Errorf("the board offers %q, want p unpin", got)
	}

	press(t, a, Key{Kind: Char, Rune: 'p'})
	if len(a.kept.stored) != 0 {
		t.Errorf("pins = %+v, want the one pin removed rather than a second added", a.kept.stored)
	}
}

// A pin whose route no longer runs is hidden and kept, because the route can
// come back. A pin whose stop has nothing left today still draws.
func TestAPinIsHiddenWhenItsRouteStopsRunning(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		dirs  []cache.Direction
		shown bool
	}{
		{"the route still runs", []cache.Direction{{Headsign: "Hurdman", Trips: 7}}, true},
		{"the route stopped running", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := boardStub()
			src.dirs = tc.dirs
			a := newApp(t, src)
			a.kept.stored = []Pin{{Stop: "s1", Route: "44", Headsign: "Hurdman"}}
			if err := a.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}

			pins := 0
			for _, c := range firstScreen(t, a).choices {
				if c.pin != nil {
					pins++
				}
			}
			if want := 0; !tc.shown && pins != want {
				t.Errorf("the screen drew %d pin rows, want %d", pins, want)
			}
			if tc.shown && pins != 1 {
				t.Errorf("the screen drew %d pin rows, want 1", pins)
			}
			if len(a.kept.stored) != 1 {
				t.Errorf("pins = %+v, want it kept either way", a.kept.stored)
			}
		})
	}
}

// A pin whose stop has nothing left today keeps its row and says so. Dropping
// it would make a pin vanish after the last bus and reappear in the morning.
func TestAPinWithNothingLeftTodayKeepsItsRowAndSaysSo(t *testing.T) {
	t.Parallel()
	a := newApp(t, stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "LAST STOP"}}},
		board: map[string][]cache.Departure{
			"s1": {{Route: "44", Headsign: "Hurdman", Scheduled: 100}},
		},
	})
	a.kept.stored = []Pin{{Stop: "s1"}}
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	first := firstScreen(t, a)
	if len(first.choices) != 3 || first.choices[0].pin == nil {
		t.Fatalf("the first screen drew %d rows, want a pin above the modes", len(first.choices))
	}
	if first.choices[0].next != nil {
		t.Errorf("the pin row carries a departure, want none")
	}

	var texts []string
	for _, c := range a.View(100, 14).Rows[0].Cells {
		texts = append(texts, c.Text)
	}
	if !slices.Contains(texts, "none left today") {
		t.Errorf("the pin row draws %q, want it to say none left today", texts)
	}
}

// A pins file is tab separated. It carries the stop id, and a route and
// headsign when the pin was made by drilling. The code and the name are in it
// for a person reading the file, and are resolved through the cache rather than
// trusted.
func TestAPinsFileIsReadByIDRouteAndHeadsign(t *testing.T) {
	t.Parallel()
	got, err := ParsePins(strings.NewReader(
		"7581\t1869\tTRANSITWAY / TERMINAL\n" +
			"9030\t8544\tALTA VISTA / CALEDON\t44\tBillings Bridge\n" +
			"\n" +
			"# a comment\n"))
	if err != nil {
		t.Fatalf("ParsePins: %v", err)
	}
	want := []Pin{
		{Stop: "7581", Code: "1869", Name: "TRANSITWAY / TERMINAL"},
		{Stop: "9030", Code: "8544", Name: "ALTA VISTA / CALEDON", Route: "44", Headsign: "Billings Bridge"},
	}
	if len(got) != len(want) {
		t.Fatalf("ParsePins = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pins[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
func TestAMalformedPinsLineIsAnError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"no stop id", "\t1869\tTRANSITWAY\n"},
		{"one field", "7581\n"},
		{"a route with no headsign", "7581\t1869\tTRANSITWAY\t44\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := ParsePins(strings.NewReader(tc.src)); err == nil {
				t.Errorf("ParsePins(%q) = nil error, want one", tc.src)
			}
		})
	}
}
