// Package cache reads the SQLite cache an ingest builds from the GTFS feed.
//
// Every query here takes the service date, because almost nothing in the feed
// is true on its own. A route exists all year and runs on some days. A stop
// stays in the cache after the last trip that called there stopped running.
// A query that forgets the date answers a question nobody asked.
package cache

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	// The driver. database/sql ships none for SQLite, and this one is pure Go,
	// so the build needs no C toolchain.
	_ "modernc.org/sqlite"
)

// Cache is an open cache file.
type Cache struct {
	db *sql.DB
}

// Open opens the cache at path for reading. A replay must never write to the
// directory it replays, so the file is opened read-only.
func Open(path string) (*Cache, error) {
	c, err := open("file:" + path + "?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("opening cache %s: %w", path, err)
	}
	// sql.Open opens nothing. Without this, a cache that cannot be read first
	// reports itself from the middle of whichever query runs first, wearing
	// that query's error message.
	if err := c.db.Ping(); err != nil {
		c.db.Close() //nolint:errcheck // the open already failed, and this error is the one to report
		return nil, fmt.Errorf("opening cache %s: %w", path, err)
	}
	return c, nil
}

// openMemory builds an empty cache in memory and applies Schema to it. Tests
// use it to build the world they are about.
func openMemory() (*Cache, error) {
	c, err := open(":memory:")
	if err != nil {
		return nil, err
	}
	if _, err := c.db.Exec(Schema + Indexes); err != nil {
		return nil, fmt.Errorf("applying schema: %w", err)
	}
	return c, nil
}

func open(dsn string) (*Cache, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// An in-memory database belongs to its connection. A pool of them would
	// hand a query an empty database that another connection had not filled.
	db.SetMaxOpenConns(1)
	return &Cache{db: db}, nil
}

// Close releases the cache.
func (c *Cache) Close() error { return c.db.Close() }

// Mode is the kind of vehicle a screen is about.
type Mode int

// The modes the program offers. The zero value is not one of them: it means no
// mode was recorded, which is the state of a board a pin opened, because a pin
// stores a stop and a route and never which list they were found in.
const (
	Bus Mode = iota + 1
	Rail
)

// types are the GTFS route_type values the mode covers. Bus is 3. Rail covers
// light rail, underground and heavy rail, because the O-Train is 0 today and
// the feed is free to describe a line as any of them.
func (m Mode) types() []int {
	switch m {
	case Bus:
		return []int{3}
	case Rail:
		return []int{0, 1, 2}
	}
	// Named rather than answered: a query for no mode would return whatever the
	// last branch happened to be, and a screen asking for one has a defect a
	// panic names and an empty list hides.
	panic(fmt.Sprintf("mode %d has no route types", int(m)))
}

// Route is one route, already collapsed across its booking periods.
type Route struct {
	ShortName string
	LongName  string
	// Colour is the route's own from the feed, without the leading hash. A
	// badge is drawn on it, in black or white: the feed's route_text_color is
	// not read, because it is not legible on its own background often enough to
	// be trusted.
	Colour string
}

// Stop is one stop that a search matched.
type Stop struct {
	ID   string
	Code string
	// Name is the stop name as the feed writes it, which already carries any
	// platform suffix. A platform is never read back out of it: CANTERBURY /
	// AD. 860 holds a municipal address and no platform at all.
	Name     string
	Platform string
}

// Match is a stop a search found, with what calls there on the date searched.
//
// A search is the only thing that asks what calls at a stop, so those two
// fields live here rather than on every Stop a route list or a pin resolves.
type Match struct {
	Stop
	// Routes are the short names calling here today, each once, in the order a
	// person reads route numbers.
	Routes []string
	// Toward is where most of the trips calling here are going. One line has
	// room for one destination, and the busiest one is the answer to "what is
	// this stop for".
	Toward string
}

