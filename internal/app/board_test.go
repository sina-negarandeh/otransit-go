package app

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/realtime"
)

// How far each screen looks down a stop's timetable.
//
// A stop in the middle of the city has hundreds of departures left today, and
// every one of them costs a row the program builds and then throws away. The
// numbers are written out here rather than computed from the constants, because
// a test that asks the code what it does cannot fail.

// departuresAt is n departures at s1, a minute apart, each on its own trip.
func departuresAt(n, from int) map[string][]cache.Departure {
	var calls []cache.Departure
	for i := range n {
		calls = append(calls, cache.Departure{
			// Each row carries its own headsign, so a test can say which one it
			// is looking at.
			Trip: "t" + strconv.Itoa(i), Route: "44", Headsign: "Stop " + strconv.Itoa(i),
			Scheduled: from + i*60,
		})
	}
	return map[string][]cache.Departure{"s1": calls}
}

// count is how many departures a screen holds, read out of what it reports
// rather than off the rows it draws: the screen clips at eight whatever it was
// given.
func count(t *testing.T, a *App) int {
	t.Helper()
	out := a.Semantic()
	rows, ok := out["departures"].([]any)
	if !ok {
		t.Fatalf("the screen reports no departures: %v", out["screen"])
	}
	return len(rows)
}

func TestABoardHoldsTwentyFourDeparturesWhateverTheStopHas(t *testing.T) {
	t.Parallel()
	// Eight rows are drawn and twenty-four are held. The other sixteen are
	// headroom for the feed: a bus running late can arrive after one scheduled
	// behind it, and a board with no headroom leaves out the bus you could
	// actually catch.
	a := newApp(t, stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "FIRST STOP"}}},
		board: departuresAt(100, 30000),
	})
	press(t, a, Key{Kind: Char, Rune: 'f'}, Key{Kind: Enter})

	if got := count(t, a); got != 24 {
		t.Errorf("the board holds %d departures, want 24", got)
	}
}

func TestAStopWithFewerDeparturesThanTheBoardHoldsKeepsThemAll(t *testing.T) {
	t.Parallel()
	// The cap is a ceiling and not a promise. A quiet stop at the end of the day
	// has three left, and three is what the board holds.
	a := newApp(t, stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "FIRST STOP"}}},
		board: departuresAt(3, 30000),
	})
	press(t, a, Key{Kind: Char, Rune: 'f'}, Key{Kind: Enter})

	if got := count(t, a); got != 3 {
		t.Errorf("the board holds %d departures, want 3", got)
	}
}

// A pin row draws one departure, so it looks eight down the timetable and not
// twenty-four. The difference shows when the feed promotes a row: a prediction
// on the twelfth departure reaches a board and cannot reach a pin.
func TestAPinLooksEightDeparturesDownAndABoardLooksFurther(t *testing.T) {
	t.Parallel()
	// The twelfth departure, counting from the first.
	const promotedTrip, promoted = "t11", "Stop 11"

	// The feed says the twelfth trip arrives before every scheduled one.
	feed := `{"Entity":[{"TripUpdate":{
		"Trip":{"TripId":"` + promotedTrip + `"},
		"StopTimeUpdate":[{"StopId":"s1","Arrival":{"Time":29700,"HasTime":true}}]}}]}`
	live, err := realtime.Parse(strings.NewReader(feed))
	if err != nil {
		t.Fatalf("realtime.Parse: %v", err)
	}

	src := stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "FIRST STOP"}}},
		board: departuresAt(20, 30000),
	}

	// The board holds twenty-four, so it holds the promoted trip, and the feed
	// puts it first.
	deep, err := newAppOn(t, src, theDay, theHour, Feed{Live: live}).board(context.Background(), "s1", "", "", boardFetch)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if topOf(deep) != promoted {
		t.Fatalf("the board draws %q first, want the trip the feed moved up", topOf(deep))
	}

	// A pin holds eight, so the twelfth departure is not one of them and nothing
	// the feed says about it can reach the row.
	shallow, err := newAppOn(t, src, theDay, theHour, Feed{Live: live}).board(context.Background(), "s1", "", "", pinFetch)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	if len(shallow) != 8 {
		t.Errorf("the pin holds %d departures, want 8", len(shallow))
	}
	if topOf(shallow) == promoted {
		t.Errorf("the pin drew %q, which is twelve departures down and past the eight it holds", promoted)
	}
}

// topOf is the departure drawn at the top, named by its headsign, or empty when
// nothing is drawn.
func topOf(board []Departure) string {
	if len(board) == 0 {
		return ""
	}
	return board[0].Headsign
}
