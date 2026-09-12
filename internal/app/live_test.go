package app

// The boards these tests read are a hundred cells wide, where the columns after
// the headsign have stopped moving. Named here rather than indexed out of a
// layout, so a test says which field it means.
//
// The tests here are about what the program does with the feeds it is handed:
// the realtime trip updates, and the updates feed the detours come from. The
// screens and the keys are in app_test.go.

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/clock"
	"github.com/sina-negarandeh/otransit-go/internal/detour"
	"github.com/sina-negarandeh/otransit-go/internal/realtime"
	"github.com/sina-negarandeh/otransit-go/internal/render"
)

// ---- realtime -------------------------------------------------------------

// Where a hundred-cell board puts its clock, its countdown and its note. Named
// here rather than indexed out of a layout, so a test says which field it means.
var boardClock, boardWait, boardNote = func() (int, int, int) {
	c := boardLayout(100, false)
	return c.clock.at, c.wait.at, c.note.at
}()

// liveApp is the program with one fetch of the feed held, on a board at the
// stop the feed is about. The feed is written out here rather than read from a
// fixture, because a test that read the fixture would measure the fixture.
func liveApp(t *testing.T, at string, feed string) *App {
	t.Helper()
	k, err := clock.At("2026-09-12", at, time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("clock.At: %v", err)
	}
	live, err := realtime.Parse(strings.NewReader(feed))
	if err != nil {
		t.Fatalf("realtime.Parse: %v", err)
	}
	a := New(liveStub(), k, Feed{Live: live})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Down to the board: a letter opens the search, and the first result is the
	// stop every prediction below names.
	press(t, a, Key{Kind: Char, Rune: 'f'}, Key{Kind: Enter})
	return a
}

// liveStub holds one stop with three departures, each on its own trip, so a
// prediction can name one of them.
func liveStub() stub {
	return stub{
		stops: []cache.Match{
			{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "FIRST STOP"}, Routes: []string{"44"}, Toward: "Hurdman"},
		},
		board: map[string][]cache.Departure{
			"s1": {
				{Trip: "t1", Route: "44", Headsign: "Hurdman", Scheduled: 30000},
				{Trip: "t2", Route: "44", Headsign: "Hurdman", Scheduled: 33000},
				{Trip: "t3", Route: "48", Headsign: "Carleton", Scheduled: 36000},
			},
		},
	}
}

// The service day began at midnight, so a schedule of 30,000 seconds is
// 08:20:00 and the feed's stamp for the same moment is this.
const dayBegan = 1789185600 // 2026-09-12 00:00 at -04:00

func stamp(second int) int64 { return dayBegan + int64(second) }

// A prediction replaces the time the row shows, and the row says how far off the
// schedule it is. The clock brightens, because a predicted time is the best
// answer this program has and a scheduled one is a guess from a table.
func TestAPredictionReplacesTheTimeAndSaysHowFarOff(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		at        int
		clock     string
		wait      string
		note      string
		notePaint render.Style
	}{
		{"four minutes late", 30240, "08:24", "24 min", "4 late", unbold(dueSoon)},
		{"on time to the second", 30000, "08:20", "20 min", "on time", comingUp},
		{"a minute late is on time", 30060, "08:21", "21 min", "on time", comingUp},
		{"a minute early is on time", 29940, "08:19", "19 min", "on time", comingUp},
		{"ninety seconds late is two", 30090, "08:21", "22 min", "2 late", unbold(dueSoon)},
		{"ninety seconds early is two", 29910, "08:18", "19 min", "2 early", muted},
		{"ten minutes early", 29400, "08:10", "10 min", "10 early", muted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := liveApp(t, "08:00", `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28800), 10)+
				`},"Entity":[{"TripUpdate":{"Trip":{"TripId":"t1"},"StopTimeUpdate":[
				{"StopId":"s1","Arrival":{"HasTime":true,"Time":`+strconv.FormatInt(stamp(tc.at), 10)+`}}]}}]}`)

			row := a.View(100, 20).Rows[0]
			if got := cellAt(t, row, boardClock); got.Text != tc.clock {
				t.Errorf("the clock says %q, want %q", got.Text, tc.clock)
			} else if got.Style != plainText {
				t.Errorf("the clock is %v, want the bright colour a prediction takes", got.Style)
			}
			if got := countdownIn(t, row); got.Text != tc.wait {
				t.Errorf("the countdown says %q, want %q", got.Text, tc.wait)
			}
			got := cellAt(t, row, boardNote)
			if got.Text != tc.note {
				t.Errorf("the note says %q, want %q", got.Text, tc.note)
			}
			if got.Style != tc.notePaint {
				t.Errorf("the note is %v, want %v", got.Style, tc.notePaint)
			}
		})
	}
}

// A row with no prediction says so and stays dim. Every row is this row until
// the feed mentions its trip.
func TestARowTheFeedDidNotMentionReadsSched(t *testing.T) {
	t.Parallel()
	a := liveApp(t, "08:00", `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28800), 10)+
		`},"Entity":[{"TripUpdate":{"Trip":{"TripId":"t1"},"StopTimeUpdate":[
		{"StopId":"s1","Arrival":{"HasTime":true,"Time":`+strconv.FormatInt(stamp(30240), 10)+`}}]}}]}`)

	// The second row is trip t2, which the feed said nothing about.
	row := a.View(100, 20).Rows[1]
	if got := cellAt(t, row, boardNote); got.Text != "sched" || got.Style != faint {
		t.Errorf("the note is %q in %v, want %q in the faint colour", got.Text, got.Style, "sched")
	}
	if got := cellAt(t, row, boardClock); got.Style != muted {
		t.Errorf("the clock is %v, want the dim colour a schedule takes", got.Style)
	}
}

