package app

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/render"
)

// What a row looks like: which column each field sits in, which field gives way
// when the terminal runs out, what is cut with an ellipsis and what the screen's
// edge simply clips, and the colour a wait wears.
//
// The columns and the text in them are one question, so format.go and layout.go
// are tested together here. What a screen holds and which key opens it is a
// different question, and that is app_test.go.

// The clock column truncates. 12:37:53 is written 12:37, because a bus does
// not leave at 12:38 and rounding the display would say it does.
func TestTheClockColumnTruncatesSecondsAndWrapsPastMidnight(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		sec  int
		want string
	}{
		{"on the minute", 32400, "09:00"},
		{"seconds are dropped, not rounded", 45473, "12:37"},
		{"one second before the minute", 31379, "08:42"},
		{"past 24:00 wraps onto the clock", 86940, "00:09"},
		{"exactly 24:00", 86400, "00:00"},
		{"borrowed from yesterday", -1800, "23:30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := clockAt(tc.sec); got != tc.want {
				t.Errorf("clockAt(%d) = %q, want %q", tc.sec, got, tc.want)
			}
		})
	}
}

// The wait rounds. 894.68 minutes is fifteen minutes short of fifteen hours,
// and truncating it would say 14h 54 where the reference says 14h 55.
func TestTheWaitRoundsToTheNearestMinuteAndTheClockDoesNot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		secs      int
		want      int
		truncates int
	}{
		{"just past the minute", 2521, 42, 42},
		{"most of a minute", 16673, 278, 277},
		{"two thirds of a minute", 53681, 895, 894},
		{"exactly on the minute", 3600, 60, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := waitMinutes(tc.secs); got != tc.want {
				t.Errorf("waitMinutes(%d) = %d, want %d. Truncating gives %d", tc.secs, got, tc.want, tc.truncates)
			}
		})
	}
}
func TestAWaitOfAnHourOrMoreIsWrittenWithATwoDigitMinute(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mins int
		want string
	}{
		{42, "42 min"},
		{59, "59 min"},
		{60, "1h 00 min"},
		{278, "4h 38 min"},
		{390, "6h 30 min"},
		{750, "12h 30 min"},
		{969, "16h 09 min"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := waitText(tc.mins); got != tc.want {
				t.Errorf("waitText(%d) = %q, want %q", tc.mins, got, tc.want)
			}
		})
	}
}

// The artifact names a stop without its platform, and the platform is only
// ever removed using the platform column. Taking the last word off the name
// instead would turn CANTERBURY / AD. 860 into CANTERBURY /, and would be
// wrong on the 5,644 stops of 5,859 in the real feed that carry no platform.
func TestAPlatformComesOffTheNameOnlyWhenTheStopHasOne(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, stop, platform, want string
	}{
		{"a stop with a platform", "BILLINGS BRIDGE 3B", "3B", "BILLINGS BRIDGE"},
		{"an address is not a platform", "CANTERBURY / AD. 860", "", "CANTERBURY / AD. 860"},
		{"another address", "VANTAGE / AD. 303", "", "VANTAGE / AD. 303"},
		{"a cross street is not a platform", "BILLINGS BRIDGE / BANK", "", "BILLINGS BRIDGE / BANK"},
		{"a platform that is not a suffix stays put", "SOMEWHERE ELSE", "A", "SOMEWHERE ELSE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := withoutPlatform(tc.stop, tc.platform); got != tc.want {
				t.Errorf("withoutPlatform(%q, %q) = %q, want %q", tc.stop, tc.platform, got, tc.want)
			}
		})
	}
}

// A bus leaving this minute is not a quantity of time. The column reads due,
// and the artifact still reports a wait of zero.
func TestAWaitOfNoneReadsDue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mins int
		want string
	}{
		{0, "due"},
		{1, "1 min"},
		{2, "2 min"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := waitText(tc.mins); got != tc.want {
				t.Errorf("waitText(%d) = %q, want %q", tc.mins, got, tc.want)
			}
		})
	}
}