// Direction is one direction of a route, as the direction screen offers it. It
// is named by where it is going and not by the feed's direction_id, because the
// two disagree: one headsign can appear under both ids.
type Direction struct {
	Headsign string
	Trips    int
}

// Departure is one scheduled call at a stop.
type Departure struct {
	// Trip is the id the realtime feed names a prediction by. It is the only
	// thing that joins a row of a schedule to a row of a feed.
	Trip     string
	Route    string
	Headsign string
	// Scheduled is seconds on the service day the board is drawn for. It can
	// exceed 86400, because a service day reaches 28:xx, and it is negative
	// for nothing: a row borrowed from yesterday is shifted onto this axis.
	Scheduled int
	// AfterMidnight marks a row that came from yesterday's service day.
	AfterMidnight bool
	// Colour is the route's own, for its badge.
	Colour string
}

// ActiveServices returns the service ids that run on date, written YYYYMMDD.
// It honours the weekly calendar, the window the calendar is valid over, and
// both kinds of calendar_dates exception.
func (c *Cache) ActiveServices(ctx context.Context, date string) ([]string, error) {
	col, err := weekdayColumn(date)
	if err != nil {
		return nil, err
	}
	// col is one of seven fixed names, chosen by weekdayColumn and never by a
	// caller, so it cannot carry anything but a column name.
	q := `
		SELECT service_id FROM calendar
		 WHERE ` + col + ` = 1 AND start_date <= ? AND end_date >= ?
		UNION
		SELECT service_id FROM calendar_dates WHERE date = ? AND exception = 1
		EXCEPT
		SELECT service_id FROM calendar_dates WHERE date = ? AND exception = 2`

	return collect(ctx, c, "active services on "+date, q,
		[]any{date, date, date, date},
		func(rows *sql.Rows) (string, error) {
			var id string
			return id, rows.Scan(&id)
		})
}

// Routes returns the routes of one mode that run on date, one row per route.
//
// A route_id repeats across booking periods, so 44 arrives as both 44 and
// 44-1. They are one route to a rider and they collapse by short name.
func (c *Cache) Routes(ctx context.Context, m Mode, date string) ([]Route, error) {
	services, err := c.ActiveServices(ctx, date)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, nil
	}

	kinds, kindArgs := in(m.types())
	svc, svcArgs := in(services)

	q := `
		SELECT r.short_name, MIN(r.long_name), MIN(r.color)
		  FROM routes r
		 WHERE r.route_type IN (` + kinds + `)
		   AND EXISTS (
		       SELECT 1 FROM trips t
		        WHERE t.route_id = r.route_id
		          AND t.service_id IN (` + svc + `))
		 GROUP BY r.short_name`

	// The order is not the feed's sort_order, which puts rail line 1 after
	// line 2, and it is not the text, which puts 10 between 1 and 2. It is the
	// number a person reads, worked out in Go: no expression SQLite has says
	// it, and short_name is the group here, so the sort is total.
	out, err := collect(ctx, c, "routes on "+date, q,
		append(kindArgs, svcArgs...),
		func(rows *sql.Rows) (Route, error) {
			var r Route
			return r, rows.Scan(&r.ShortName, &r.LongName, &r.Colour)
		})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b Route) int { return byNumber(a.ShortName, b.ShortName) })
	return out, nil
}

// byNumber orders two route names the way a person reads them: by the number
// they start with, and then as text.
//
// A name that starts with a letter comes after every name that starts with a
// digit, because E1, N45 and R1 are a different kind of thing from 6 and 12.
func byNumber(a, b string) int {
	if m, n := leadingNumber(a), leadingNumber(b); m != n {
		return cmp.Compare(m, n)
	}
	return cmp.Compare(a, b)
}

// leadingNumber is the number a name begins with, and the largest there is when
// it begins with something else, so those names sort last.
func leadingNumber(s string) int64 {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, err := strconv.ParseInt(s[:end], 10, 64)
	if err != nil {
		return math.MaxInt64
	}
	return n
}

