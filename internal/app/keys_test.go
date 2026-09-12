package app

import (
	"testing"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
)

// Which letters are commands, and where.
//
// On a list every letter goes to the filter, so a letter can only mean something
// else on a screen where typing does nothing. That is the departures board, and
// it is the only one. No fixture presses one of these keys, so these tests are
// the whole of the contract for them.

func TestALetterOnAListIsFilteredAndNeverACommand(t *testing.T) {
	t.Parallel()
	// A key guarded on "nothing has been typed yet" is guarded on a condition
	// that holds at the start of every search, so it takes the first letter a
	// person types. 58 stops begin with Q, 139 with J and 221 with K, so
	// searching for QUEENSWAY quit the program on the first keystroke.
	for _, tc := range []struct {
		name string
		open []Key
		want string
	}{
		{"on the first screen", nil, "search"},
		{"on a list of routes", []Key{{Kind: Enter}}, "routes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, busy())
			press(t, a, tc.open...)
			press(t, a,
				Key{Kind: Char, Rune: 'q'},
				Key{Kind: Char, Rune: 'j'},
				Key{Kind: Char, Rune: 'k'})

			got := a.State()
			if got.Filter != "qjk" {
				t.Errorf("Filter = %q, want qjk: every letter belongs to the filter here", got.Filter)
			}
			if got.Screen != tc.want {
				t.Errorf("Screen = %q, want %q", got.Screen, tc.want)
			}
			if a.Leaving() {
				t.Error("the program is leaving, and a letter on a list must not end it")
			}
		})
	}
}

func TestQQuitsFromADepartureBoard(t *testing.T) {
	t.Parallel()
	// The one screen where typing does nothing, so the one screen where a letter
	// can mean something else. Its own hint says so.
	a := newApp(t, boardStub())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Enter})
	if got := a.State().Screen; got != "departures" {
		t.Fatalf("Screen = %q, want the board", got)
	}
	if a.Leaving() {
		t.Fatal("the program was leaving before q was pressed")
	}

	press(t, a, Key{Kind: Char, Rune: 'q'})
	if !a.Leaving() {
		t.Error("q on a board did not end the program")
	}
}

func TestALetterWithNoMeaningOnABoardDoesNothingAtAll(t *testing.T) {
	t.Parallel()
	// A board has no filter, so a letter it does not know is not typed
	// anywhere. It must not quit either: only q does that.
	a := newApp(t, boardStub())
	press(t, a, Key{Kind: Char, Rune: 'b'}, Key{Kind: Enter},
		Key{Kind: Char, Rune: 'j'}, Key{Kind: Char, Rune: 'k'}, Key{Kind: Char, Rune: 'z'})

	got := a.State()
	if got.Screen != "departures" || got.Filter != "" {
		t.Errorf("screen %q with filter %q, want the board with nothing typed", got.Screen, got.Filter)
	}
	if a.Leaving() {
		t.Error("a letter that means nothing ended the program")
	}
}

func TestEveryListSaysEscAndNoListSaysQ(t *testing.T) {
	t.Parallel()
	// The hint is the only thing on screen that says which keys a screen has, so
	// it may not offer one the screen does not.
	for _, tc := range []struct {
		name string
		open []Key
	}{
		{"the first screen", nil},
		{"a list of routes", []Key{{Kind: Enter}}},
		{"a search", []Key{{Kind: Char, Rune: 'b'}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, busy())
			press(t, a, tc.open...)

			hints := a.View(100, 14).Status.Hints
			if want := " · esc"; len(hints) < len(want) || hints[len(hints)-len(want):] != want {
				t.Errorf("the hints read %q, and a list ends at esc", hints)
			}
		})
	}
}

func TestABoardStillOffersQ(t *testing.T) {
	t.Parallel()
	a := newApp(t, stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "FIRST STOP"}}},
		board: departuresAt(3, 30000),
	})
	press(t, a, Key{Kind: Char, Rune: 'f'}, Key{Kind: Enter})

	hints := a.View(100, 14).Status.Hints
	if want := " · esc · q"; len(hints) < len(want) || hints[len(hints)-len(want):] != want {
		t.Errorf("the hints read %q, and a board ends at esc then q", hints)
	}
}

func TestDeleteWithNothingTypedGoesBackALevel(t *testing.T) {
	t.Parallel()
	// The Mac keyboard's delete key, which is the only backspace it has. With a
	// filter it takes a letter off. With nothing typed there is nothing to undo,
	// so it walks back up, and on the first screen that means the program is
	// done: the same floor esc reaches.
	for _, tc := range []struct {
		name    string
		keys    []Key
		screen  string
		filter  string
		leaving bool
	}{
		{"on a list of routes", []Key{{Kind: Enter}, {Kind: Backspace}}, "mode", "", false},
		{"on the first screen", []Key{{Kind: Backspace}}, "mode", "", true},
		{"with a letter typed", []Key{{Kind: Char, Rune: 'b'}, {Kind: Char, Rune: 'i'}, {Kind: Backspace}}, "search", "b", false},
		{"the last letter takes the search with it", []Key{{Kind: Char, Rune: 'b'}, {Kind: Backspace}}, "mode", "", false},
		{"and then the first screen is the floor", []Key{{Kind: Char, Rune: 'b'}, {Kind: Backspace}, {Kind: Backspace}}, "mode", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := newApp(t, busy())
			press(t, a, tc.keys...)

			got := a.State()
			if got.Screen != tc.screen || got.Filter != tc.filter {
				t.Errorf("screen %q with filter %q, want %q and %q", got.Screen, got.Filter, tc.screen, tc.filter)
			}
			if a.Leaving() != tc.leaving {
				t.Errorf("Leaving = %v, want %v", a.Leaving(), tc.leaving)
			}
		})
	}
}
