package cache

import (
	"context"
	"slices"
	"strconv"
	"testing"
)

// A world is a cache the test built. It holds exactly what the test is about.
type world struct {
	t *testing.T
	*Cache
}

func newWorld(t *testing.T) *world {
	t.Helper()
	c, err := openMemory()
	if err != nil {
		t.Fatalf("openMemory: %v", err)
	}
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return &world{t: t, Cache: c}
}

func (w *world) exec(q string, args ...any) {
	w.t.Helper()
	if _, err := w.db.Exec(q, args...); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
}

// runsOn takes the seven weekday flags in GTFS order, Monday first.
func (w *world) service(id string, days [7]int, start, end string) {
	w.t.Helper()
	w.exec(`INSERT INTO calendar VALUES (?,?,?,?,?,?,?,?,?,?)`,
		id, days[0], days[1], days[2], days[3], days[4], days[5], days[6], start, end)
}

func (w *world) exception(id, date string, kind int) {
	w.t.Helper()
	w.exec(`INSERT INTO calendar_dates VALUES (?,?,?)`, id, date, kind)
}

func (w *world) route(id, short, long string, kind, order int) {
	w.t.Helper()
	w.exec(`INSERT INTO routes VALUES (?,?,?,?,'','',?)`, id, short, long, kind, order)
}

func (w *world) trip(id, routeID, serviceID, headsign string) {
	w.t.Helper()
	w.exec(`INSERT INTO trips VALUES (?,?,?,?,0)`, id, routeID, serviceID, headsign)
}

func (w *world) stop(id, code, name string) {
	w.t.Helper()
	w.exec(`INSERT INTO stops VALUES (?,?,?,0,0,0,'',NULL)`, id, code, name)
}

func (w *world) calls(tripID, stopID string, arr int) {
	w.t.Helper()
	w.exec(`INSERT INTO stop_times VALUES (?,?,1,?,?)`, tripID, stopID, arr, arr)
}

const (
	friday   = "20260911"
	saturday = "20260912"
)

var (
	fridays  = [7]int{0, 0, 0, 0, 1, 0, 0}
	weekdays = [7]int{1, 1, 1, 1, 1, 0, 0}
	everyDay = [7]int{1, 1, 1, 1, 1, 1, 1}
)

func TestOnlyAServiceThatRunsOnTheDateIsActive(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.service("week", weekdays, "20260830", "20260920")

	for _, tc := range []struct {
		name, date string
		want       []string
	}{
		{"a Friday runs both", friday, []string{"fri", "week"}},
		{"a Saturday runs neither", saturday, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := w.ActiveServices(context.Background(), tc.date)
			if err != nil {
				t.Fatalf("ActiveServices: %v", err)
			}
			slices.Sort(got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("ActiveServices(%s) = %v, want %v", tc.date, got, tc.want)
			}
		})
	}
}

func TestADateOutsideTheCalendarWindowIsNotActive(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260918", "20260920") // starts a week after
	got, err := w.ActiveServices(context.Background(), friday)
	if err != nil {
		t.Fatalf("ActiveServices: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ActiveServices = %v, want none", got)
	}
}

func TestACalendarDateHonoursBothExceptionTypes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind int
		want []string
	}{
		{"exception 1 adds a service the weekday refuses", 1, []string{"fri"}},
		{"exception 2 removes a service the weekday allows", 2, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			w := newWorld(t)
			date := saturday
			if tc.kind == 2 {
				date = friday
			}
			w.service("fri", fridays, "20260830", "20260920")
			w.exception("fri", date, tc.kind)

			got, err := w.ActiveServices(context.Background(), date)
			if err != nil {
				t.Fatalf("ActiveServices: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("ActiveServices(%s) = %v, want %v", date, got, tc.want)
			}
		})
	}
}

// route_id repeats across booking periods. 44 and 44-1 are one route.
func TestARouteThatRepeatsAcrossBookingPeriodsIsListedOnce(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.route("44-1", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "fri", "Hurdman")
	w.trip("t2", "44-1", "fri", "Hurdman")

	got, err := w.Routes(context.Background(), Bus, friday)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Routes = %v, want one row", got)
	}
	if got[0].ShortName != "44" {
		t.Errorf("ShortName = %q, want %q", got[0].ShortName, "44")
	}
}

