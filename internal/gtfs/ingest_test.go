package gtfs

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A feed is a directory of GTFS files a test can overwrite one at a time.
//
// Every file starts minimal and valid and they agree with each other, so a test
// about one column writes one file and inherits a feed that ingests. The files
// are the real thing: the export's own column names, in the order it writes
// them, with a row that could have come out of it.
type feed struct {
	t   *testing.T
	dir string
}

var minimal = map[string]string{
	"routes.txt": "route_id,route_short_name,route_long_name,route_type,route_color,route_text_color,route_sort_order\n" +
		"R1,7,Carleton,3,D62D20,FFFFFF,7\n",
	"stops.txt": "stop_id,stop_code,stop_name,stop_lat,stop_lon,location_type,parent_station,platform_code\n" +
		"S1,1902,BANK / SOMERSET,45.4133,-75.6900,0,,\n",
	"trips.txt": "trip_id,route_id,service_id,trip_headsign,direction_id\n" +
		"t1,R1,SV1,SOMERSET,0\n",
	"calendar.txt": "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\n" +
		"SV1,1,1,1,1,1,0,0,20260901,20261231\n",
	"calendar_dates.txt": "service_id,date,exception_type\n" +
		"SV1,20260907,2\n",
	"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence\n" +
		"t1,10:00:00,10:00:30,S1,1\n",
}

func newFeed(t *testing.T) *feed {
	t.Helper()
	f := &feed{t: t, dir: t.TempDir()}
	for name, body := range minimal {
		f.write(name, body)
	}
	return f
}

func (f *feed) write(name, body string) {
	f.t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), []byte(body), 0o600); err != nil {
		f.t.Fatalf("writing %s: %v", name, err)
	}
}

func (f *feed) remove(name string) {
	f.t.Helper()
	if err := os.Remove(filepath.Join(f.dir, name)); err != nil {
		f.t.Fatalf("removing %s: %v", name, err)
	}
}

// ingest builds a cache from the feed and returns where it went. The error is
// the test's to check, so it is returned rather than reported here.
func (f *feed) ingest() (string, error) {
	f.t.Helper()
	path := filepath.Join(f.t.TempDir(), "gtfs.db")
	return path, Ingest(context.Background(), f.dir, path, io.Discard)
}

// built ingests and fails the test if that did not work.
func (f *feed) built() string {
	f.t.Helper()
	path, err := f.ingest()
	if err != nil {
		f.t.Fatalf("Ingest: %v", err)
	}
	return path
}

// asks runs one query against a built cache and returns the one value it names.
func asks(t *testing.T, path, query string) string {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer db.Close() //nolint:errcheck // read-only, and the query below reports
	var got string
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return got
}

func TestAnIngestFillsEveryTableTheQueryLayerReads(t *testing.T) {
	t.Parallel()
	path := newFeed(t).built()

	// Named one by one, because a table left empty is a screen that draws
	// nothing rather than an error anybody sees.
	for _, table := range []string{"routes", "stops", "trips", "calendar", "calendar_dates", "stop_times"} {
		if got := asks(t, path, "SELECT COUNT(*) FROM "+table); got != "1" {
			t.Errorf("%s holds %s rows, want 1", table, got)
		}
	}
}

func TestAByteOrderMarkDoesNotEmptyTheFirstColumn(t *testing.T) {
	t.Parallel()
	// The export ships every file with a UTF-8 byte order mark, and
	// encoding/csv leaves it on the first header. Left on, the column is named
	// "\ufeffroute_id", nothing asks for that, and every route_id comes back
	// empty: a cache that ingests cleanly and holds no usable route.
	f := newFeed(t)
	f.write("routes.txt", "\ufeff"+minimal["routes.txt"])

	if got := asks(t, f.built(), "SELECT route_id FROM routes"); got != "R1" {
		t.Errorf("route_id is %q, want R1", got)
	}
}