// A prediction for a trip at another stop is not a prediction here. One trip
// calls at many stops, and the pair is what a prediction is about.
func TestAPredictionAtAnotherStopIsNotAppliedHere(t *testing.T) {
	t.Parallel()
	a := liveApp(t, "08:00", `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28800), 10)+
		`},"Entity":[{"TripUpdate":{"Trip":{"TripId":"t1"},"StopTimeUpdate":[
		{"StopId":"somewhere else","Arrival":{"HasTime":true,"Time":`+strconv.FormatInt(stamp(30240), 10)+`}}]}}]}`)

	row := a.View(100, 20).Rows[0]
	if got := cellAt(t, row, boardClock); got.Text != "08:20" {
		t.Errorf("the clock says %q, want the scheduled %q", got.Text, "08:20")
	}
	if got := cellAt(t, row, boardNote); got.Text != "sched" {
		t.Errorf("the note says %q, want %q", got.Text, "sched")
	}
}

// A cancelled trip keeps its row and its scheduled time, and gives a dash where
// the countdown goes. There is no countdown to a bus that is not coming, so the
// dash does not take the colour a wait of that length would.
func TestACancelledTripDrawsADashWhateverTheWaitWouldHaveBeen(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		at   string
	}{
		{"a long way off", "08:00"},
		// Two minutes out, where a wait would be red.
		{"almost due", "08:18"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := liveApp(t, tc.at, `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28800), 10)+
				`},"Entity":[{"TripUpdate":{"Trip":{"TripId":"t1","ScheduleRelationship":3},
				"StopTimeUpdate":[]}}]}`)

			row := a.View(100, 20).Rows[0]
			if got := countdownIn(t, row); got.Text != "—" || got.Style != muted {
				t.Errorf("the countdown is %q in %v, want a dash in the dim colour", got.Text, got.Style)
			}
			if got := cellAt(t, row, boardNote); got.Text != "cancelled" || got.Style != dueNow {
				t.Errorf("the note is %q in %v, want %q in the red one", got.Text, got.Style, "cancelled")
			}
			// The trip was going to run, so the time it was going to run at is
			// still there, with a line through it.
			if got := cellAt(t, row, boardClock); got.Text != "08:20" || got.Style != strikeOut(muted) {
				t.Errorf("the clock is %q in %v, want %q struck through", got.Text, got.Style, "08:20")
			}
		})
	}
}