func TestARouteWithNoTripOnTheDateIsNotListed(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.trip("t1", "44", "fri", "Hurdman")

	for _, tc := range []struct {
		name, date string
		want       int
	}{
		{"the day it runs", friday, 1},
		{"the day it does not", saturday, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := w.Routes(context.Background(), Bus, tc.date)
			if err != nil {
				t.Fatalf("Routes: %v", err)
			}
			if len(got) != tc.want {
				t.Errorf("Routes(%s) = %v, want %d", tc.date, got, tc.want)
			}
		})
	}
}

func TestEachModeListsOnlyItsOwnRoutes(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.route("1-350", "1", "Blair <> Tunney's Pasture", 0, 1)
	w.trip("t1", "44", "all", "Hurdman")
	w.trip("t2", "1-350", "all", "Blair")

	for _, tc := range []struct {
		name string
		mode Mode
		want string
	}{
		{"bus", Bus, "44"},
		{"rail", Rail, "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := w.Routes(context.Background(), tc.mode, saturday)
			if err != nil {
				t.Fatalf("Routes: %v", err)
			}
			if len(got) != 1 || got[0].ShortName != tc.want {
				t.Errorf("Routes = %v, want one row %q", got, tc.want)
			}
		})
	}
}

func TestTheRouteListIsOrderedByTheNumberEachRouteStartsWith(t *testing.T) {
	t.Parallel()
	// Not by the feed's own sort_order, which orders 1 after 2 among the rail
	// lines, and not as text, which puts 10 between 1 and 2. A letter is not a
	// number, so a name that starts with one comes after every name that does
	// not: E1 and N45 are a different kind of thing from 6 and 12.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	for i, r := range []struct {
		name  string
		order int
	}{{"12", 4}, {"E1", 1}, {"2", 3}, {"7", 5}, {"N45", 2}, {"1", 6}} {
		w.route(r.name, r.name, "somewhere <> somewhere", 3, r.order)
		w.trip("t"+strconv.Itoa(i), r.name, "all", "Hurdman")
	}

	got, err := w.Routes(context.Background(), Bus, saturday)
	if err != nil {
		t.Fatalf("Routes: %v", err)
	}
	names := make([]string, len(got))
	for i, r := range got {
		names[i] = r.ShortName
	}
	// The length is asserted through the comparison on purpose. A slice of one
	// is in order whatever the query did.
	if want := []string{"1", "2", "7", "12", "E1", "N45"}; !slices.Equal(names, want) {
		t.Errorf("Routes = %v, want %v", names, want)
	}
}

func TestTheDirectionsOfARouteComeBackWithTheirTripCounts(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.route("44-1", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.exec(`INSERT INTO trips VALUES ('a','44','fri','Hurdman',1)`)
	w.exec(`INSERT INTO trips VALUES ('b','44-1','fri','Hurdman',1)`)
	w.exec(`INSERT INTO trips VALUES ('c','44','fri','Billings Bridge',0)`)

	got, err := w.Directions(context.Background(), "44", friday)
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Directions = %v, want two", got)
	}
	// The busier direction first, and a route counted across both of its
	// booking periods.
	if got[0].Headsign != "Hurdman" || got[0].Trips != 2 {
		t.Errorf("Directions[0] = %+v, want two trips across 44 and 44-1", got[0])
	}
	if got[1].Headsign != "Billings Bridge" || got[1].Trips != 1 {
		t.Errorf("Directions[1] = %+v", got[1])
	}
}

func TestTwoDirectionsWithOneHeadsignAreOneDirection(t *testing.T) {
	t.Parallel()
	// A direction is where a route is going, and that is the headsign. The feed
	// says which way round it is in direction_id, and the two disagree: rail line
	// 1 runs one trip towards Lyon under each id, and offering "toward Lyon"
	// twice asks a person to choose between two identical rows.
	w := newWorld(t)
	w.service("all", everyDay, "20260830", "20260920")
	w.route("1", "1", "Blair <> Tunney's Pasture", 0, 1)
	w.exec(`INSERT INTO trips VALUES ('t1','1','all','Lyon',0)`)
	w.exec(`INSERT INTO trips VALUES ('t2','1','all','Lyon',1)`)
	w.exec(`INSERT INTO trips VALUES ('t3','1','all','Blair',0)`)

	got, err := w.Directions(context.Background(), "1", saturday)
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Directions = %v, want two: Lyon and Blair", got)
	}
	if got[0].Headsign != "Lyon" || got[0].Trips != 2 {
		t.Errorf("the first row is %q with %d trips, want Lyon with both of its", got[0].Headsign, got[0].Trips)
	}
}

