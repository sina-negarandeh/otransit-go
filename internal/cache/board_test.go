package cache

import (
	"context"
	"slices"
	"strconv"
	"testing"
)

// The departures at one stop, on one axis.
//
// A board reads two service days: a trip written 25:10 on Friday is the one a
// person catches at 01:10 on Saturday, so yesterday's late rows are shifted back
// by a day and both days sit on one axis. Everything downstream assumes that
// axis, including a prediction.

func TestABoardHoldsTheTripsThatCallAtTheStopInOrder(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.stop("s1", "3034", "BILLINGS BRIDGE 3B")
	for i, arr := range []int{50400, 22500, 32400} {
		w.trip("t"+string(rune('a'+i)), "44", "fri", "Hurdman")
		w.calls("t"+string(rune('a'+i)), "s1", arr)
	}

	got, err := w.Departures(context.Background(), "s1", friday, "20260910")
	if err != nil {
		t.Fatalf("Departures: %v", err)
	}
	var at []int
	for _, d := range got {
		at = append(at, d.Scheduled)
	}
	if !slices.Equal(at, []int{22500, 32400, 50400}) {
		t.Errorf("Departures = %v, want them in order", at)
	}
	if got[0].Route != "44" || got[0].Headsign != "Hurdman" {
		t.Errorf("first row = %q %q", got[0].Route, got[0].Headsign)
	}
}

// A trip at 25:10 on Friday is the one a person catches at 01:10 on Saturday.
// A board queries yesterday as well and shifts those rows back by a day, so
// both sit on one axis and the ordering means something.
func TestATripPastMidnightAppearsOnTheNextDayShiftedBackADay(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.service("sat", [7]int{0, 0, 0, 0, 0, 1, 0}, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.stop("s1", "3034", "BILLINGS BRIDGE 3B")
	// 25:10 on Friday, which is 01:10 on Saturday.
	w.trip("late", "44", "fri", "Hurdman")
	w.calls("late", "s1", 90600)
	// 06:00 on Saturday itself.
	w.trip("morning", "44", "sat", "Hurdman")
	w.calls("morning", "s1", 21600)

	got, err := w.Departures(context.Background(), "s1", saturday, friday)
	if err != nil {
		t.Fatalf("Departures: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Departures = %d rows, want 2", len(got))
	}
	if got[0].Scheduled != 90600-86400 || !got[0].AfterMidnight {
		t.Errorf("first row = %d after_midnight %v, want 4200 and true", got[0].Scheduled, got[0].AfterMidnight)
	}
	if got[1].Scheduled != 21600 || got[1].AfterMidnight {
		t.Errorf("second row = %d after_midnight %v, want 21600 and false", got[1].Scheduled, got[1].AfterMidnight)
	}
}

// Yesterday contributes only what reaches past midnight. A trip at 09:00 on
// Friday is not a thing anyone can catch on Saturday.
func TestYesterdayContributesOnlyTheTripsThatRunPastMidnight(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.stop("s1", "3034", "BILLINGS BRIDGE 3B")
	w.trip("daytime", "44", "fri", "Hurdman")
	w.calls("daytime", "s1", 32400)

	got, err := w.Departures(context.Background(), "s1", saturday, friday)
	if err != nil {
		t.Fatalf("Departures: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Departures = %v, want none", got)
	}
}

// Two routes can call at one stop in the same second. The order of a board has
// to be settled by the query rather than left to whatever the database returns,
// because a sort cannot break a tie its input did not order.
func TestTwoDeparturesInOneSecondKeepTheOrderTheExportListsThemIn(t *testing.T) {
	t.Parallel()
	// Two routes can call in the same second, and something has to decide which
	// row is drawn first. Nothing in the data says, so the answer is the order
	// the export wrote them in, which the cache keeps as it loads them.
	//
	// A stronger rule was tried first here: order a tie by route number, and
	// assert only that the answer is the same whatever order the rows arrived
	// in. It is tidier and it is wrong, and no fixture could tell: the two rules
	// agree on every one of them and disagree on the real feed.
	for _, tc := range []struct {
		name  string
		order []string
	}{
		{"48 written first", []string{"48", "44"}},
		{"44 written first", []string{"44", "48"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			w.service("fri", fridays, "20260830", "20260920")
			w.route("48", "48", "Hurdman <> Carleton", 3, 55)
			w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
			w.stop("s1", "3034", "SHARED STOP")
			for i, route := range tc.order {
				id := "t" + strconv.Itoa(i)
				w.trip(id, route, "fri", "Hurdman")
				w.calls(id, "s1", 32400)
			}

			got, err := w.Departures(context.Background(), "s1", friday, "20260910")
			if err != nil {
				t.Fatalf("Departures: %v", err)
			}
			if len(got) != 2 {
				t.Fatalf("Departures = %d rows, want 2", len(got))
			}
			if seen := []string{got[0].Route, got[1].Route}; !slices.Equal(seen, tc.order) {
				t.Errorf("the second came back as %v, want %v", seen, tc.order)
			}
		})
	}
}
func TestARowBorrowedFromYesterdayComesAfterTodaysAtTheSameSecond(t *testing.T) {
	t.Parallel()
	// 25:00 yesterday and 01:00 today are the same moment on one axis, so the
	// two rows tie and one has to be drawn first. Today's is, because today is
	// the day the board is about.
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.service("sat", [7]int{0, 0, 0, 0, 0, 1, 0}, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.stop("s1", "3034", "SHARED STOP")
	// Yesterday's trip runs at 25:00, which is 01:00 today.
	w.trip("late", "44", "fri", "Yesterday")
	w.calls("late", "s1", 25*3600)
	w.trip("early", "44", "sat", "Today")
	w.calls("early", "s1", 3600)

	got, err := w.Departures(context.Background(), "s1", saturday, friday)
	if err != nil {
		t.Fatalf("Departures: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Departures = %d rows, want 2", len(got))
	}
	if got[0].Headsign != "Today" || got[1].Headsign != "Yesterday" {
		t.Errorf("the order is %q then %q, want today's row first", got[0].Headsign, got[1].Headsign)
	}
}
func TestADepartureNamesItsTrip(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("S", everyDay, "20260101", "20261231")
	w.route("44", "44", "Billings <> Hurdman", 3, 1)
	w.stop("7581", "1869", "TRANSITWAY / TERMINAL")
	w.trip("early", "44", "S", "Billings Bridge")
	w.trip("later", "44", "S", "Billings Bridge")
	w.calls("early", "7581", 30000)
	w.calls("later", "7581", 31000)

	got, err := w.Departures(t.Context(), "7581", friday, "20260910")
	if err != nil {
		t.Fatalf("Departures: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("drew %d departures, want 2", len(got))
	}
	for i, want := range []string{"early", "later"} {
		if got[i].Trip != want {
			t.Errorf("departure %d is trip %q, want %q", i, got[i].Trip, want)
		}
	}
}