// A prediction moves a row to where it is now expected, so the board stays in
// the order a person will see the buses in.
func TestAPredictionMovesARowToWhereItIsExpected(t *testing.T) {
	t.Parallel()
	// Trip t3, scheduled last at 10:00, is now expected before the other two.
	a := liveApp(t, "08:00", `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28800), 10)+
		`},"Entity":[{"TripUpdate":{"Trip":{"TripId":"t3"},"StopTimeUpdate":[
		{"StopId":"s1","Arrival":{"HasTime":true,"Time":`+strconv.FormatInt(stamp(29000), 10)+`}}]}}]}`)

	var times []string
	for _, row := range a.View(100, 20).Rows {
		times = append(times, cellAt(t, row, boardClock).Text)
	}
	if want := []string{"08:03", "08:20", "09:10"}; !slices.Equal(times, want) {
		t.Errorf("the board reads %v, want %v", times, want)
	}
}

// A departure the feed has already put in the past keeps its row, because its
// schedule has not gone yet. It reads due, which is what a bus arriving now
// reads, and only the line through it says the difference.
func TestADepartureThePredictionHasAlreadyPassedReadsDueAndIsStruckThrough(t *testing.T) {
	t.Parallel()
	a := liveApp(t, "08:00", `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28800), 10)+
		`},"Entity":[{"TripUpdate":{"Trip":{"TripId":"t1"},"StopTimeUpdate":[
		{"StopId":"s1","Arrival":{"HasTime":true,"Time":`+strconv.FormatInt(stamp(27000), 10)+`}}]}}]}`)

	row := a.View(100, 20).Rows[0]
	got := countdownIn(t, row)
	if got.Text != "due" {
		t.Errorf("the countdown says %q, want %q", got.Text, "due")
	}
	if got.Style != strikeOut(muted) {
		t.Errorf("the countdown is %v, want the dim struck-through one", got.Style)
	}
	if note := cellAt(t, row, boardNote); note.Text != "50 early" {
		t.Errorf("the note says %q, want %q", note.Text, "50 early")
	}
}

