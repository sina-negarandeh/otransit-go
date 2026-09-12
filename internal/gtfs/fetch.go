package gtfs

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// URL is where OC Transpo publishes the schedule. It needs no key.
//
// The export is republished daily and the agency asks that it be taken daily,
// because a realtime trip id is only promised to match the same day's export.
const URL = "https://oct-gtfs-emasagcnfmcgeham.z01.azurefd.net/public-access/GTFSExport.zip"

// megabyte is what the progress lines count in. The export is about 109 of them.
const megabyte = 1 << 20

// An outcome is what a response says about the body that follows it.
type outcome int

const (
	// notModified: the copy we hold is the published one. There is no body.
	notModified outcome = iota + 1
	// body: the export follows.
	body
	// refused: the server calls this an error. The URL is compiled in, so a
	// refusal is this program's problem and not the network's.
	refused
)

// classify says what a status means before a byte is written to disk.
//
// net/http returns a response and no error for every status, so both surprises
// arrive here looking like success: a 304 is an ordinary response with an empty
// body, and so is a 404. A 304 written to disk is a zero-byte file, and the
// complaint arrives one step later in the words of the zip reader.
func classify(status int) outcome {
	switch {
	case status == http.StatusNotModified:
		return notModified
	case status >= 400:
		return refused
	}
	return body
}

// checkBody refuses a feed with nothing in it.
//
// An export is never legitimately empty, and this is the last place that can say
// so in its own words.
func checkBody(n int64) error {
	if n == 0 {
		return errors.New("the feed returned an empty body")
	}
	return nil
}

// A transfer is what arrived.
type transfer struct {
	// etag identifies the bytes, for the next conditional request. The server
	// computes it from the file, so a match means the same export rather than
	// the same day.
	etag  string
	bytes int64
}

// download fetches url into dest, and returns nil when the server answers that
// the copy the caller already has is the published one.
func download(ctx context.Context, url, dest, etag string, say reporter) (*transfer, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if etag != "" {
		// The whole point of storing it: an unchanged export costs one
		// round-trip instead of 109 MB.
		req.Header.Set("If-None-Match", etag)
	}

	// Long, because the body is 109 MB on a line the agency does not choose,
	// and bounded, because a transfer that stops moving must not hang a command
	// somebody is waiting on.
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching the feed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // a read-only body, and the copy below reports what it read

	switch classify(resp.StatusCode) {
	case notModified:
		return nil, nil
	case refused:
		return nil, fmt.Errorf("the feed answered %s", resp.Status)
	}

	f, err := os.Create(dest)
	if err != nil {
		return nil, fmt.Errorf("creating %s: %w", dest, err)
	}
	p := &progress{say: say, total: resp.ContentLength}
	n, err := io.Copy(f, io.TeeReader(resp.Body, p))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = checkBody(n)
	}
	if err != nil {
		// Nothing downstream should be able to find this file and mistake it
		// for a feed.
		if rmErr := os.Remove(dest); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return nil, errors.Join(err, rmErr)
		}
		return nil, err
	}
	say.line("\r  downloaded %.0f MB          ", float64(n)/megabyte)
	return &transfer{etag: resp.Header.Get("Etag"), bytes: n}, nil
}

// progress reports how far a download has got, every ten per cent, on one line
// that rewrites itself. A server that sends no length gets no line: a percentage
// of an unknown total is a number nobody can read.
type progress struct {
	say   reporter
	total int64
	done  int64
	last  int64
}

func (p *progress) Write(b []byte) (int, error) {
	p.done += int64(len(b))
	if p.total > 0 {
		if pct := p.done * 100 / p.total; pct != p.last && pct%10 == 0 {
			p.last = pct
			p.say.mid("\r  downloading %d%% (%.0f MB)", pct, float64(p.done)/megabyte)
		}
	}
	return len(b), nil
}

// extract unpacks the archive into dir, flat, and replaces whatever is there.
//
// Flat because the ingest opens the files by name in one directory, and the
// export has wrapped them in a directory of its own before now. Taking each
// entry's base name also settles what an entry called "../etc/passwd" can do:
// nothing, by construction rather than by a check that can be got round.
func extract(archive, dir string) error {
	// A file the current export no longer ships would otherwise be ingested
	// again from whichever run last wrote it.
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("reading the feed archive: %w", err)
	}
	defer r.Close() //nolint:errcheck // read-only, and every entry's own error is reported below

	for _, entry := range r.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		name := filepath.Base(filepath.Clean(entry.Name))
		if name == "." || name == string(filepath.Separator) {
			continue
		}
		if err := unpack(entry, filepath.Join(dir, name)); err != nil {
			return err
		}
	}

	// A zip that unpacks cleanly and holds something else is the failure to
	// name here. One table later it reads as a city with no departures.
	if _, err := os.Stat(filepath.Join(dir, "stop_times.txt")); err != nil {
		return fmt.Errorf("the archive holds no stop_times.txt, so it is not a GTFS feed: %w", err)
	}
	return nil
}

func unpack(entry *zip.File, path string) (err error) {
	in, err := entry.Open()
	if err != nil {
		return fmt.Errorf("reading %s from the archive: %w", entry.Name, err)
	}
	defer in.Close() //nolint:errcheck // read-only, and the copy below reports what it read

	out, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	defer func() { err = errors.Join(err, out.Close()) }()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
