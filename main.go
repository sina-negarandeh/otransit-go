// Command otransit is a terminal browser for OC Transpo schedules and live
// arrivals.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/gtfs"
	"github.com/sina-negarandeh/otransit-go/internal/live"
	"github.com/sina-negarandeh/otransit-go/internal/logo"
	"github.com/sina-negarandeh/otransit-go/internal/replay"
	"github.com/sina-negarandeh/otransit-go/internal/term"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "otransit:", err)
		os.Exit(1)
	}
}

const usage = `usage: otransit [cache]
       otransit update [cache]
       otransit ingest <gtfs-directory> [cache]
       otransit logo [width]
       otransit replay <directory> [semantic] [styles]
       otransit --version`

// version is this program's. The attribution below is the data's: the Open
// Government Licence asks for it wherever the data is used, and a line in a
// README does not cover a running binary.
const version = "0.1.0"

const attribution = `Contains information licensed under the Open Government Licence -
City of Ottawa. https://open.ottawa.ca/pages/open-data-licence

Not affiliated with, endorsed by, or sponsored by OC Transpo or the City of
Ottawa.`

// slice is the piece of a real cache that this repository can reach. It is a
// few days of one corner of the network, and it is what makes the program
// runnable here with no download at all.
const slice = "conformance/cache.db"

func run(ctx context.Context, args []string, out io.Writer) error {
	command, rest := "", args
	if len(args) > 0 {
		command, rest = args[0], args[1:]
	}

	switch command {
	case "replay":
		return replayCmd(ctx, rest, out)

	case "update":
		path, err := only(rest, cache())
		if err != nil {
			return err
		}
		return gtfs.Update(ctx, path, time.Now(), out)

	case "ingest":
		if len(rest) == 0 {
			return fmt.Errorf("ingest takes a directory of GTFS files\n%s", usage)
		}
		path, err := only(rest[1:], cache())
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(out, "ingesting %s into %s\n", rest[0], path); err != nil {
			return err
		}
		return gtfs.Ingest(ctx, rest[0], path, out)

	case "logo":
		// The mark on its own, so it draws the rule it stands on. A width
		// argument is here because a terminal that is not one says nothing
		// about its size, and the mark has two forms that depend on it.
		width, err := only(rest, strconv.Itoa(term.Columns(os.Stdout)))
		if err != nil {
			return err
		}
		cells, err := strconv.Atoi(width)
		if err != nil {
			return fmt.Errorf("%q is not a width\n%s", width, usage)
		}
		return logo.PrintAlone(out, cells)

	case "--version", "-V":
		// The attribution is part of the answer, so a write that failed is a
		// command that failed: the licence asks for the line, not for an attempt.
		_, err := fmt.Fprintf(out, "otransit %s\n\n%s\n", version, attribution)
		return err

	case "-h", "--help":
		_, err := fmt.Fprintln(out, usage)
		return err
	}

	path, err := only(args, browsable())
	if err != nil {
		return err
	}
	return browse(ctx, path)
}

// only is the one optional cache path a command takes, or the default when it
// takes none. A second argument is a typo, and a typo that is ignored is a
// command that worked on something other than what it was given.
//
// Both words are named, because the first of them is the likelier mistake: a
// command nobody has is read as a cache path, and the surplus word is the only
// sign of it.
func only(args []string, or string) (string, error) {
	switch len(args) {
	case 0:
		return or, nil
	case 1:
		return args[0], nil
	}
	return "", fmt.Errorf("%q and %q: one cache path is the most any command takes\n%s",
		args[0], args[1], usage)
}

// cache is where the schedule lives: the user's cache directory, which is where
// update builds it. The other implementation of this program reads the same
// path, so one download serves both.
func cache() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		// No cache directory to be had. The working directory is somewhere, and
		// naming the file says which one it is.
		return ".otransit.db"
	}
	return filepath.Join(dir, "otransit", "gtfs.db")
}

// browsable is the cache to open for reading: the real one, and the slice in
// this repository until an update has built the real one.
func browsable() string {
	if path := cache(); exists(path) {
		return path
	}
	if exists(slice) {
		return slice
	}
	return cache()
}

// pins is where the kept boards live: beside the config file, and never beside
// the cache. Deleting the cache is a documented way to recover from a bad
// ingest, and the pins are the only thing here a person cannot get back.
//
// The other implementation of this program reads the same file, so a board
// pinned in one is pinned in both.
func pins() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ".otransit-pins"
	}
	return filepath.Join(dir, "otransit", "pins")
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// browse runs the program on the terminal a person is sitting at.
//
// The banner is printed before the viewport is claimed, so it stays above the
// band the program redraws and scrolls away with the rest of the session.
func browse(ctx context.Context, path string) error {
	if !term.Interactive(os.Stdin, os.Stdout) {
		return fmt.Errorf("this is not a terminal. %s", usage)
	}
	if !exists(path) {
		return fmt.Errorf("there is no schedule at %s yet. `otransit update` downloads the published feed and builds it", path)
	}
	// The mark, before the viewport is claimed, so it lands in scrollback and is
	// never repainted. It draws no rule of its own: the viewport's top edge is
	// the rule the pole stands on, and nothing goes between them.
	if err := logo.Print(os.Stdout, term.Columns(os.Stdout)); err != nil {
		return err
	}
	return live.Run(ctx, path, pins(), os.Stdin, os.Stdout)
}

func replayCmd(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("replay takes a fixture directory\n%s", usage)
	}

	var opt replay.Options
	for _, arg := range args[1:] {
		switch arg {
		case "semantic":
			opt.Semantic = true
		case "styles":
			opt.Styles = true
		default:
			return fmt.Errorf("%q is not an observation\n%s", arg, usage)
		}
	}
	return replay.Run(ctx, args[0], opt, out)
}