// The status bar says how old the feed is, and the artifact reports the same
// words. With no feed at all it says there is no key.
func TestTheFeedSaysHowOldItIs(t *testing.T) {
	t.Parallel()
	a := liveApp(t, "08:00", `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28740), 10)+`},"Entity":[]}`)

	if got, want := a.State().Feed, "live 60s"; got != want {
		t.Errorf("the feed says %q, want %q", got, want)
	}
	feed, ok := a.Semantic()["feed"].(map[string]any)
	if !ok {
		t.Fatalf("the artifact reports the feed as %T, want an object", a.Semantic()["feed"])
	}
	if got := feed["note"]; got != "live 60s" {
		t.Errorf("the artifact reports %q, want the same words the bar shows", got)
	}
	if hints := a.View(100, 20).Status.Hints; !strings.HasPrefix(hints, "live 60s · ") {
		t.Errorf("the hints are %q, want them to start with the feed's note", hints)
	}
}

// Two departures can expect the same second, and which of them is drawn first
// has to be the order the query gave. The query orders a day totally for this
// reason, and the sort that applies predictions has to keep that order.
//
// What this catches is a sort that reorders ties on purpose, by adding a
// tiebreak of its own. What it cannot catch is sort.SliceStable becoming
// sort.Slice: the two differ only in behaviour Go leaves unspecified, and every
// input tried here comes out the same under both. That one is held by the call
// itself and by the comment beside it.
func TestDeparturesExpectedInTheSameSecondKeepTheOrderTheQueryGave(t *testing.T) {
	t.Parallel()
	var calls []cache.Departure
	var want []string
	for i := range 20 {
		headsign := "Stop " + strconv.Itoa(i)
		calls = append(calls, cache.Departure{
			Trip: "t" + strconv.Itoa(i), Route: "44", Headsign: headsign, Scheduled: 30000,
		})
		want = append(want, headsign)
	}

	k, err := clock.At("2026-09-12", "08:00", time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("clock.At: %v", err)
	}
	a := New(stub{board: map[string][]cache.Departure{"s1": calls}}, k, NoKey())

	got, err := a.board(context.Background(), "s1", "", "", boardFetch)
	if err != nil {
		t.Fatalf("board: %v", err)
	}
	var order []string
	for _, d := range got {
		order = append(order, d.Headsign)
	}
	if !slices.Equal(order, want) {
		t.Errorf("the board reads\n%v\nwant the order the query gave\n%v", order, want)
	}
}

// ---- detours --------------------------------------------------------------

// detouring is a world with two bus routes and one rail line, where every route
// has a direction and a stop, so a walk can reach every screen that might carry
// a detour.
func detouring() stub {
	return stub{
		bus:  []cache.Route{{ShortName: "44", Colour: "0057b8"}},
		rail: []cache.Route{{ShortName: "1", Colour: "d30f1d"}},
		dirs: []cache.Direction{{Headsign: "Hurdman", Trips: 8}},
		routeStops: []cache.Stop{
			{ID: "s1", Code: "3034", Name: "BILLINGS BRIDGE 3B"},
		},
		// A pin resolves its stop through the same lookup a search uses.
		stops: []cache.Match{
			{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "BILLINGS BRIDGE 3B"}},
		},
		board: map[string][]cache.Departure{
			"s1": {{Trip: "t1", Route: "44", Headsign: "Hurdman", Scheduled: 30000,
				Colour: "0057b8"}},
		},
	}
}

// notifying is the program holding one detour, walked down to a screen.
func notifying(t *testing.T, routes string, keys ...Key) *App {
	t.Helper()
	feed, err := detour.Parse(strings.NewReader(
		`<rss><channel><item><title><![CDATA[Bridge closure]]></title>` +
			`<category><![CDATA[Detours]]></category>` +
			`<category><![CDATA[affectedRoutes-` + routes + `]]></category></item></channel></rss>`))
	if err != nil {
		t.Fatalf("detour.Parse: %v", err)
	}
	k, err := clock.At("2026-09-12", "08:00", time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("clock.At: %v", err)
	}
	a := New(detouring(), k, NoKey())
	a.Updates(Notices{Live: feed})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	press(t, a, keys...)
	return a
}

// A detour is shown on the screens that are about a route, which are the list of
// its directions and the list of its stops. The board is about a stop and shows
// none, even the board drilled to through the route the detour names.
func TestADetourIsShownOnTheScreensAboutARoute(t *testing.T) {
	t.Parallel()
	const notice = "Bridge closure"
	down := []Key{{Kind: Enter}, {Kind: Enter}, {Kind: Enter}, {Kind: Enter}}

	for _, tc := range []struct {
		name   string
		keys   []Key
		screen string
		want   string
	}{
		{"the first screen", nil, "mode", ""},
		{"the routes", down[:1], "routes", ""},
		{"the directions", down[:2], "directions", notice},
		{"the stops", down[:3], "stops", notice},
		{"the board it drills to", down, "departures", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := notifying(t, "44", tc.keys...)
			if got := a.State().Screen; got != tc.screen {
				t.Fatalf("Screen = %v, want %v", got, tc.screen)
			}

			var reported any
			if got := a.Semantic()["detour"]; got != nil {
				reported = got
			}
			if tc.want == "" {
				if reported != nil {
					t.Errorf("the artifact reports %q, want nothing", reported)
				}
			} else if reported != tc.want {
				t.Errorf("the artifact reports %v, want %q", reported, tc.want)
			}

			// Drawn with the glyph, which the artifact does not carry: the glyph
			// is how a screen says it and not part of what was said. Spelled out
			// rather than built with warning, which is the function under test.
			want := ""
			if tc.want != "" {
				want = "⚠ " + tc.want
			}
			if got := a.View(100, 14).Notice; got != want {
				t.Errorf("the screen draws %q, want %q", got, want)
			}
			if tc.want != "" {
				if got := a.View(100, 14).NoticeStyle; got != unbold(dueSoon) {
					t.Errorf("the warning is %v, want the amber a late bus takes, unbolded", got)
				}
			}
		})
	}
}

// A detour names its routes, and a screen about another route shows none.
func TestADetourAboutAnotherRouteIsNotShownHere(t *testing.T) {
	t.Parallel()
	a := notifying(t, "19, 42", Key{Kind: Enter}, Key{Kind: Enter})

	if got := a.Semantic()["detour"]; got != nil {
		t.Errorf("the artifact reports %v, want nothing", got)
	}
	if got := a.View(100, 14).Notice; got != "" {
		t.Errorf("the screen draws %q, want nothing", got)
	}
}

// The last crumb of a trail says what the screen is about. A rail screen paints
// it in the line's own colour and a bus screen in the app's red, whatever colour
// the route has.
func TestTheLastCrumbTakesTheLinesColourOnRail(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		keys []Key
		want render.Style
	}{
		{"a bus stop list", []Key{{Kind: Enter}, {Kind: Enter}, {Kind: Enter}}, brand},
		{"a rail station list", []Key{{Kind: Down}, {Kind: Enter}, {Kind: Enter}, {Kind: Enter}}, guideStyle("d30f1d")},
		// The board carries the answer down with it, so the crumb naming the
		// stop is painted the same way the station list was.
		{"a bus board", []Key{{Kind: Enter}, {Kind: Enter}, {Kind: Enter}, {Kind: Enter}}, brand},
		{"a rail board", []Key{{Kind: Down}, {Kind: Enter}, {Kind: Enter}, {Kind: Enter}, {Kind: Enter}}, guideStyle("d30f1d")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := notifying(t, "44", tc.keys...)
			trail := a.View(100, 14).Status.Trail
			if len(trail) == 0 {
				t.Fatal("the status bar has no trail")
			}
			if got := trail[len(trail)-1].Style; got != tc.want {
				t.Errorf("the last crumb is %v, want %v", got, tc.want)
			}
		})
	}
}

// A pin stores a stop and a route and never a colour, so the board it opens
// takes the route's colours off the rows it is showing. Without them the badge in
// that board's trail is drawn with no colour at all.
func TestABoardOpenedFromAPinPaintsItsBadge(t *testing.T) {
	t.Parallel()
	a := notifying(t, "44")
	a.Pinned([]Pin{{Stop: "s1", Route: "44", Headsign: "Hurdman"}})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	press(t, a, Key{Kind: Enter})

	if got := a.State().Screen; got != "departures" {
		t.Fatalf("Screen = %v, want the pinned board", got)
	}
	for _, c := range a.View(100, 14).Status.Trail {
		if c.Text == badge("44") {
			if want := badgeStyle("0057b8"); c.Style != want {
				t.Errorf("the badge crumb is %v, want %v", c.Style, want)
			}
			return
		}
	}
	t.Errorf("the trail has no badge for the pinned route: %+v", a.View(100, 14).Status.Trail)
}

// The artifact reports what the feed said and how far off the table it was. A
// trip the feed named and put on time reports zero, and a trip it never named
// reports nothing. Those are different things.
func TestTheArtifactReportsWhatTheFeedSaidAboutEachDeparture(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		at   int
		live any
		late any
	}{
		{"four minutes late", 30240, 30240, 4},
		{"on time to the second", 30000, 30000, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := liveApp(t, "08:00", `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28800), 10)+
				`},"Entity":[{"TripUpdate":{"Trip":{"TripId":"t1"},"StopTimeUpdate":[
				{"StopId":"s1","Arrival":{"HasTime":true,"Time":`+strconv.FormatInt(stamp(tc.at), 10)+`}}]}}]}`)

			rows, ok := a.Semantic()["departures"].([]any)
			if !ok || len(rows) < 2 {
				t.Fatalf("the artifact reports %v, want a list of departures", a.Semantic()["departures"])
			}
			named, _ := rows[0].(map[string]any)
			if named["live"] != tc.live || named["late"] != tc.late {
				t.Errorf("the named trip reports live %v and late %v, want %v and %v",
					named["live"], named["late"], tc.live, tc.late)
			}
			// The next row is a trip the feed never mentioned.
			quiet, _ := rows[1].(map[string]any)
			if quiet["live"] != nil || quiet["late"] != nil {
				t.Errorf("the trip the feed never named reports live %v and late %v, want nothing for both",
					quiet["live"], quiet["late"])
			}
		})
	}
}

