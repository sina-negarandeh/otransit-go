// Package gtfs builds the schedule cache from the export OC Transpo publishes.
//
// The export is a zip of CSV files, about 109 MB of them, republished daily.
// This package downloads it, unpacks it, and writes it into the SQLite file the
// query layer reads.
//
// The cache is a build artifact and never user state. Deleting it costs one
// update, which is how a person recovers from a bad ingest, and it is why pinned
// stops live beside the config file instead of in here.
package gtfs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// A reporter is where an update says what it is doing.
//
// One place says what these lines are worth, rather than a checked write at
// every one of them: the progress of an update is not its result. An update that
// rebuilt the schedule has not failed because the far end of stdout went away,
// and a line nobody can read is worth nothing to anybody.
type reporter struct{ w io.Writer }

func (r reporter) line(format string, args ...any) {
	fmt.Fprintf(r.w, format+"\n", args...) //nolint:errcheck // a progress line that cannot be written changes nothing that matters
}

// mid writes where a line is rewritten in place, and ends no line.
func (r reporter) mid(format string, args ...any) {
	fmt.Fprintf(r.w, format, args...) //nolint:errcheck // see line above
}

// Update downloads the published export and rebuilds the cache at path.
//
// The new cache is built beside the old one and moved into place at the end. A
// download that dies half way, an archive that holds the wrong thing, or an
// ingest that fails on the last file all leave the schedule that was already
// there. A rename within one directory is atomic, which is the whole reason for
// the temporary name.
func Update(ctx context.Context, path string, now time.Time, out io.Writer) error {
	say := reporter{out}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	archive := filepath.Join(dir, "GTFSExport.zip")
	feed := filepath.Join(dir, "gtfs-extract")
	building := path + ".new"
	// What a failed run left behind is not evidence about this one.
	if err := clear(archive, feed, building); err != nil {
		return err
	}

	say.line("checking %s", URL)
	got, err := download(ctx, URL, archive, reusableETag(ctx, path, say), say)
	if err != nil {
		return err
	}
	if got == nil {
		say.line("the schedule is already current (304 Not Modified)")
		return nil
	}

	say.line("  extracting...")
	if err := extract(archive, feed); err != nil {
		return err
	}
	if err := Ingest(ctx, feed, building, out); err != nil {
		return err
	}
	if err := stamp(ctx, building, got.etag, now); err != nil {
		return err
	}
	if err := os.Rename(building, path); err != nil {
		return err
	}

	if err := clear(archive, feed); err != nil {
		return err
	}
	say.line("the schedule is updated (%.0f MB)", float64(got.bytes)/megabyte)
	return nil
}

// clear removes files and directories that may or may not be there.
func clear(paths ...string) error {
	for _, p := range paths {
		if err := os.RemoveAll(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// reusableETag is the etag to ask the feed about, and empty when there is no
// question worth asking.
//
// An etag is only worth offering when the cache it came from is one this code
// could still read. Offering one from an older layout invites a 304, and a 304
// is an instruction to keep a cache whose columns have moved. A rebuild that
// costs 109 MB says out loud why it is happening.
//
// Nothing here is an error. A cache that is not there, cannot be opened, or
// answers nothing is a cache with no etag, and the next step is the same in
// every one of those cases: download the export.
func reusableETag(ctx context.Context, path string, say reporter) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return ""
	}
	defer db.Close() //nolint:errcheck // nothing was written, and a failure to close changes no answer

	if getMeta(ctx, db, "schema_version") != SchemaVersion {
		say.line("  the cache was built to an older layout, so it is rebuilt in full")
		return ""
	}
	return getMeta(ctx, db, "etag")
}

// stamp records what the cache was built from, in the cache.
//
// The etag is the only one of these that anything reads back. The date is
// provenance for a person looking at the file, so it is the local date rather
// than an instant.
func stamp(ctx context.Context, path, etag string, now time.Time) (err error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()

	if etag != "" {
		if err := setMeta(ctx, db, "etag", etag); err != nil {
			return err
		}
	}
	return setMeta(ctx, db, "ingested_on", now.Format(time.DateOnly))
}

// The cache's key/value table. The keys this program writes:
//
//	etag            the export the cache holds, for the next conditional request
//	schema_version  the layout, so an update can tell that it must rebuild
//	ingested_from   the directory it was built from
//	ingested_on     the day it was built
func setMeta(ctx context.Context, db *sql.DB, key, value string) error {
	if _, err := db.ExecContext(ctx, `INSERT OR REPLACE INTO meta VALUES (?,?)`, key, value); err != nil {
		return fmt.Errorf("recording %s: %w", key, err)
	}
	return nil
}

// getMeta is what the cache says, and empty when it says nothing. A cache with
// no answer and a cache that cannot be read are the same news to every caller
// here, so neither is reported as an error.
func getMeta(ctx context.Context, db *sql.DB, key string) string {
	var value string
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&value); err != nil {
		return ""
	}
	return value
}
