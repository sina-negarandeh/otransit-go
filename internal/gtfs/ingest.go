package gtfs

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	"github.com/sina-negarandeh/otransit-go/internal/cache"

	// The driver. This package writes the file that cache reads, so it needs
	// the same one.
	_ "modernc.org/sqlite"
)

// SchemaVersion is stamped into every cache this package builds. An update
// reads it back and rebuilds when it does not match, because a cache built to
// an older layout is a set of columns the queries no longer ask for.
//
// The two implementations of this program share one cache file, so this number
// belongs to the layout and not to either of them.
const SchemaVersion = "2"

// Ingest builds a cache at path from the GTFS files in dir.
//
// The cache is built, never added to: the file at path is removed first. The
// rows carry almost no constraints, so a second load into one file would hold
// every departure twice and every board would draw each bus two times.
func Ingest(ctx context.Context, dir, path string, out io.Writer) (err error) {
	say := reporter{out}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("creating cache %s: %w", path, err)
	}
	// The pragmas below belong to the connection that does the work, so there is
	// one connection.
	db.SetMaxOpenConns(1)
	defer func() { err = errors.Join(err, db.Close()) }()

	if _, err := db.ExecContext(ctx, pragmas+cache.Schema); err != nil {
		return fmt.Errorf("preparing cache %s: %w", path, err)
	}

	for _, t := range tables {
		n, err := t.load(ctx, db, dir)
		if err != nil {
			return err
		}
		say.line("  %s %d", strings.TrimSuffix(t.file, ".txt"), n)
	}
	if err := stopTimes(ctx, db, dir, say); err != nil {
		return err
	}

	// After the rows, not before: maintaining five indexes across six million
	// inserts costs more than building them once at the end.
	say.line("  building indexes...")
	if _, err := db.ExecContext(ctx, cache.Indexes); err != nil {
		return fmt.Errorf("indexing cache %s: %w", path, err)
	}
	if err := setMeta(ctx, db, "schema_version", SchemaVersion); err != nil {
		return err
	}
	if err := setMeta(ctx, db, "ingested_from", dir); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "PRAGMA optimize")
	return err
}

// pragmas make the writes as cheap as they can be made. The cache is a build
// artifact: an ingest that dies part way through loses a file that one command
// rebuilds, and never the live cache, because the build happens beside it.
const pragmas = `
PRAGMA journal_mode = OFF;
PRAGMA synchronous = OFF;
PRAGMA cache_size = -200000;
PRAGMA temp_store = MEMORY;
`

// A table is one file of the export loaded into one table of the cache.
type table struct {
	file, insert string
	// bind turns one row into the parameters of insert.
	bind func(header, []string) []any
}