// A trip written past midnight belongs to the service day that ended, and a
// board draws it shifted back onto the day it is drawn for. A prediction for that
// trip arrives as an absolute instant, so the clock is what puts the two on one
// axis. An implementation that forgot the shift, or took it twice, would move the
// row by a whole day.
func TestAPredictionOnATripBorrowedFromYesterdayLandsOnTheDrawnDay(t *testing.T) {
	t.Parallel()
	// 25:10 yesterday is 01:10 on the day being drawn, which is 4,200 seconds in.
	const borrowed = 25*3600 + 10*60 - 86400
	// The feed puts it four minutes later, at 01:14.
	const expected = borrowed + 240

	live, err := realtime.Parse(strings.NewReader(
		`{"Header":{"Timestamp":` + strconv.FormatInt(stamp(0), 10) + `},"Entity":[
		{"TripUpdate":{"Trip":{"TripId":"late"},"StopTimeUpdate":[
		{"StopId":"s1","Arrival":{"HasTime":true,"Time":` + strconv.FormatInt(stamp(expected), 10) + `}}]}}]}`))
	if err != nil {
		t.Fatalf("realtime.Parse: %v", err)
	}

	k, err := clock.At("2026-09-12", "00:00", time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("clock.At: %v", err)
	}
	a := New(stub{
		stops: []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "1869", Name: "A STOP"}}},
		board: map[string][]cache.Departure{"s1": {
			{Trip: "late", Route: "44", Headsign: "Hurdman", Scheduled: borrowed, AfterMidnight: true},
		}},
	}, k, Feed{Live: live})
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	press(t, a, Key{Kind: Char, Rune: 'a'}, Key{Kind: Enter})

	row := a.View(100, 20).Rows[0]
	if got := cellAt(t, row, boardClock); got.Text != "01:14" {
		t.Errorf("the clock says %q, want %q", got.Text, "01:14")
	}
	if got := countdownIn(t, row); got.Text != "1h 14 min" {
		t.Errorf("the countdown says %q, want %q", got.Text, "1h 14 min")
	}
	if got := cellAt(t, row, boardNote); got.Text != "4 late" {
		t.Errorf("the note says %q, want %q", got.Text, "4 late")
	}
}

