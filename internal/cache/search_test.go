package cache

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"testing"
)

// Finding a stop by what a person typed: what matches, in what order, how far the
// search looks, and what each row says about the stop it found.
//
// Every trap here is a property of the feed. A stop_code covers several
// platforms, a platform_code is rare and a number in a name is not one, and three
// stops share the name TERMINAL / SANDFORD FLEMING.

func TestASearchFindsOnlyStopsSomethingCallsAtOnTheDate(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "fri", "Hurdman")
	w.stop("s1", "3034", "BILLINGS BRIDGE 3B")
	w.stop("s2", "3035", "BILLINGS BRIDGE 4B") // in the cache, nothing calls
	w.calls("t1", "s1", 32400)

	for _, tc := range []struct {
		name, date string
		want       []string
	}{
		{"the day the service runs", friday, []string{"s1"}},
		{"the day it does not", saturday, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := w.SearchStops(context.Background(), "billings", tc.date)
			if err != nil {
				t.Fatalf("SearchStops: %v", err)
			}
			ids := make([]string, len(got))
			for i, s := range got {
				ids[i] = s.ID
			}
			if !slices.Equal(ids, tc.want) {
				t.Errorf("SearchStops(%s) = %v, want %v", tc.date, ids, tc.want)
			}
		})
	}
}
func TestASearchMatchesANameOrAStopCodeAndIgnoresCase(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	w.stop("s1", "3034", "BILLINGS BRIDGE 3B")
	w.calls("t1", "s1", 32400)

	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"a name in lower case", "billings", 1},
		{"a name in upper case", "BILLINGS", 1},
		{"part of a name", "bridge", 1},
		{"the stop code", "3034", 1},
		{"a code that is not there", "9999", 0},
		{"a name that is not there", "hurdman", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := w.SearchStops(context.Background(), tc.query, saturday)
			if err != nil {
				t.Fatalf("SearchStops: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("SearchStops(%q) = %d rows, want %d", tc.query, len(got), tc.want)
			}
		})
	}
}

// Backspacing a search down to nothing must clear the list. An empty pattern
// matched every served stop, and the status bar reported that as the count.
func TestAnEmptySearchQueryFindsNothingRatherThanEverything(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	w.stop("s1", "3034", "BILLINGS BRIDGE 3B")
	w.stop("s2", "3035", "HURDMAN A")
	w.calls("t1", "s1", 32400)
	w.calls("t1", "s2", 32500)

	got, _, err := w.SearchStops(context.Background(), "", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("SearchStops(\"\") = %d stops, want none", len(got))
	}
}
func TestALikeWildcardIsSearchedForAndNotActedOn(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	w.stop("s1", "3034", "BILLINGS BRIDGE 3B")
	w.stop("s2", "3035", "HURDMAN A")
	w.calls("t1", "s1", 32400)
	w.calls("t1", "s2", 32500)

	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"a per cent sign matches no stop name", "%", 0},
		{"an underscore is not any character", "3_34", 0},
		{"a plain query still matches", "3034", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, _, err := w.SearchStops(context.Background(), tc.query, saturday)
			if err != nil {
				t.Fatalf("SearchStops: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("SearchStops(%q) = %d stops, want %d", tc.query, len(got), tc.want)
			}
		})
	}
}
func TestASearchRowCarriesTheRouteAndHeadsignThatCallThere(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("48", "48", "Hurdman <> Carleton", 3, 55)
	w.trip("t1", "48", "all", "Carleton")
	w.stop("s1", "3034", "BILLINGS BRIDGE / BANK")
	w.calls("t1", "s1", 32400)

	got, _, err := w.SearchStops(context.Background(), "billings", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("SearchStops = %d rows, want 1", len(got))
	}
	if !slices.Equal(got[0].Routes, []string{"48"}) || got[0].Toward != "Carleton" {
		t.Errorf("row = routes %v toward %q, want [48] and Carleton", got[0].Routes, got[0].Toward)
	}
}