func TestColumnsAreReadByNameSoTheirOrderDoesNotMatter(t *testing.T) {
	t.Parallel()
	f := newFeed(t)
	f.write("stops.txt", "platform_code,stop_name,stop_id,stop_code,location_type,parent_station,stop_lon,stop_lat\n"+
		"C,BAYVIEW C,S9,3060,0,3060_stn,-75.7,45.4\n")

	path := f.built()
	for _, tc := range []struct{ query, want string }{
		{"SELECT stop_id FROM stops", "S9"},
		{"SELECT name FROM stops", "BAYVIEW C"},
		{"SELECT platform FROM stops", "C"},
		{"SELECT stop_code FROM stops", "3060"},
	} {
		if got := asks(t, path, tc.query); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestATimePastMidnightSurvivesTheIngestAsWritten(t *testing.T) {
	t.Parallel()
	// The feed reaches 28:xx, and those trips belong to the day they started.
	// Wrapping one here would move the 01:10 bus to the start of the day, which
	// is the bus you most want at one in the morning.
	f := newFeed(t)
	f.write("stop_times.txt", "trip_id,arrival_time,departure_time,stop_id,stop_sequence\n"+
		"t1,25:10:00,25:11:30,S1,1\n")

	path := f.built()
	if got := asks(t, path, "SELECT arr FROM stop_times"); got != "90600" {
		t.Errorf("arr is %s, want 90600", got)
	}
	if got := asks(t, path, "SELECT dep FROM stop_times"); got != "90690" {
		t.Errorf("dep is %s, want 90690", got)
	}
}

func TestADepartureThatIsNotGivenFallsBackToTheArrival(t *testing.T) {
	t.Parallel()
	f := newFeed(t)
	f.write("stop_times.txt", "trip_id,arrival_time,departure_time,stop_id,stop_sequence\n"+
		"t1,10:00:00,,S1,1\n")

	if got := asks(t, f.built(), "SELECT dep FROM stop_times"); got != "36000" {
		t.Errorf("dep is %s, want the arrival at 36000", got)
	}
}

func TestAnAbsentOptionalColumnReadsAsEmptyRatherThanFailing(t *testing.T) {
	t.Parallel()
	// platform_code is absent from most agencies' exports, and rare within this
	// one. Its absence must not cost the whole schedule.
	f := newFeed(t)
	f.write("stops.txt", "stop_id,stop_code,stop_name,stop_lat,stop_lon,location_type,parent_station\n"+
		"S1,1902,BANK / SOMERSET,45.0,-75.0,0,\n")

	if got := asks(t, f.built(), "SELECT platform FROM stops"); got != "" {
		t.Errorf("platform is %q, want empty", got)
	}
}

func TestTheSchemaVersionIsStampedSoALayoutChangeForcesARebuild(t *testing.T) {
	t.Parallel()
	got := asks(t, newFeed(t).built(), "SELECT value FROM meta WHERE key = 'schema_version'")
	if got != SchemaVersion {
		t.Errorf("schema_version is %q, want %q", got, SchemaVersion)
	}
}

func TestAColumnStopTimesCannotDoWithoutNamesItselfAndItsFile(t *testing.T) {
	t.Parallel()
	f := newFeed(t)
	f.write("stop_times.txt", "trip_id,arrival_time,stop_id,stop_sequence\n"+
		"t1,10:00:00,S1,1\n")

	_, err := f.ingest()
	if err == nil {
		t.Fatal("a feed with no departure_time column ingested")
	}
	if !strings.Contains(err.Error(), "departure_time") || !strings.Contains(err.Error(), "stop_times") {
		t.Errorf("error is %q, and it has to say which column is missing from which file", err)
	}
}

func TestAMissingFileNamesTheFile(t *testing.T) {
	t.Parallel()
	f := newFeed(t)
	f.remove("trips.txt")

	_, err := f.ingest()
	if err == nil {
		t.Fatal("a feed with no trips.txt ingested")
	}
	if !strings.Contains(err.Error(), "trips.txt") {
		t.Errorf("error is %q, and it has to name the file", err)
	}
}

func TestAFileWithNothingInItNamesItself(t *testing.T) {
	t.Parallel()
	// A truncated download can still unzip, and an empty file opens and reads
	// like any other. The operating system says nothing about which file that
	// was, because opening it worked, so this message is the only place the name
	// can come from.
	f := newFeed(t)
	f.write("stop_times.txt", "")

	_, err := f.ingest()
	if err == nil {
		t.Fatal("a feed with an empty stop_times.txt ingested")
	}
	if !strings.Contains(err.Error(), "stop_times.txt") {
		t.Errorf("error is %q, and it has to name the file that is empty", err)
	}
}

func TestIngestingTwiceBuildsTheCacheAgainRatherThanAddingToIt(t *testing.T) {
	t.Parallel()
	// The rows carry no constraint that would catch a double load: stop_times
	// has no primary key, so a second ingest into one file would quietly hold
	// every departure twice and every board would draw each bus two times.
	f := newFeed(t)
	path := filepath.Join(t.TempDir(), "gtfs.db")
	for i := range 2 {
		if err := Ingest(context.Background(), f.dir, path, io.Discard); err != nil {
			t.Fatalf("ingest %d: %v", i+1, err)
		}
	}

	if got := asks(t, path, "SELECT COUNT(*) FROM stop_times"); got != "1" {
		t.Errorf("stop_times holds %s rows after two ingests, want 1", got)
	}
}

func TestTheIndexesEveryQueryLeansOnAreBuilt(t *testing.T) {
	t.Parallel()
	// They are applied after the rows, so they are the part of the layout an
	// ingest can leave out and still look like it worked. A board with no index
	// behind it answers correctly and takes a second to do it.
	path := newFeed(t).built()

	for _, name := range []string{"idx_st_stop", "idx_st_trip", "idx_trips_route", "idx_trips_service", "idx_cd_date"} {
		got := asks(t, path, "SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = '"+name+"'")
		if got != "1" {
			t.Errorf("%s is not in the cache", name)
		}
	}
}

func TestParsingAScheduleTime(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   string
		want int64
		ok   bool
	}{
		{"a normal time", "10:05:30", 10*3600 + 5*60 + 30, true},
		{"midnight", "00:00:00", 0, true},
		{"past midnight, unwrapped", "25:10:00", 25*3600 + 600, true},
		{"the latest the feed goes", "28:45:00", 28*3600 + 45*60, true},
		{"padded with spaces", " 10:05:00 ", 10*3600 + 5*60, true},
		{"no seconds field", "10:05", 10*3600 + 5*60, true},
		{"seconds that are not a number", "10:05:xx", 10*3600 + 5*60, true},
		{"empty", "", 0, false},
		{"hours alone", "10", 0, false},
		{"not a time at all", "abc", 0, false},
		{"minutes that are not a number", "10:xx:00", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := parseHMS(tc.in)
			if ok != tc.ok || (ok && got != tc.want) {
				t.Errorf("parseHMS(%q) = %d, %v; want %d, %v", tc.in, got, ok, tc.want, tc.ok)
			}
		})
	}
}