func TestADirectionWithNoTripOnTheDateIsNotOffered(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.exec(`INSERT INTO trips VALUES ('a','44','fri','Hurdman',1)`)

	got, err := w.Directions(context.Background(), "44", saturday)
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Directions = %v, want none", got)
	}
}

// A route's stops are in the order the bus calls at them, not in the order
// their names sort. WALKLEY / SIBLINGS comes before WALKLEY / HAMPSTEAD
// because that is the way the route runs.
func TestARoutesStopsAreInTheOrderTheBusCallsAtThem(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.exec(`INSERT INTO trips VALUES ('a','44','fri','Hurdman',1)`)
	for i, name := range []string{"ZEBRA", "APPLE", "MIDDLE"} {
		id := "s" + strconv.Itoa(i)
		w.stop(id, "300"+strconv.Itoa(i), name)
		w.exec(`INSERT INTO stop_times VALUES ('a',?,?,?,?)`, id, i+1, 30000+i, 30000+i)
	}

	got, err := w.RouteStops(context.Background(), "44", "Hurdman", friday)
	if err != nil {
		t.Fatalf("RouteStops: %v", err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	if !slices.Equal(names, []string{"ZEBRA", "APPLE", "MIDDLE"}) {
		t.Errorf("RouteStops = %v, want them in calling order", names)
	}
}

func TestARoutesStopsAreOnlyTheOnesThatDirectionCallsAt(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("44", "44", "Billings Bridge <> Hurdman", 3, 51)
	w.exec(`INSERT INTO trips VALUES ('a','44','fri','Hurdman',1)`)
	w.exec(`INSERT INTO trips VALUES ('b','44','fri','Billings Bridge',0)`)
	w.stop("s1", "3034", "ONE WAY ONLY")
	w.stop("s2", "3035", "THE OTHER WAY")
	w.exec(`INSERT INTO stop_times VALUES ('a','s1',1,30000,30000)`)
	w.exec(`INSERT INTO stop_times VALUES ('b','s2',1,30000,30000)`)

	got, err := w.RouteStops(context.Background(), "44", "Hurdman", friday)
	if err != nil {
		t.Fatalf("RouteStops: %v", err)
	}
	if len(got) != 1 || got[0].Name != "ONE WAY ONLY" {
		t.Errorf("RouteStops = %v, want only the stop that direction calls at", got)
	}
}

// The busiest direction comes first, which is what the reference draws for
// routes 44 and 48.
func TestTheBusiestDirectionComesFirst(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("fri", fridays, "20260830", "20260920")
	w.route("48", "48", "Hurdman <> Carleton", 3, 55)
	// direction 1 runs more trips than direction 0, so it comes first even
	// though its id is higher and its headsign sorts later.
	w.exec(`INSERT INTO trips VALUES ('a','48','fri','Carleton',1)`)
	w.exec(`INSERT INTO trips VALUES ('b','48','fri','Carleton',1)`)
	w.exec(`INSERT INTO trips VALUES ('c','48','fri','Hurdman',0)`)

	got, err := w.Directions(context.Background(), "48", friday)
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Directions = %v, want two", got)
	}
	if got[0].Headsign != "Carleton" || got[0].Trips != 2 {
		t.Errorf("Directions[0] = %+v, want the two-trip direction first", got[0])
	}
}

// Directions tie on trip count, and the reference orders a tie by headsign.
// Route 1 runs eight trips each way, and Blair comes before Tunney's Pasture.
func TestDirectionsThatTieOnTripCountAreOrderedByHeadsign(t *testing.T) {
	t.Parallel()
	w := newWorld(t)
	w.service("week", weekdays, "20260830", "20260920")
	w.route("1-373-1", "1", "Blair <> Tunney's Pasture", 0, 1)
	// Direction 1 is Tunney's Pasture and direction 0 is Blair, eight each.
	for i := 0; i < 8; i++ {
		w.exec(`INSERT INTO trips VALUES (?,'1-373-1','week','Blair',0)`, "b"+strconv.Itoa(i))
		w.exec(`INSERT INTO trips VALUES (?,'1-373-1','week',"Tunney's Pasture",1)`, "t"+strconv.Itoa(i))
	}

	got, err := w.Directions(context.Background(), "1", "20260914")
	if err != nil {
		t.Fatalf("Directions: %v", err)
	}
	var names []string
	for _, d := range got {
		names = append(names, d.Headsign)
	}
	if !slices.Equal(names, []string{"Blair", "Tunney's Pasture"}) {
		t.Errorf("Directions = %v, want Blair first", names)
	}
}