// A platform is a column. CANTERBURY / AD. 860 carries a municipal address in
// its name and no platform at all, and 5,644 of the 5,859 stops in the real
// feed are the same. Reading a number out of a name would be wrong on nearly
// all of them.
func TestAPlatformIsReadFromItsColumnAndNeverFromTheName(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("48", "48", "Hurdman <> Carleton", 3, 55)
	w.trip("t1", "48", "all", "Hurdman")
	w.exec(`INSERT INTO stops VALUES ('s1','3034','BILLINGS BRIDGE 3B',0,0,0,'','3B')`)
	w.exec(`INSERT INTO stops VALUES ('s2','7275','CANTERBURY / AD. 860',0,0,0,'',NULL)`)
	w.calls("t1", "s1", 32400)
	w.calls("t1", "s2", 32500)

	got, _, err := w.SearchStops(context.Background(), "b", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	byName := map[string]Match{}
	for _, s := range got {
		byName[s.Name] = s
	}
	if p := byName["BILLINGS BRIDGE 3B"].Platform; p != "3B" {
		t.Errorf("BILLINGS BRIDGE 3B has platform %q, want %q", p, "3B")
	}
	if p := byName["CANTERBURY / AD. 860"].Platform; p != "" {
		t.Errorf("CANTERBURY / AD. 860 has platform %q, want none", p)
	}
}

// A search stops counting at 25. The status bar says 25+ rather than a number,
// because the query stopped looking rather than found exactly that many.
func TestASearchStopsAtTwentyFiveAndSaysThereAreMore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	for i := range 30 {
		id := "s" + strconv.Itoa(i)
		w.stop(id, "30"+strconv.Itoa(i), fmt.Sprintf("WALKLEY %02d", i))
		w.calls("t1", id, 30000+i)
	}

	got, more, err := w.SearchStops(context.Background(), "walkley", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 25 {
		t.Errorf("SearchStops = %d rows, want 25", len(got))
	}
	if !more {
		t.Error("more = false, want true when the search stopped short")
	}
}
func TestASearchThatFindsExactlyTwentyFiveStillSaysThereAreMore(t *testing.T) {
	t.Parallel()
	// At the cap the count is the cap, and not a total. Twenty-five matches and
	// twenty-five thousand look the same from here, because the query stopped
	// looking at the same place in both.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	for i := range 25 {
		id := "s" + strconv.Itoa(i)
		w.stop(id, "30"+strconv.Itoa(i), fmt.Sprintf("WALKLEY %02d", i))
		w.calls("t1", id, 30000+i)
	}

	got, more, err := w.SearchStops(context.Background(), "walkley", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 25 || !more {
		t.Errorf("SearchStops = %d rows, more = %v, want 25 and true", len(got), more)
	}
}
func TestASearchThatFindsEverythingSaysThereAreNoMore(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	w.stop("s1", "3034", "BILLINGS BRIDGE")
	w.calls("t1", "s1", 32400)

	got, more, err := w.SearchStops(context.Background(), "billings", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 || more {
		t.Errorf("SearchStops = %d rows, more = %v, want 1 and false", len(got), more)
	}
}
func TestASearchLooksPastTheLimitForStopsWithServiceButNotForever(t *testing.T) {
	t.Parallel()
	// The service filter is applied to the stops the name match already chose,
	// so a platform nothing calls at today costs one of the candidates rather
	// than being invisible. The candidate list is four times the limit: past
	// that the search gives up rather than reading every stop in the city.
	//
	// So a hundred dead platforms in front of a live one hide it, and that is a
	// bound rather than a bug.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	// A hundred and one names that sort before the one with service.
	for i := range 101 {
		w.stop("dead"+strconv.Itoa(i), "9"+strconv.Itoa(i), fmt.Sprintf("WALKLEY A%03d", i))
	}
	w.stop("alive", "3034", "WALKLEY Z")
	w.calls("t1", "alive", 32400)

	got, _, err := w.SearchStops(context.Background(), "walkley", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("SearchStops = %v, want nothing: the live stop is past the candidates", got)
	}

	// One fewer dead platform, and the live one is inside the candidates.
	w2 := newWorld(t)
	w2.service("all", everyDay, "20260830", "20260920")
	w2.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w2.trip("t1", "44", "all", "Hurdman")
	for i := range 99 {
		w2.stop("dead"+strconv.Itoa(i), "9"+strconv.Itoa(i), fmt.Sprintf("WALKLEY A%03d", i))
	}
	w2.stop("alive", "3034", "WALKLEY Z")
	w2.calls("t1", "alive", 32400)

	got, _, err = w2.SearchStops(context.Background(), "walkley", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 || got[0].ID != "alive" {
		t.Errorf("SearchStops = %v, want the one stop with service", got)
	}
}
func TestASearchOffersACodeMatchBeforeANameMatch(t *testing.T) {
	t.Parallel()
	// Typing 44 is asking for stop 44 first. A stop whose code begins with what
	// was typed comes before one whose name does, and a code that is exactly
	// what was typed comes before everything.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	for i, st := range []struct{ id, code, name string }{
		{"s1", "9001", "44 RIDEAU"},     // the name begins with it
		{"s2", "4401", "ZZ BASELINE"},   // the code begins with it
		{"s3", "44", "ZZ HURDMAN"},      // the code is it
		{"s4", "9440", "ZZ ALTA VISTA"}, // the code merely holds it
	} {
		w.stop(st.id, st.code, st.name)
		w.calls("t1", st.id, 30000+i)
	}

	got, _, err := w.SearchStops(context.Background(), "44", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	ids := make([]string, len(got))
	for i, m := range got {
		ids[i] = m.ID
	}
	if want := []string{"s3", "s2", "s1", "s4"}; !slices.Equal(ids, want) {
		t.Errorf("SearchStops = %v, want %v: the code exactly, the code first, the name first, then the rest", ids, want)
	}
}
func TestASearchOfSeveralWordsWantsThemAllAndNotInThatOrder(t *testing.T) {
	t.Parallel()
	// "somerset bank" is a person naming two things about one stop. Matching
	// the phrase as typed finds nothing, because the feed writes it the other
	// way round.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	w.stop("s1", "3034", "BANK / SOMERSET W")
	w.calls("t1", "s1", 32400)
	w.stop("s2", "3035", "BANK / GLADSTONE")
	w.calls("t1", "s2", 32500)

	got, _, err := w.SearchStops(context.Background(), "somerset bank", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 || got[0].ID != "s1" {
		t.Errorf("SearchStops = %v, want the one stop both words are about", got)
	}
}
func TestAStopsDestinationIsTheHeadsignMostOfItsTripsCarry(t *testing.T) {
	t.Parallel()
	// The one line on a search row that says where this stop takes you. It is
	// the busiest headsign across every route calling there, and not the
	// headsign of whichever route happens to be listed first.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("10", "10", "Somewhere", 3, 1)
	w.route("105", "105", "Elsewhere", 3, 99)
	w.stop("s1", "3034", "BILLINGS BRIDGE")
	// Route 10 calls once, towards Lyon. Route 105 calls three times, towards
	// Uplands. The first route listed is 10, and the destination is Uplands.
	w.trip("t1", "10", "all", "Lyon")
	w.calls("t1", "s1", 30000)
	for i, id := range []string{"t2", "t3", "t4"} {
		w.trip(id, "105", "all", "Uplands")
		w.calls(id, "s1", 31000+i)
	}

	got, _, err := w.SearchStops(context.Background(), "billings", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("SearchStops = %d rows, want 1", len(got))
	}
	if got[0].Toward != "Uplands" {
		t.Errorf("Toward = %q, want Uplands, which three of the four trips carry", got[0].Toward)
	}
}
func TestTwoDestinationsThatTieAreDecidedByName(t *testing.T) {
	t.Parallel()
	// A tie is common at a stop two routes share, and the label must not flicker
	// between runs. The name decides it, and the first alphabetically wins.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("10", "10", "Somewhere", 3, 1)
	w.route("105", "105", "Elsewhere", 3, 99)
	w.stop("s1", "3034", "BILLINGS BRIDGE")
	w.trip("t1", "10", "all", "Uplands")
	w.calls("t1", "s1", 30000)
	w.trip("t2", "105", "all", "Lyon")
	w.calls("t2", "s1", 31000)

	got, _, err := w.SearchStops(context.Background(), "billings", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 || got[0].Toward != "Lyon" {
		t.Errorf("Toward = %v, want Lyon, which comes first by name", got)
	}
}
func TestTheRoutesOnASearchRowAreOrderedByTheirNumber(t *testing.T) {
	t.Parallel()
	// The same order as the route list, for the same reason, and each route once
	// however many of its trips call here.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.stop("s1", "3034", "BILLINGS BRIDGE")
	for i, name := range []string{"105", "E1", "6", "44"} {
		w.route(name, name, "somewhere", 3, 100-i)
		w.trip("t"+strconv.Itoa(i), name, "all", "Hurdman")
		w.calls("t"+strconv.Itoa(i), "s1", 30000+i)
		// The same route again, going the other way. The two directions are two
		// rows of the query, so a route calling twice is still named once.
		//
		// The second trip carried the same headsign here at first, which the
		// query groups into one row: the name could not repeat, so the test
		// could not fail.
		w.trip("u"+strconv.Itoa(i), name, "all", "Tunney's Pasture")
		w.calls("u"+strconv.Itoa(i), "s1", 40000+i)
	}

	got, _, err := w.SearchStops(context.Background(), "billings", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("SearchStops = %d rows, want 1", len(got))
	}
	if want := []string{"6", "44", "105", "E1"}; !slices.Equal(got[0].Routes, want) {
		t.Errorf("Routes = %v, want %v", got[0].Routes, want)
	}
}

// A stop served by more than one route names them all. TRANSITWAY / TERMINAL
// carries 44 and 48, and offering only the first would hide a bus a person
// could catch.
func TestAStopServedBySeveralRoutesNamesThemAll(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("48", "48", "Hurdman <> Carleton", 3, 55)
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t48", "48", "all", "Billings Bridge")
	w.trip("t44", "44", "all", "Billings Bridge")
	w.stop("s1", "1869", "TRANSITWAY / TERMINAL")
	w.calls("t48", "s1", 32400)
	w.calls("t44", "s1", 32500)

	got, _, err := w.SearchStops(context.Background(), "transitway", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("SearchStops = %d rows, want 1", len(got))
	}
	// By number, not the order they were inserted in.
	if want := []string{"44", "48"}; !slices.Equal(got[0].Routes, want) {
		t.Errorf("Routes = %v, want %v", got[0].Routes, want)
	}
}

// A pin stores a stop id. An update replaces the whole database, so that id
// can point at nothing, and the row is resolved through the current cache
// every time rather than trusted.
func TestAStopIsLookedUpByIDAndReportsWhenItIsGone(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.stop("s1", "8544", "ALTA VISTA / CALEDON")

	got, ok, err := w.Stop(context.Background(), "s1")
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if !ok || got.Code != "8544" || got.Name != "ALTA VISTA / CALEDON" {
		t.Errorf("Stop = %+v, ok = %v", got, ok)
	}

	if _, ok, err := w.Stop(context.Background(), "gone"); err != nil || ok {
		t.Errorf("a stop that is not there returned ok = %v, err = %v", ok, err)
	}
}

// A name that begins with what was typed comes before one that merely contains
// it. BILLINGS BRIDGE / BANK is what a person typing "billings" meant, and
// ALTA VISTA / BILLINGS is not.
func TestASearchPutsNamesThatBeginWithTheQueryFirst(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "all", "Hurdman")
	for i, name := range []string{"ALTA VISTA / BILLINGS", "BILLINGS BRIDGE / BANK", "BILLINGS BRIDGE 3B"} {
		id := "s" + strconv.Itoa(i)
		w.stop(id, "300"+strconv.Itoa(i), name)
		w.calls("t1", id, 30000+i)
	}

	got, _, err := w.SearchStops(context.Background(), "billings", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	want := []string{"BILLINGS BRIDGE / BANK", "BILLINGS BRIDGE 3B", "ALTA VISTA / BILLINGS"}
	if !slices.Equal(names, want) {
		t.Errorf("SearchStops = %v, want %v", names, want)
	}
}
func TestTwoStopsWithOneNameKeepTheOrderTheExportListsThemIn(t *testing.T) {
	t.Parallel()
	// TERMINAL / SANDFORD FLEMING is three stops with one name, and a search
	// offers all three. Nothing in the data orders them, so the export's own
	// order does, for the same reason a tie between two departures does.
	for _, tc := range []struct {
		name  string
		order []string
	}{
		{"1801 written first", []string{"1801", "1802"}},
		{"1802 written first", []string{"1802", "1801"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			w.service("all", everyDay, "20260830", "20260920")
			w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
			w.trip("t1", "44", "all", "Hurdman")
			for i, code := range tc.order {
				// The ids run the other way from the codes, so an order by id
				// cannot pass this by accident.
				id := "s" + strconv.Itoa(len(tc.order)-i)
				w.stop(id, code, "TERMINAL / SANDFORD FLEMING")
				w.calls("t1", id, 30000+i)
			}

			got, _, err := w.SearchStops(context.Background(), "sandford", saturday)
			if err != nil {
				t.Fatalf("SearchStops: %v", err)
			}
			codes := make([]string, len(got))
			for i, m := range got {
				codes[i] = m.Code
			}
			if !slices.Equal(codes, tc.order) {
				t.Errorf("the stops came back as %v, want %v", codes, tc.order)
			}
		})
	}
}
func TestASearchDoesNotOfferAStationBecauseNoBusStopsAtOne(t *testing.T) {
	t.Parallel()
	// A station is the place the platforms belong to. It carries the name a
	// person types, and nothing calls there: every departure is at a platform
	// under it. Offering it is offering a row that opens an empty board.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("1", "1", "Blair <> Tunney's Pasture", 0, 1)
	w.trip("t1", "1", "all", "Blair")
	w.exec(`INSERT INTO stops VALUES ('stn','3060','BAYVIEW STATION',0,0,1,'',NULL)`)
	w.exec(`INSERT INTO stops VALUES ('p1','3061','BAYVIEW 1A',0,0,0,'stn','1A')`)
	w.calls("t1", "p1", 32400)
	// The station has a call of its own in the table, which the real export does
	// not write. Nothing about it being a station may depend on that.
	w.calls("t1", "stn", 32400)

	got, _, err := w.SearchStops(context.Background(), "bayview", saturday)
	if err != nil {
		t.Fatalf("SearchStops: %v", err)
	}
	if len(got) != 1 || got[0].ID != "p1" {
		t.Errorf("SearchStops = %v, want only the platform", got)
	}
}
