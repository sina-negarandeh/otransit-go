package gtfs

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// No test here reaches the network. What the transfer decides is a function of
// the status and the length, and those are the parts that have been wrong.

func TestWhatAStatusMeansForTheBodyThatFollows(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		status int
		want   outcome
	}{
		// This one shipped in the other implementation and would ship here for
		// a different reason: net/http hands back a 304 as an ordinary response
		// with an empty body, so a download writes a zero-byte file and the
		// failure arrives much later, wearing the zip reader's words.
		{"not modified", 304, notModified},
		{"the feed", 200, body},
		{"a partial body", 206, body},
		// net/http returns no error for a status the server calls an error
		// either, so this is the only place that can tell them apart.
		{"not there", 404, refused},
		{"the service is down", 503, refused},
		{"a redirect the client did not follow", 302, body},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classify(tc.status); got != tc.want {
				t.Errorf("classify(%d) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestAnEmptyBodyIsRefusedWhereTheMessageCanSayWhy(t *testing.T) {
	t.Parallel()
	// A feed is never legitimately empty. Refusing it here costs one sentence.
	// Letting it through costs "Could not find EOCD" from the zip reader.
	err := checkBody(0)
	if err == nil {
		t.Fatal("an empty body was accepted as a feed")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error is %q, and it has to say the body was empty", err)
	}
	if err := checkBody(1); err != nil {
		t.Errorf("checkBody(1) = %v, want a body of one byte to be allowed through", err)
	}
}

// archive writes a zip holding the named entries and returns where it went.
func archive(t *testing.T, entries map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "GTFSExport.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the archive: %v", err)
	}
	w := zip.NewWriter(f)
	for name, content := range entries {
		e, err := w.Create(name)
		if err != nil {
			t.Fatalf("adding %s: %v", name, err)
		}
		if _, err := e.Write([]byte(content)); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the archive: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("closing the file: %v", err)
	}
	return path
}

func TestAnArchiveUnpacksFlatWhereTheIngestLooksForIt(t *testing.T) {
	t.Parallel()
	// The export has wrapped its files in a directory before. The ingest opens
	// them by name in one directory, so the shape inside the archive is not
	// allowed to decide whether the ingest finds anything.
	dir := filepath.Join(t.TempDir(), "gtfs")
	if err := extract(archive(t, map[string]string{
		"GTFSExport/stop_times.txt": "trip_id\n",
		"routes.txt":                "route_id\n",
	}), dir); err != nil {
		t.Fatalf("extract: %v", err)
	}

	for _, name := range []string{"stop_times.txt", "routes.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s is not in the directory: %v", name, err)
		}
	}
}

func TestAnEntryThatWouldWriteOutsideTheDirectoryCannot(t *testing.T) {
	t.Parallel()
	// An archive is data from the network and its entry names are part of that
	// data. This one is not a threat anybody expects from OC Transpo, and it
	// costs one line to make impossible rather than unlikely.
	parent := t.TempDir()
	dir := filepath.Join(parent, "gtfs")
	if err := extract(archive(t, map[string]string{
		"../escaped.txt": "no",
		"stop_times.txt": "trip_id\n",
	}), dir); err != nil {
		t.Fatalf("extract: %v", err)
	}

	if _, err := os.Stat(filepath.Join(parent, "escaped.txt")); err == nil {
		t.Error("an entry wrote above the directory it was unpacked into")
	}
	if _, err := os.Stat(filepath.Join(dir, "escaped.txt")); err != nil {
		t.Errorf("the entry did not land inside the directory either: %v", err)
	}
}

func TestAnArchiveWithNoStopTimesIsNotAFeed(t *testing.T) {
	t.Parallel()
	// A zip that unpacks cleanly and holds the wrong thing is the failure to
	// name here. One table later, it reads as a city with no departures.
	err := extract(archive(t, map[string]string{"readme.html": "hello"}), filepath.Join(t.TempDir(), "gtfs"))
	if err == nil {
		t.Fatal("an archive with no stop_times.txt was accepted")
	}
	if !strings.Contains(err.Error(), "stop_times.txt") {
		t.Errorf("error is %q, and it has to name what was missing", err)
	}
}

func TestExtractClearsWhatAnEarlierRunLeftBehind(t *testing.T) {
	t.Parallel()
	// A file the current export no longer ships would otherwise be ingested
	// again from whichever run last wrote it.
	dir := filepath.Join(t.TempDir(), "gtfs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the directory: %v", err)
	}
	stale := filepath.Join(dir, "shapes.txt")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatalf("writing the stale file: %v", err)
	}

	if err := extract(archive(t, map[string]string{"stop_times.txt": "trip_id\n"}), dir); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if _, err := os.Stat(stale); err == nil {
		t.Error("a file from an earlier run is still there")
	}
}

func TestAnEtagIsOnlyOfferedWhenTheCacheMatchesTheLayoutThisCodeBuilds(t *testing.T) {
	t.Parallel()
	// The point of the etag is to be told "you already have this" and stop. A
	// cache built to an older layout must not be allowed to stop the rebuild:
	// it would answer 304 and leave the program reading columns that moved.
	for _, tc := range []struct {
		name    string
		version string
		want    string
	}{
		{"the layout this code builds", SchemaVersion, "0xABC"},
		{"an older layout", "1", ""},
		{"no version at all", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := newFeed(t).built()
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatalf("opening the cache: %v", err)
			}
			if _, err := db.Exec(`INSERT OR REPLACE INTO meta VALUES ('etag', '0xABC')`); err != nil {
				t.Fatalf("stamping the etag: %v", err)
			}
			if _, err := db.Exec(`INSERT OR REPLACE INTO meta VALUES ('schema_version', ?)`, tc.version); err != nil {
				t.Fatalf("stamping the version: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("closing the cache: %v", err)
			}

			var said bytes.Buffer
			if got := reusableETag(context.Background(), path, reporter{&said}); got != tc.want {
				t.Errorf("etag offered is %q, want %q", got, tc.want)
			}
			// A rebuild that costs 109 MB says why it is happening.
			if tc.want == "" && tc.version != "" && said.Len() == 0 {
				t.Error("the cache was rebuilt from scratch and nothing said why")
			}
		})
	}
}

func TestACacheThatIsNotThereOffersNoEtagAndNoError(t *testing.T) {
	t.Parallel()
	// The first update on a new machine. There is nothing to ask about, and
	// nothing has gone wrong.
	var said bytes.Buffer
	if got := reusableETag(context.Background(), filepath.Join(t.TempDir(), "none.db"), reporter{&said}); got != "" {
		t.Errorf("etag offered is %q, want none", got)
	}
	if said.Len() != 0 {
		t.Errorf("a first update said %q, and it has nothing to report", said.String())
	}
}