// The files that load the same way. stop_times is not among them: it is a
// thousand times the size of all of these together and has its own loop.
//
// Every default here is the value the GTFS specification gives when the column
// is absent, except route_type: a feed that does not say is a bus feed.
var tables = []table{{
	file:   "routes.txt",
	insert: `INSERT OR REPLACE INTO routes VALUES (?,?,?,?,?,?,?)`,
	bind: func(h header, rec []string) []any {
		return []any{
			h.text(rec, "route_id"), h.text(rec, "route_short_name"), h.text(rec, "route_long_name"),
			h.num(rec, "route_type", 3), h.text(rec, "route_color"), h.text(rec, "route_text_color"),
			h.num(rec, "route_sort_order", 0),
		}
	},
}, {
	file:   "stops.txt",
	insert: `INSERT OR REPLACE INTO stops VALUES (?,?,?,?,?,?,?,?)`,
	bind: func(h header, rec []string) []any {
		return []any{
			h.text(rec, "stop_id"), h.text(rec, "stop_code"), h.text(rec, "stop_name"),
			h.real(rec, "stop_lat"), h.real(rec, "stop_lon"),
			h.num(rec, "location_type", 0), h.text(rec, "parent_station"),
			h.text(rec, "platform_code"),
		}
	},
}, {
	file:   "trips.txt",
	insert: `INSERT OR REPLACE INTO trips VALUES (?,?,?,?,?)`,
	bind: func(h header, rec []string) []any {
		return []any{
			h.text(rec, "trip_id"), h.text(rec, "route_id"), h.text(rec, "service_id"),
			h.text(rec, "trip_headsign"), h.num(rec, "direction_id", 0),
		}
	},
}, {
	file:   "calendar.txt",
	insert: `INSERT OR REPLACE INTO calendar VALUES (?,?,?,?,?,?,?,?,?,?)`,
	bind: func(h header, rec []string) []any {
		return []any{
			h.text(rec, "service_id"),
			h.num(rec, "monday", 0), h.num(rec, "tuesday", 0), h.num(rec, "wednesday", 0),
			h.num(rec, "thursday", 0), h.num(rec, "friday", 0), h.num(rec, "saturday", 0),
			h.num(rec, "sunday", 0),
			h.text(rec, "start_date"), h.text(rec, "end_date"),
		}
	},
}, {
	file: "calendar_dates.txt",
	// No key to replace on, and none is wanted: a service can be added on one
	// date and removed on another.
	insert: `INSERT INTO calendar_dates VALUES (?,?,?)`,
	bind: func(h header, rec []string) []any {
		return []any{h.text(rec, "service_id"), h.text(rec, "date"), h.num(rec, "exception_type", 1)}
	},
}}

// load streams one file into one table inside a single transaction, and returns
// how many rows went in. The count is reported, because a file that changed
// shape and a city with nothing running look the same on the screen.
func (t table) load(ctx context.Context, db *sql.DB, dir string) (n int64, err error) {
	f, r, h, err := open(dir, t.file)
	if err != nil {
		return 0, err
	}
	defer f.Close() //nolint:errcheck // read-only, and the read errors are reported already

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // a commit that worked makes this a no-op, and one that did not is the error already returned

	stmt, err := tx.PrepareContext(ctx, t.insert)
	if err != nil {
		return 0, err
	}
	defer stmt.Close() //nolint:errcheck // the statement dies with the transaction either way

	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return n, fmt.Errorf("reading %s: %w", t.file, err)
		}
		if _, err := stmt.ExecContext(ctx, t.bind(h, rec)...); err != nil {
			return n, fmt.Errorf("loading %s: %w", t.file, err)
		}
		n++
	}
	return n, tx.Commit()
}

// The columns stop_times cannot be loaded without, and where each one sits.
const (
	colTrip = iota
	colArr
	colDep
	colStop
	colSeq
)

var stopTimeColumns = [...]string{
	colTrip: "trip_id",
	colArr:  "arrival_time",
	colDep:  "departure_time",
	colStop: "stop_id",
	colSeq:  "stop_sequence",
}