// A rail platform's name carries the line after the station, and that is noise
// in a list of stops. HURDMAN O-TRAIN EAST / EST is Hurdman.
func TestARailPlatformIsNamedAfterItsStation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, want string
	}{
		{"HURDMAN O-TRAIN EAST / EST", "HURDMAN"},
		{"PARLIAMENT / PARLEMENT O-TRAIN WEST / OUEST", "PARLIAMENT / PARLEMENT"},
		// What goes is the direction written after the words, and not the words.
		// This platform carries no direction and keeps every one of them. The
		// line I had here before said it came out as TUNNEY'S PASTURE, which is
		// what the rule reads like, and the drilldown artifact says otherwise.
		{"TUNNEY'S PASTURE O-TRAIN", "TUNNEY'S PASTURE O-TRAIN"},
		{"BILLINGS BRIDGE 3B", "BILLINGS BRIDGE 3B"},
		{"CANTERBURY / AD. 860", "CANTERBURY / AD. 860"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			if got := station(tc.name); got != tc.want {
				t.Errorf("station(%q) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// A search names the station and then which platform. That is not the name in
// the feed: a name already ending in its platform keeps one copy, and a name
// that does not gains it.
func TestASearchNamesTheStationThenThePlatform(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, platform, shown, reported string
	}{
		{"BILLINGS BRIDGE 3B", "3B", "BILLINGS BRIDGE 3B", "BILLINGS BRIDGE"},
		{"HERON 1A (A)", "1A", "HERON 1A (A) 1A", "HERON 1A (A)"},
		{"HURDMAN O-TRAIN EAST / EST", "2", "HURDMAN 2", "HURDMAN"},
		{"HURDMAN B", "B", "HURDMAN B", "HURDMAN"},
		{"CANTERBURY / AD. 860", "", "CANTERBURY / AD. 860", "CANTERBURY / AD. 860"},
	} {
		t.Run(tc.shown, func(t *testing.T) {
			t.Parallel()
			if got := withPlatform(tc.name, tc.platform); got != tc.shown {
				t.Errorf("withPlatform(%q, %q) = %q, want %q", tc.name, tc.platform, got, tc.shown)
			}
			if got := withoutPlatform(tc.name, tc.platform); got != tc.reported {
				t.Errorf("withoutPlatform(%q, %q) = %q, want %q", tc.name, tc.platform, got, tc.reported)
			}
		})
	}
}

// A route badge carries one cell of air on each side and is not padded to a
// width. Route 1 is written " 1 " and route 44 " 44 ".
func TestARouteBadgeIsNotPaddedToAWidth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ route, want string }{
		{"1", " 1 "},
		{"44", " 44 "},
		{"101", " 101 "},
	} {
		t.Run(tc.route, func(t *testing.T) {
			t.Parallel()
			if got := badge(tc.route); got != tc.want {
				t.Errorf("badge(%q) = %q, want %q", tc.route, got, tc.want)
			}
		})
	}
}

// The search screen draws the station and the platform, which is not the name
// in the feed. Testing the rule without testing that the screen uses it left
// the screen free to draw the raw name.
func TestTheSearchScreenDrawsTheStationAndThePlatform(t *testing.T) {
	t.Parallel()
	a := newApp(t, stub{stops: []cache.Match{{
		Stop:   cache.Stop{ID: "s1", Code: "3023", Name: "HURDMAN O-TRAIN EAST / EST", Platform: "2"},
		Routes: []string{"1"},
		Toward: "Blair",
	}}})
	press(t, a, Key{Kind: Char, Rune: 'h'})

	// Read the drawn row rather than a cell index: the marker is a cell too.
	drawn := render.Draw(a.View(100, 14)).String()
	if !strings.Contains(drawn, "HURDMAN 2") {
		t.Errorf("the search row does not read %q", "HURDMAN 2")
	}
	if strings.Contains(drawn, "HURDMAN O-TRAIN") {
		t.Error("the search row draws the raw feed name")
	}
	reported := a.Semantic()["rows"].([]any)[0].(map[string]any)["primary"]
	if reported != "HURDMAN" {
		t.Errorf("the artifact reports %q, want %q", reported, "HURDMAN")
	}
}

// How soon a bus is due is a colour. The thresholds read the rounded minute and
// not the seconds: a bus 121 seconds away rounds to two minutes and is drawn as
// two minutes, which the reference confirms.
func TestHowSoonABusIsDueIsAColour(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		mins int
		want render.Style
	}{
		{0, dueNow}, {1, dueNow}, {2, dueNow},
		{3, dueSoon}, {6, dueSoon},
		{7, comingUp}, {15, comingUp},
		{16, muted}, {60, muted},
	} {
		t.Run(waitText(tc.mins), func(t *testing.T) {
			t.Parallel()
			if got := waitPaint(tc.mins); got != tc.want {
				t.Errorf("waitPaint(%d) = %v, want %v", tc.mins, got, tc.want)
			}
		})
	}
}