// Every screen draws in the same frame, and the frame carries what belongs to
// the world rather than to the screen. A screen that built its own would lose
// the weather off the rule the day somebody added a seventh one, and a rule with
// nothing on it is what a missing feed draws.
func TestEveryScreenDrawsInTheFrameTheWorldGivesIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		keys   []Key
		screen string
		cursor bool
	}{
		{"the first screen", nil, "mode", true},
		{"the routes", []Key{{Kind: Enter}}, "routes", true},
		{"the directions", []Key{{Kind: Enter}, {Kind: Enter}}, "directions", true},
		{"the stops", []Key{{Kind: Enter}, {Kind: Enter}, {Kind: Enter}}, "stops", true},
		{"the search", []Key{{Kind: Char, Rune: 'b'}}, "search", true},
		// A board is not a list anyone moves through.
		{"the board", []Key{{Kind: Enter}, {Kind: Enter}, {Kind: Enter}, {Kind: Enter}}, "departures", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := notifying(t, "44", tc.keys...)
			a.Outside(Weather{Label: "⛆ light rain · 21°"})

			if got := a.State().Screen; got != tc.screen {
				t.Fatalf("Screen = %v, want %v", got, tc.screen)
			}
			v := a.View(100, 14)
			if v.Banner != "⛆ light rain · 21°" || v.BannerStyle != muted {
				t.Errorf("the rule carries %q in %v, want the weather in the dim colour", v.Banner, v.BannerStyle)
			}
			if v.Rule != faint {
				t.Errorf("the rules are %v, want %v", v.Rule, faint)
			}
			if v.EmptyStyle != emptyPaint {
				t.Errorf("an empty list would be %v, want %v", v.EmptyStyle, emptyPaint)
			}
			if v.NoticeStyle != detourPaint {
				t.Errorf("a warning would be %v, want %v", v.NoticeStyle, detourPaint)
			}
			if v.Cursor != tc.cursor {
				t.Errorf("Cursor = %v, want %v", v.Cursor, tc.cursor)
			}
			if v.Width != 100 || v.Height != 14 {
				t.Errorf("the frame is %dx%d, want 100x14", v.Width, v.Height)
			}
		})
	}
}

// Every board names the list its route is on, however it was reached. A pin
// stores a stop and a route and never a mode, so the board it opens looks the
// mode up through the current cache, the way it resolves everything else a pin
// carries.
//
// A hundred-cell terminal hides this: the trail there is long enough to lose its
// leading crumb, so a board with no mode and a board whose mode was dropped draw
// the same row. Measured at 119 cells, where both fit.
func TestEveryBoardNamesTheListItsRouteIsOn(t *testing.T) {
	t.Parallel()
	drilled := notifying(t, "44", Key{Kind: Enter}, Key{Kind: Enter}, Key{Kind: Enter}, Key{Kind: Enter})
	if got := first(t, drilled).Text; got != "Bus" {
		t.Errorf("the drilled board's trail starts %q, want %q", got, "Bus")
	}

	pinned := notifying(t, "44")
	pinned.Pinned([]Pin{{Stop: "s1", Route: "44", Headsign: "Hurdman"}})
	if err := pinned.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	press(t, pinned, Key{Kind: Enter})
	if got := pinned.State().Screen; got != "departures" {
		t.Fatalf("Screen = %v, want the pinned board", got)
	}
	if got := first(t, pinned).Text; got != "Bus" {
		t.Errorf("the pinned board's trail starts %q, want %q", got, "Bus")
	}
}