// SearchStops returns the stops matching query that something calls at on date.
//
// The date is not a refinement. A stop stays in the cache long after the last
// trip that served it stopped running, so a search that matched names alone
// would offer a rider a stop with no service and call it a result.
func (c *Cache) SearchStops(ctx context.Context, query, date string) (found []Match, more bool, err error) {
	// An empty filter is not a search for everything. Backspacing a search
	// down to nothing must empty the list, and not offer every stop in the
	// city as though the person had asked for them.
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, false, nil
	}

	services, err := c.ActiveServices(ctx, date)
	if err != nil {
		return nil, false, err
	}
	if len(services) == 0 {
		return nil, false, nil
	}

	stops, err := c.candidates(ctx, q, date)
	if err != nil {
		return nil, false, err
	}
	calling, err := c.callingAt(ctx, stops, services, date)
	if err != nil {
		return nil, false, err
	}

	// A stop nothing calls at today is noise rather than an answer, and it is
	// dropped here rather than in the query above: see candidates for why.
	for _, stop := range stops {
		if len(found) == searchLimit {
			break
		}
		if at, ok := calling[stop.ID]; ok {
			found = append(found, Match{Stop: stop, Routes: at.routes, Toward: at.toward})
		}
	}
	// At the cap the count is the cap and not a total, because the search
	// stopped looking. Twenty-five matches and twenty-five thousand are the
	// same news from here.
	return found, len(found) >= searchLimit, nil
}

// candidates is the stops whose name or code matches, best first, without
// asking what calls there.
//
// Service is not in this query. A stop with no service today is dropped
// afterwards, which costs it one of the candidates, and the candidate list is
// four times what the screen offers. So a hundred dead platforms in front of a
// live one hide it. That is the bound: looking a little past the limit costs one
// query, and reading every stop in the city costs a search a person can feel
// between keystrokes.
func (c *Cache) candidates(ctx context.Context, q, date string) ([]Stop, error) {
	// Every word must appear in the name, in any order, because "somerset bank"
	// is a person naming two things about one stop and the feed writes it the
	// other way round. The code is matched against the whole of what was typed.
	var names []string
	var args []any
	for _, word := range strings.Fields(q) {
		names = append(names, `name_l LIKE ? ESCAPE '\'`)
		args = append(args, anywhere(word))
	}

	q4 := `
		WITH c AS (
		    SELECT stop_id, stop_code, name, COALESCE(platform, '') AS platform,
		           LOWER(name) AS name_l, LOWER(COALESCE(stop_code, '')) AS code_l,
		           -- Where the export wrote this stop. Three stops share the name
		           -- TERMINAL / SANDFORD FLEMING and nothing in the data orders
		           -- them, so the order they were written in does.
		           rowid AS written
		      FROM stops
		     -- A station is a place, not somewhere a bus stops.
		     WHERE location_type != 1)
		SELECT stop_id, stop_code, name, platform,
		       -- Typing 44 is asking for stop 44 first, and BASELINE / 44 after
		       -- it. A code that is what was typed comes before a code that
		       -- begins with it, which comes before a name that begins with it.
		       CASE WHEN code_l = ?                THEN 0
		            WHEN code_l LIKE ? ESCAPE '\' THEN 1
		            WHEN name_l LIKE ? ESCAPE '\' THEN 2
		            ELSE 3 END AS rank
		  FROM c
		 WHERE (` + strings.Join(names, " AND ") + `)
		    OR code_l LIKE ? ESCAPE '\'
		 -- Total: no two rows of one table share a rowid.
		 ORDER BY rank, name, written
		 LIMIT ?`

	// The rank is in the select list, so its three patterns bind before the
	// ones in the where clause. Reading order is binding order, and that seam
	// has produced a wrong binding here before.
	bound := []any{q, beginning(q), beginning(q)}
	bound = append(bound, args...)
	bound = append(bound, anywhere(q), searchLimit*4)

	return collect(ctx, c, "searching stops for "+q+" on "+date, q4, bound,
		func(rows *sql.Rows) (Stop, error) {
			var st Stop
			var rank int
			return st, rows.Scan(&st.ID, &st.Code, &st.Name, &st.Platform, &rank)
		})
}