// A board colours its wait column by how soon the bus is. Testing the rule
// without testing that the board uses it left the board free to draw it muted.
func TestABoardColoursItsWaitByHowSoonTheBusIs(t *testing.T) {
	t.Parallel()
	a := newApp(t, stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "A STOP"}}},
		board: map[string][]cache.Departure{"s1": {
			{Route: "44", Headsign: "Hurdman", Scheduled: 8*3600 + 60},   // a minute off
			{Route: "44", Headsign: "Hurdman", Scheduled: 8*3600 + 3600}, // an hour
		}},
	})
	press(t, a, Key{Kind: Char, Rune: 'a'}, Key{Kind: Enter})

	rows := a.View(100, 14).Rows
	if len(rows) != 2 {
		t.Fatalf("the board drew %d rows, want 2", len(rows))
	}
	if got := waitCellOf(t, rows[0]); got != dueNow {
		t.Errorf("a bus a minute away is drawn %v, want %v", got, dueNow)
	}
	if got := waitCellOf(t, rows[1]); got != muted {
		t.Errorf("a bus an hour away is drawn %v, want %v", got, muted)
	}
}

// A trip borrowed from the service day that ended says so on its row, or a
// departure at 24:39 reads as a quarter past one in the afternoon.
func TestABorrowedTripSaysItIsAfterMidnight(t *testing.T) {
	t.Parallel()
	a := newAppOn(t, stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "1869", Name: "A STOP"}}},
		board: map[string][]cache.Departure{"s1": {
			{Route: "44", Headsign: "Hurdman", Scheduled: 245, AfterMidnight: true},
			{Route: "44", Headsign: "Hurdman", Scheduled: 8 * 3600, AfterMidnight: false},
		}},
	}, theDay, "00:00", NoKey())
	press(t, a, Key{Kind: Char, Rune: 'a'}, Key{Kind: Enter})

	drawn := strings.Split(render.Draw(a.View(100, 14)).String(), "\n")
	var withNote, without int
	for _, line := range drawn {
		// The status bar says "scheduled only", so match a departure row by its
		// own column rather than by the word.
		if !strings.Contains(line, "44") || strings.Contains(line, "Departures") {
			continue
		}
		if strings.Contains(line, "after midnight") {
			withNote++
		} else {
			without++
		}
	}
	if withNote != 1 || without != 1 {
		t.Errorf("%d rows say after midnight and %d do not, want one each", withNote, without)
	}
}

// A badge in a row is five cells, centred, with the odd space on the left. The
// slice holds only one and two character routes, which both land in the same
// place, so a three character one is the case that tells the two rules apart.
func TestARowBadgeIsFiveCellsCentredWithTheOddSpaceLeft(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		route       string
		wantTextCol int
		wantFirst   int
		wantLast    int
	}{
		{"1", 5, 3, 8},
		{"44", 5, 3, 8},
		{"101", 4, 3, 8},
		{"1234", 4, 3, 8},
		{"12345", 3, 3, 8},
	} {
		t.Run(tc.route, func(t *testing.T) {
			t.Parallel()
			cell := badgeCell(tc.route, "0057B8", column{at: 5, room: badgeWidth})
			if cell.At != tc.wantTextCol {
				t.Errorf("route %q sits at %d, want %d", tc.route, cell.At, tc.wantTextCol)
			}
			if want := (render.Span{From: tc.wantFirst, To: tc.wantLast}); cell.Paints != want {
				t.Errorf("route %q paints %+v, want %+v", tc.route, cell.Paints, want)
			}
		})
	}
}

// A wait below zero is a bus that has gone, and it is struck through. No
// fixture reaches one without a realtime prediction, so this comes from
// conformance/README.md rather than from a diff.
func TestAWaitBelowZeroIsStruckThrough(t *testing.T) {
	t.Parallel()
	got := waitPaint(-1)
	if !got.Strike {
		t.Errorf("waitPaint(-1) = %v, want it struck through", got)
	}
	if got.Fg != muted.Fg {
		t.Errorf("waitPaint(-1) = %v, want the dim colour %q", got, muted.Fg)
	}
}