// first is the leading crumb of the status bar's trail.
func first(t *testing.T, a *App) render.Crumb {
	t.Helper()
	trail := a.View(100, 14).Status.Trail
	if len(trail) == 0 {
		t.Fatalf("the status bar has no trail: %+v", a.View(100, 14).Status)
	}
	return trail[0]
}

// ---- the cadence ----------------------------------------------------------

// polling is the program with a queue of answers and a clock that moves.
func polling(t *testing.T, answers ...Answer) *App {
	t.Helper()
	k, err := clock.At("2026-09-12", "08:00", time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("clock.At: %v", err)
	}
	a := New(liveStub(), k, NoKey())
	a.Answers(answers)
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return a
}

// answered is one answer carrying a feed stamped at a service second.
func answered(t *testing.T, second int) Answer {
	t.Helper()
	live, err := realtime.Parse(strings.NewReader(
		`{"Header":{"Timestamp":` + strconv.FormatInt(stamp(second), 10) + `},"Entity":[]}`))
	if err != nil {
		t.Fatalf("realtime.Parse: %v", err)
	}
	return Answer{Live: live}
}

// due is what the artifact says about the poller.
func due(t *testing.T, a *App) (dueIn, requests, failures int, note string) {
	t.Helper()
	f, ok := a.Semantic()["feed"].(map[string]any)
	if !ok {
		t.Fatalf("the artifact reports the feed as %T, want an object", a.Semantic()["feed"])
	}
	for _, key := range []string{"due_in", "requests", "failures", "note"} {
		if _, held := f[key]; !held {
			t.Fatalf("the artifact reports %v, want a %s in it", f, key)
		}
	}
	return f["due_in"].(int), f["requests"].(int), f["failures"].(int), f["note"].(string)
}

// The poller asks once at the start, then on a cadence. A wait covers every
// interval it spans, so a minute and a half is three attempts and not one.
func TestAWaitCoversEveryIntervalItSpans(t *testing.T) {
	t.Parallel()
	const start = 8 * 3600
	a := polling(t, answered(t, start), answered(t, start), answered(t, start), answered(t, start))

	if _, requests, _, _ := due(t, a); requests != 1 {
		t.Errorf("at the start %d requests were made, want 1", requests)
	}
	a.Wait(90)
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	// Due at 25, 50 and 75 seconds in, so three more attempts.
	dueIn, requests, _, _ := due(t, a)
	if requests != 4 {
		t.Errorf("after ninety seconds %d requests were made, want 4", requests)
	}
	// The last fell due 75 seconds in, so the next is at 100, and the clock has
	// reached 90.
	if dueIn != 10 {
		t.Errorf("the next attempt is %d seconds away, want 10", dueIn)
	}
}

// A refusal backs the next attempt off to a minute, and each refusal after it
// doubles that. One answer puts the cadence back rather than halving the backoff.
func TestARefusalBacksOffAndAnAnswerPutsTheCadenceBack(t *testing.T) {
	t.Parallel()
	const start = 8 * 3600
	refused := Answer{Refusal: "http status: 503"}
	a := polling(t, answered(t, start), refused, refused, answered(t, start+160), refused)

	for _, tc := range []struct {
		wait            int
		dueIn, requests int
		failures        int
	}{
		// The first refusal lands at 25 seconds in and waits a minute.
		{30, 55, 2, 1},
		// Thirty more is not a minute, so nothing is asked at all.
		{30, 25, 2, 1},
		// The minute is up, and the second refusal doubles the wait to two.
		{40, 105, 3, 2},
		// Still not due: the backoff really did grow.
		{60, 45, 3, 2},
		// An answer, and the cadence is back to twenty-five seconds.
		{50, 20, 4, 0},
		// A refusal after a recovery starts again at a minute, not at four: the
		// attempt fell due five seconds before the clock got here, so the minute
		// has five of its seconds already spent.
		{30, 50, 5, 1},
	} {
		a.Wait(tc.wait)
		if err := a.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		dueIn, requests, failures, _ := due(t, a)
		if dueIn != tc.dueIn || requests != tc.requests || failures != tc.failures {
			t.Errorf("after %ds: due in %d, %d requests, %d failures, want %d, %d and %d",
				tc.wait, dueIn, requests, failures, tc.dueIn, tc.requests, tc.failures)
		}
	}
}