// stopTimes loads the six million rows of stop_times.
//
// It has a loop of its own because of that number: the columns are found once
// instead of once per row, the reader hands back the same slice each time, and
// the progress line says how far in it is. Everything else in the export goes
// through load above.
func stopTimes(ctx context.Context, db *sql.DB, dir string, say reporter) error {
	f, r, h, err := open(dir, "stop_times.txt")
	if err != nil {
		return err
	}
	defer f.Close() //nolint:errcheck // read-only, and the read errors are reported already

	var at [len(stopTimeColumns)]int
	for k, name := range stopTimeColumns {
		i, ok := h[name]
		if !ok {
			return fmt.Errorf("stop_times.txt has no %s column", name)
		}
		at[k] = i
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // a commit that worked makes this a no-op, and one that did not is the error already returned

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO stop_times VALUES (?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close() //nolint:errcheck // the statement dies with the transaction either way

	var rec []string
	field := func(k int) string {
		if i := at[k]; i < len(rec) {
			return rec[i]
		}
		return ""
	}
	var n int64
	for {
		rec, err = r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("reading stop_times.txt: %w", err)
		}
		arr := seconds(field(colArr))
		dep := seconds(field(colDep))
		if dep == nil {
			// A stop the export gives an arrival for and no departure. The bus
			// does not wait there, and a row with no time is a row no board can
			// draw at all.
			dep = arr
		}
		if _, err := stmt.ExecContext(ctx, field(colTrip), field(colStop),
			integer(field(colSeq), 0), arr, dep); err != nil {
			return fmt.Errorf("loading stop_times.txt: %w", err)
		}
		n++
		if n%1_000_000 == 0 {
			say.line("  stop_times %dM...", n/1_000_000)
		}
	}
	say.line("  stop_times %d", n)
	return tx.Commit()
}

// open reads a file's header and leaves the reader on its first row.
func open(dir, file string) (*os.File, *csv.Reader, header, error) {
	f, err := os.Open(filepath.Join(dir, file))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("opening %s: %w", file, err)
	}
	r := csv.NewReader(f)
	// The export writes rows with fewer fields than its header, and one short
	// row is not a reason to lose a day's schedule.
	r.FieldsPerRecord = -1
	r.ReuseRecord = true

	rec, err := r.Read()
	if err != nil {
		f.Close() //nolint:errcheck // the read already failed, and that is the error to report
		return nil, nil, nil, fmt.Errorf("reading the header of %s: %w", file, err)
	}
	h := make(header, len(rec))
	for i, name := range rec {
		h[strings.TrimFunc(name, invisible)] = i
	}
	return f, r, h, nil
}

// invisible is a character that is no part of a column name: a space, or a mark
// the file carries for a reason of its own.
//
// The export writes a byte order mark at the head of every file and encoding/csv
// leaves it on. Left on, the first column of each file is named with an
// invisible character in front of it, nothing asks for that name, and every
// value in the column reads as empty: a cache that ingests without complaint and
// holds no usable route.
func invisible(r rune) bool { return unicode.IsSpace(r) || !unicode.IsGraphic(r) }

// header is where each column of one file sits.
//
// Columns are read by name, because the export is free to reorder them. A
// column that is not there reads as empty rather than as an error: platform_code
// is absent from most exports and rare within this one.
type header map[string]int

func (h header) text(rec []string, name string) string {
	if i, ok := h[name]; ok && i < len(rec) {
		return rec[i]
	}
	return ""
}

func (h header) num(rec []string, name string, absent int64) int64 {
	return integer(h.text(rec, name), absent)
}

func (h header) real(rec []string, name string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(h.text(rec, name)), 64)
	if err != nil {
		return 0
	}
	return f
}

// integer is a whole number the export wrote, and absent when it wrote
// something else there. Every count in the feed is optional in this sense: the
// specification gives a default for each one, and a row is never dropped over a
// field a screen does not draw.
func integer(s string, absent int64) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return absent
	}
	return n
}

// seconds is a schedule time as the cache stores it, and nil for a row that
// gives none. A time that cannot be read is not a time at the start of the day.
func seconds(s string) any {
	if n, ok := parseHMS(s); ok {
		return n
	}
	return nil
}

// parseHMS reads "HH:MM:SS" as seconds after midnight of the service day.
//
// The hour reaches 28. A trip that runs past midnight belongs to the day it
// started on, so wrapping the hour here would move the 01:10 bus to the start of
// the day, and that is the bus you most want at one in the morning. The seconds
// field is optional, because a feed is allowed to write "10:05".
func parseHMS(s string) (int64, bool) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) < 2 {
		return 0, false
	}
	h, err := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
	if err != nil {
		return 0, false
	}
	m, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return 0, false
	}
	var sec int64
	if len(parts) > 2 {
		// Seconds nobody can read are seconds nobody needs: the minute is the
		// unit every screen draws.
		sec = integer(parts[2], 0)
	}
	return h*3600 + m*60 + sec, true
}