// Four fields compete for a search row and three of them give way. Every number
// here was measured off the reference at that width, and the rule is integer
// arithmetic against a floor and a ceiling.
//
// Look at 49 and 50: the name is thirteen cells in the narrower terminal and
// twelve in the wider one. Two divisions and a clamp do not move in step with the
// terminal, and a port that smoothed that out would be wrong at nine widths in
// twenty.
func TestASearchRowsColumnsGiveWayByArithmetic(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width                    int
		name, toward, routes     int
		nameAt, codeAt, towardAt int
		routesAt                 int
	}{
		{119, 36, 22, 26, 3, 41, 49, 73},
		{105, 36, 22, 23, 3, 41, 49, 73},
		{100, 36, 22, 22, 3, 41, 49, 73},
		{93, 36, 22, 20, 3, 41, 49, 73},
		{92, 35, 22, 20, 3, 40, 48, 72},
		{70, 24, 16, 15, 3, 29, 37, 55},
		{60, 18, 14, 13, 3, 23, 31, 47},
		{51, 13, 12, 11, 3, 18, 26, 40},
		{50, 12, 12, 11, 3, 17, 25, 39},
		{49, 13, 11, 10, 3, 18, 26, 39},
		{48, 12, 11, 10, 3, 17, 25, 38},
		{44, 12, 10, 9, 3, 17, 25, 37},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			c := searchLayout(tc.width)
			got := []column{c.name, c.code, c.toward, c.routes}
			want := []column{
				{tc.nameAt, tc.name}, {tc.codeAt, 6},
				{tc.towardAt, tc.toward}, {tc.routesAt, tc.routes},
			}
			if !slices.Equal(got, want) {
				t.Errorf("the columns are %+v, want %+v", got, want)
			}
		})
	}
}

// A board's headsign is the one field there that gives way, and everything after
// it moves along. Above seventy-one cells it has stopped giving.
func TestABoardsHeadsignGivesWayAndTheRestFollowsIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width             int
		room              int
		clock, wait, note int
	}{
		{100, 24, 37, 55, 58},
		{72, 24, 37, 55, 58},
		{71, 24, 37, 55, 58},
		{70, 23, 36, 54, 57},
		{60, 13, 26, 44, 47},
		{55, 8, 21, 39, 42},
		{44, 8, 21, 39, 42},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			if got := headsignRoom(tc.width); got != tc.room {
				t.Errorf("the headsign has %d cells, want %d", got, tc.room)
			}
			c := boardLayout(tc.width, false)
			got := []column{c.badge, c.headsign, c.clock, c.wait, c.note}
			want := []column{
				{5, 5}, {11, tc.room}, {tc.clock, 5}, {tc.wait, 10}, {tc.note, 10},
			}
			if !slices.Equal(got, want) {
				t.Errorf("the columns are %+v, want %+v", got, want)
			}

			// Drilled to, the headsign is left out and the rest stops moving.
			drilled := boardLayout(tc.width, true)
			if drilled.headsign.room != 0 {
				t.Errorf("a drilled board gave its headsign %d cells, want none", drilled.headsign.room)
			}
			if fixed := (boardColumns{
				badge: column{5, 5}, clock: column{11, 5},
				wait: column{29, 10}, note: column{32, 10},
			}); drilled != fixed {
				t.Errorf("a drilled board's columns are %+v, want %+v", drilled, fixed)
			}
		})
	}
}

// A name that does not fit its column ends in a single ellipsis, and the platform
// keeps its cells: which platform is what a person came to the search for, and
// the station is the part they can guess.
func TestASearchRowCutsTheStationAndKeepsThePlatform(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width int
		want  string
	}{
		{100, "BILLINGS BRIDGE 4D"},
		{60, "BILLINGS BRIDGE 4D"},
		{55, "BILLINGS BR… 4D"},
		{50, "BILLINGS… 4D"},
		{49, "BILLINGS … 4D"},
		{44, "BILLINGS… 4D"},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			row := searchRow(cache.Match{
				Stop:   cache.Stop{Code: "3034", Name: "BILLINGS BRIDGE 4D", Platform: "4D"},
				Routes: []string{"44"},
				Toward: "Billings Bridge",
			}, false, searchLayout(tc.width))

			name := searchLayout(tc.width).name
			var got string
			for _, c := range row.Cells {
				if c.At >= name.at && c.At < name.at+name.room {
					got += c.Text
				}
			}
			if got != tc.want {
				t.Errorf("the name cell is %q, want %q", got, tc.want)
			}
		})
	}
}