// searchLimit is how many stops a search offers. Past it the count reads 25+,
// because the query stopped looking rather than found exactly that many.
const searchLimit = 25

// Stop looks a stop up by its id, and reports whether it is still there.
//
// A pin stores an id, and an update replaces the whole database, so an id from
// an old cache can point at nothing. Nothing trusts a stored id.
func (c *Cache) Stop(ctx context.Context, id string) (Stop, bool, error) {
	q := `SELECT stop_id, stop_code, name, COALESCE(platform, '')
	        FROM stops WHERE stop_id = ?`

	found, err := collect(ctx, c, "looking up stop "+id, q, []any{id},
		func(rows *sql.Rows) (Stop, error) {
			var s Stop
			return s, rows.Scan(&s.ID, &s.Code, &s.Name, &s.Platform)
		})
	if err != nil || len(found) == 0 {
		return Stop{}, false, err
	}
	return found[0], true, nil
}

// Directions returns the directions a route runs on a date, with how many
// trips each one has. A route repeats across booking periods, so the trips are
// counted across every route_id that carries the same short name.
//
// The busiest direction comes first, and a tie is broken by headsign. Route 1
// runs eight trips each way, and Blair comes before Tunney's Pasture.
func (c *Cache) Directions(ctx context.Context, route, date string) ([]Direction, error) {
	services, err := c.ActiveServices(ctx, date)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, nil
	}
	svc, svcArgs := in(services)

	q := `
		SELECT t.headsign, COUNT(*)
		  FROM trips t
		  JOIN routes r ON r.route_id = t.route_id
		 WHERE r.short_name = ? AND t.service_id IN (` + svc + `)
		 -- By headsign and not by direction_id. The feed gives rail line 1 one
		 -- trip towards Lyon under each id, and two rows reading "toward Lyon"
		 -- ask a person to choose between them.
		 GROUP BY t.headsign
		 -- Busiest first, then by name. Total: the headsign is the group.
		 ORDER BY COUNT(*) DESC, t.headsign`

	return collect(ctx, c, "directions of route "+route+" on "+date, q,
		append([]any{route}, svcArgs...),
		func(rows *sql.Rows) (Direction, error) {
			var d Direction
			return d, rows.Scan(&d.Headsign, &d.Trips)
		})
}

// RouteStops returns the stops one direction of a route calls at, in the order
// it calls at them. That is not the order their names sort in, and a rider
// reading the list down is reading the way the bus goes.
func (c *Cache) RouteStops(ctx context.Context, route, headsign, date string) ([]Stop, error) {
	services, err := c.ActiveServices(ctx, date)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, nil
	}
	svc, svcArgs := in(services)

	// One trip's path, and not every trip's stops merged.
	//
	// Trips going the same way do not all call at the same stops: on route 48
	// toward Hurdman some start at Carleton and some at Billings Bridge. Ordering
	// the union by each stop's earliest sequence number interleaves those
	// patterns and draws a route no bus takes, which is the opposite of what this
	// list is for. The longest trip is the one that calls everywhere, and reading
	// it down is reading the way the bus goes.
	//
	// The count ties often, so the id breaks it. Without that the order is the
	// database's to choose and no fixture would catch the day it chose again.
	q := `
		SELECT s.stop_id, s.stop_code, s.name, COALESCE(s.platform, '')
		  FROM stop_times st
		  JOIN stops s ON s.stop_id = st.stop_id
		 WHERE st.trip_id = (
		       SELECT longest.trip_id
		         FROM stop_times longest
		         JOIN trips t ON t.trip_id = longest.trip_id
		         JOIN routes r ON r.route_id = t.route_id
		        WHERE r.short_name = ? AND t.headsign = ?
		          AND t.service_id IN (` + svc + `)
		        GROUP BY longest.trip_id
		        ORDER BY COUNT(*) DESC, longest.trip_id
		        LIMIT 1)
		 GROUP BY s.stop_id
		 -- Total: stop_id is the group, and one trip can call at a stop twice,
		 -- which keeps the first of them.
		 ORDER BY MIN(st.seq), s.stop_id`

	args := append([]any{route, headsign}, svcArgs...)
	return collect(ctx, c, "stops of route "+route+" toward "+headsign+" on "+date, q, args,
		func(rows *sql.Rows) (Stop, error) {
			var s Stop
			return s, rows.Scan(&s.ID, &s.Code, &s.Name, &s.Platform)
		})
}