// An attempt that comes due with nothing left to answer stays owed. The count
// stops climbing and the countdown holds at zero, which is what a feed gone
// quiet looks like from inside.
func TestAnAttemptWithNothingLeftToAnswerStaysOwed(t *testing.T) {
	t.Parallel()
	a := polling(t, answered(t, 8*3600))

	for _, wait := range []int{60, 600} {
		a.Wait(wait)
		if err := a.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		dueIn, requests, _, _ := due(t, a)
		if dueIn != 0 || requests != 1 {
			t.Errorf("after %ds: due in %d with %d requests, want 0 and 1", wait, dueIn, requests)
		}
	}
}

// A refusal with nothing behind it has to be said out loud: there is no board to
// protect and silence reads as a quiet Sunday. A refusal after an answer keeps
// showing the answer's age instead, because that is what a person judges the
// predictions by.
func TestARefusalIsNamedOnlyWhenNothingHasArrived(t *testing.T) {
	t.Parallel()
	refused := Answer{Refusal: "http status: 503"}

	down := polling(t, refused)
	if _, _, failures, note := due(t, down); note != "live: http status: 503" || failures != 1 {
		t.Errorf("with nothing but a refusal the feed says %q after %d failures, want the refusal named",
			note, failures)
	}
	// Down to a board, which is the screen that shows what the poller says.
	press(t, down, Key{Kind: Char, Rune: 'f'}, Key{Kind: Enter})
	if hints := down.View(100, 20).Status.Hints; !strings.HasPrefix(hints, "live: http status: 503 · ") {
		t.Errorf("the hints are %q, want them to start with the refusal", hints)
	}

	kept := polling(t, answered(t, 8*3600), refused)
	kept.Wait(30)
	if err := kept.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if _, _, failures, note := due(t, kept); note != "live 30s" || failures != 1 {
		t.Errorf("after a refusal the feed says %q with %d failures, want the age it still has",
			note, failures)
	}
}

// A fixture that hands over a feed and no queue has no cadence to report, and
// the artifact says only what it heard. The counters appear when there is a
// poller to count.
func TestAFeedWithNoQueueReportsNoCadence(t *testing.T) {
	t.Parallel()
	a := liveApp(t, "08:00", `{"Header":{"Timestamp":`+strconv.FormatInt(stamp(28800), 10)+`},"Entity":[]}`)

	f, ok := a.Semantic()["feed"].(map[string]any)
	if !ok {
		t.Fatalf("the artifact reports the feed as %T, want an object", a.Semantic()["feed"])
	}
	if len(f) != 1 || f["note"] != "live 0s" {
		t.Errorf("the artifact reports %v, want the note alone", f)
	}
}

// The poller runs before the screen is built, so a prediction that arrives on
// this turn is on this turn's board. Polling after would draw every arrival one
// frame late, which reads as a feed that is always a step behind.
func TestAPredictionThatArrivesThisTurnIsOnThisTurnsBoard(t *testing.T) {
	t.Parallel()
	// Nothing at the start, then an answer that puts trip t1 four minutes late.
	late, err := realtime.Parse(strings.NewReader(
		`{"Header":{"Timestamp":` + strconv.FormatInt(stamp(28800), 10) + `},"Entity":[
		{"TripUpdate":{"Trip":{"TripId":"t1"},"StopTimeUpdate":[
		{"StopId":"s1","Arrival":{"HasTime":true,"Time":` + strconv.FormatInt(stamp(30240), 10) + `}}]}}]}`))
	if err != nil {
		t.Fatalf("realtime.Parse: %v", err)
	}
	a := polling(t, Answer{Refusal: "http status: 503"}, Answer{Live: late})
	press(t, a, Key{Kind: Char, Rune: 'f'}, Key{Kind: Enter})

	if got := cellAt(t, a.View(100, 20).Rows[0], boardNote).Text; got != "sched" {
		t.Fatalf("before the answer the row says %q, want %q", got, "sched")
	}
	// A minute, because the refusal above backed the next attempt off that far.
	a.Wait(60)
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := cellAt(t, a.View(100, 20).Rows[0], boardNote).Text; got != "4 late" {
		t.Errorf("on the turn the answer arrived the row says %q, want %q", got, "4 late")
	}
}