// The last column of a search row has a column of its own, so a screen ending
// inside it clips the text rather than marking it cut. An ellipsis there would
// claim the routes were shortened, when what ran out was the screen.
func TestTheRouteListClipsRatherThanMarkingACut(t *testing.T) {
	t.Parallel()
	// At 38 cells the routes start at 37, so one cell of the screen is left.
	const width = 38
	row := searchRow(cache.Match{
		Stop:   cache.Stop{Code: "8403", Name: "ALBION / WALKLEY"},
		Routes: []string{"44"},
		Toward: "Billings Bridge",
	}, false, searchLayout(width))

	drawn := render.Draw(render.View{Width: width, Height: 3, Rows: []render.Row{row}})
	last := strings.Split(strings.TrimSuffix(drawn.String(), "\n"), "\n")[1]
	if strings.HasSuffix(last, "…") {
		t.Errorf("the row is %q, want the routes clipped by the edge and not marked", last)
	}
	if !strings.HasSuffix(last, "4") {
		t.Errorf("the row is %q, want it to end in the first cell of the route list", last)
	}
}

// The route list paints what it holds and not the column it sits in. A column
// that painted its whole room would stripe the dim colour across the empty cells
// after a one-route stop, where every other list leaves them bare.
func TestTheRouteListPaintsOnlyTheRoutes(t *testing.T) {
	t.Parallel()
	cols := searchLayout(100)
	row := searchRow(cache.Match{
		Stop:   cache.Stop{Code: "3034", Name: "A STOP"},
		Routes: []string{"44"},
		Toward: "Hurdman",
	}, false, cols)

	for _, c := range row.Cells {
		if c.At != cols.routes.at {
			continue
		}
		if want := (render.Span{}); c.Paints != want {
			t.Errorf("the route list paints %+v, want the empty span that means its own text", c.Paints)
		}
		return
	}
	t.Errorf("no cell in the route list's column: %+v", row.Cells)
}

// A pin row's name and destination divide what the fixed fields leave, and the
// name wins the wider half. Measured off the reference at each width.
func TestAPinRowsNameWinsTheWiderHalf(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width           int
		name, headsign  int
		nameAt, poleAt  int
		badgeAt, headAt int
		waitAt, noteAt  int
	}{
		{100, 28, 20, 3, 32, 40, 44, 75, 77},
		{80, 23, 18, 3, 27, 35, 39, 68, 70},
		{60, 12, 9, 3, 16, 24, 28, 48, 50},
		// Both land on their floors here, and stay there however narrow it gets.
		{50, 10, 6, 3, 14, 22, 26, 43, 45},
		{40, 10, 6, 3, 14, 22, 26, 43, 45},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			c := pinLayout(tc.width)
			got := []column{c.name, c.pole, c.badge, c.headsign, c.wait, c.note}
			want := []column{
				{tc.nameAt, tc.name}, {tc.poleAt, 6}, {tc.badgeAt, 5},
				{tc.headAt, tc.headsign}, {tc.waitAt, 10}, {tc.noteAt, 9},
			}
			if !slices.Equal(got, want) {
				t.Errorf("the columns are %+v, want %+v", got, want)
			}
		})
	}
}

// A column with another column after it is clipped by the screen and says
// nothing about it. A column that runs to the end of the row is cut, and the
// ellipsis is how it says so.
//
// Both rows below are drawn at nineteen cells. The stop name has the pole number
// after it, so the screen simply ends inside it; the route's long name is the
// last thing on its row, so it gives way and marks it. Measured against the
// reference, which no fixture reaches: nothing in the suite is this narrow on
// either screen.
func TestAnInteriorColumnIsClippedAndALastOneIsCut(t *testing.T) {
	t.Parallel()
	const width = 19

	tree := treeRows([]Row{{Primary: "TRANSITWAY / TERMINAL", Secondary: "#1869"}}, 0, "0057b8")
	route := routeRows([]cache.Route{{ShortName: "44", LongName: "Billings Bridge <> Hurdman"}}, 0)

	for _, tc := range []struct {
		name string
		row  render.Row
		want string
	}{
		{"a stop name, with the pole number after it", tree[0], " ❯ │ TRANSITWAY / T"},
		{"a route's long name, which ends the row", route[0], " ❯   44    Billing…"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			drawn := render.Draw(render.View{
				Width: width, Height: 3, Cursor: true, Rows: []render.Row{tc.row},
			})
			got := strings.Split(strings.TrimSuffix(drawn.String(), "\n"), "\n")[1]
			if got != tc.want {
				t.Errorf("the row is %q, want %q", got, tc.want)
			}
		})
	}
}