// callingAt is what calls at each of these stops today, by stop id. A stop
// nothing calls at is absent rather than empty.
//
// This is a second query rather than a pair of subqueries in the first. The
// service list would then appear three times in one statement, and the
// arguments would have to be bound in the order the placeholders happen to be
// written. That seam has produced a wrong binding here before.
func (c *Cache) callingAt(ctx context.Context, stops []Stop, services []string, date string) (map[string]serving, error) {
	ids := make([]string, len(stops))
	for i, s := range stops {
		ids[i] = s.ID
	}
	id, idArgs := in(ids)
	svc, svcArgs := in(services)

	q := `
		SELECT st.stop_id, r.short_name, t.headsign, COUNT(*)
		  FROM stop_times st
		  JOIN trips t ON t.trip_id = st.trip_id
		  JOIN routes r ON r.route_id = t.route_id
		 WHERE st.stop_id IN (` + id + `) AND t.service_id IN (` + svc + `)
		 GROUP BY st.stop_id, r.short_name, t.headsign`

	type call struct {
		stop, route, headsign string
		trips                 int
	}
	calls, err := collect(ctx, c, "routes calling at the stops found on "+date, q,
		append(idArgs, svcArgs...),
		func(rows *sql.Rows) (call, error) {
			var v call
			return v, rows.Scan(&v.stop, &v.route, &v.headsign, &v.trips)
		})
	if err != nil {
		return nil, err
	}

	// No order is asked of the query, because neither answer is the first row:
	// the routes are a set, and the destination is a count across all of them.
	routes := map[string][]string{}
	trips := map[string]map[string]int{}
	for _, v := range calls {
		routes[v.stop] = append(routes[v.stop], v.route)
		if trips[v.stop] == nil {
			trips[v.stop] = map[string]int{}
		}
		trips[v.stop][v.headsign] += v.trips
	}

	at := make(map[string]serving, len(routes))
	for stop, names := range routes {
		slices.SortFunc(names, byNumber)
		at[stop] = serving{routes: slices.Compact(names), toward: busiest(trips[stop])}
	}
	return at, nil
}

// serving is what a search row says about one stop.
type serving struct {
	routes []string
	toward string
}

// busiest is the headsign most of the trips carry, and the first by name when
// two of them tie. A tie is ordinary at a stop two routes share, and the label
// must not change between two runs over the same data.
func busiest(trips map[string]int) string {
	best, most := "", -1
	for headsign, n := range trips {
		if n > most || (n == most && headsign < best) {
			best, most = headsign, n
		}
	}
	return best
}

// Departures returns every call at a stop on one service day, in order.
//
// It reads two days. A trip written 25:10 on Friday is the one a person
// catches at 01:10 on Saturday, so yesterday's late rows are shifted back by a
// day and both days sit on one axis. Everything downstream assumes that axis.
func (c *Cache) Departures(ctx context.Context, stopID, day, prev string) ([]Departure, error) {
	today, err := c.callsOn(ctx, stopID, day, 0)
	if err != nil {
		return nil, err
	}
	// Only yesterday's rows that reach past midnight can still be caught.
	borrowed, err := c.callsOn(ctx, stopID, prev, 86400)
	if err != nil {
		return nil, err
	}
	for i := range borrowed {
		borrowed[i].AfterMidnight = true
	}

	// Stable, because Scheduled is not unique: two routes can call in the same
	// second. callsOn orders each day totally, so a tie keeps that order, and
	// today's rows come before yesterday's at the same second because they are
	// first in the slice.
	out := append(today, borrowed...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Scheduled < out[j].Scheduled })
	return out, nil
}

// callsOn returns the calls at a stop on one date, with shift subtracted from
// each. A shift of 86400 also drops everything that does not reach past
// midnight, because nothing earlier is still catchable the next day.
func (c *Cache) callsOn(ctx context.Context, stopID, date string, shift int) ([]Departure, error) {
	services, err := c.ActiveServices(ctx, date)
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, nil
	}
	svc, svcArgs := in(services)

	q := `
		SELECT st.trip_id, r.short_name, t.headsign, st.arr, r.color
		  FROM stop_times st
		  JOIN trips t ON t.trip_id = st.trip_id
		  JOIN routes r ON r.route_id = t.route_id
		 WHERE st.stop_id = ? AND t.service_id IN (` + svc + `)
		   AND st.arr >= ?
		 -- Total, on purpose, and the second key is the order the export wrote
		 -- the rows in. Two routes can call in the same second and nothing in
		 -- the data says which is drawn first, so the file's own order does. The
		 -- sort that merges the two days is stable and keeps it.
		 --
		 -- Ordering a tie by route number instead is tidier and invents an order
		 -- the data does not have. This one is the only order the file itself
		 -- carries, so anything reading this cache can reproduce it.
		 ORDER BY st.arr, st.rowid`

	args := append([]any{stopID}, svcArgs...)
	args = append(args, shift)

	return collect(ctx, c, "departures at "+stopID+" on "+date, q, args,
		func(rows *sql.Rows) (Departure, error) {
			var d Departure
			if err := rows.Scan(&d.Trip, &d.Route, &d.Headsign, &d.Scheduled, &d.Colour); err != nil {
				return d, err
			}
			d.Scheduled -= shift
			return d, nil
		})
}

// collect runs q and builds a slice from its rows. Every query here has this
// same shape, and writing it once keeps one error context per query rather
// than the same sentence three times inside each.
func collect[T any](ctx context.Context, c *Cache, what, q string, args []any, scan func(*sql.Rows) (T, error)) ([]T, error) {
	rows, err := c.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	defer rows.Close() //nolint:errcheck // rows.Err below reports what went wrong

	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", what, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	return out, nil
}

// in returns the placeholder list for an IN clause together with the arguments
// that fill it. They come back from one call because they must agree. Built
// apart, they drifted once already and bound a route type where a date
// belonged, which returned no rows and looked like a day with no service.
func in[T any](vs []T) (string, []any) {
	args := make([]any, len(vs))
	for i, v := range vs {
		args[i] = v
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(vs)), ","), args
}

// beginning builds a LIKE pattern matching query at the start of a field.
func beginning(query string) string {
	return escapeLike(strings.ToLower(query)) + "%"
}

// anywhere builds a LIKE pattern matching query anywhere in a field. The
// wildcards are escaped, so a person typing % searches for a per cent sign
// rather than for everything.
func anywhere(query string) string {
	return "%" + escapeLike(strings.ToLower(query)) + "%"
}

func escapeLike(query string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(query)
}

func weekdayColumn(date string) (string, error) {
	day, err := time.Parse("20060102", date)
	if err != nil {
		return "", fmt.Errorf("date %q is not written YYYYMMDD: %w", date, err)
	}
	return [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}[day.Weekday()], nil
}
