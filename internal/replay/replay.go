// Package replay runs a fixture and writes the artifact two implementations
// are compared on.
//
// Four rules decide whether the comparison means anything, and all four are in
// conformance/README.md. Nothing reads the machine, every key goes through the
// handler the event loop uses, a frame is one whole turn of that loop, and a
// replay never writes to the directory it is replaying.
package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/app"
	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/clock"
	"github.com/sina-negarandeh/otransit-go/internal/detour"
	"github.com/sina-negarandeh/otransit-go/internal/realtime"
	"github.com/sina-negarandeh/otransit-go/internal/render"
	"github.com/sina-negarandeh/otransit-go/internal/script"
	"github.com/sina-negarandeh/otransit-go/internal/weather"
)

// Options are the observations to write. The text is always written, and each
// flag adds to it.
type Options struct {
	Semantic bool
	Styles   bool
}

// Run replays the fixture at dir, or every fixture under it when dir holds no
// script of its own. Fixtures run in name order, so the artifact is a function
// of the directory alone.
func Run(ctx context.Context, dir string, opt Options, w io.Writer) (err error) {
	out := bufio.NewWriter(w)
	// Flush on the way out however this ends. A fixture that fails half way
	// through a suite used to discard every frame already written, so the
	// comparison showed an empty file rather than the point it broke at.
	defer func() {
		if flushErr := out.Flush(); err == nil {
			err = flushErr
		}
	}()

	if _, statErr := os.Stat(filepath.Join(dir, "script")); statErr == nil {
		return one(ctx, dir, opt, out)
	}

	names, err := fixtures(dir)
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, err := fmt.Fprintf(out, "\n======== %s ========\n", name); err != nil {
			return err
		}
		if err := one(ctx, filepath.Join(dir, name), opt, out); err != nil {
			return err
		}
	}
	return nil
}

func fixtures(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, e.Name(), "script")); err == nil {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no fixture under %s, and no script in it either", dir)
	}
	// Total: a directory holds each name once.
	sort.Strings(names)
	return names, nil
}

func one(ctx context.Context, dir string, opt Options, w *bufio.Writer) error {
	s, err := script.Load(filepath.Join(dir, "script"))
	if err != nil {
		return err
	}

	// Named before any work starts. Left to the driver, a cache that is not
	// there first showed up as an errno from inside the opening query, which
	// told a person neither which file nor which fixture.
	if _, statErr := os.Stat(s.CachePath()); statErr != nil {
		return fmt.Errorf("%s has no %s", s.Dir, s.Cache)
	}

	c, err := cache.Open(s.CachePath())
	if err != nil {
		return err
	}
	defer c.Close() //nolint:errcheck // the cache is open read-only and nothing was written

	kept, err := pinsIn(dir)
	if err != nil {
		return err
	}
	outside, err := weatherIn(dir)
	if err != nil {
		return err
	}
	// A script that queues answers drives the poller with them, and the feed
	// files it names are read through the queue. A fixture with a feed file and
	// no queue hands over data and no cadence.
	var poller app.Feed
	queue, err := queueIn(dir, s.Answers)
	if err != nil {
		return err
	}
	if len(queue) == 0 {
		if poller, err = feedIn(dir); err != nil {
			return err
		}
	}
	notices, err := updatesIn(dir)
	if err != nil {
		return err
	}
	return runScript(ctx, s, c, kept, outside, poller, queue, notices, opt, w)
}

// pinsIn reads the fixture's pins, if it has any. A fixture with no pins file
// has none.
//
// Nothing is copied and nothing is written: this program holds its pins in
// memory for the length of a replay, so a script that presses p cannot reach
// the directory it is replaying.
func pinsIn(dir string) ([]app.Pin, error) {
	f, err := os.Open(filepath.Join(dir, "pins"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening pins: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only, and the parse already reported

	kept, err := app.ParsePins(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	return kept, nil
}

// weatherIn reads the fixture's weather, if it has any.
//
// An absent file means the feed had nothing to say. A file that is there must
// parse: the whole claim of a fixture directory is that it decides the output,
// so a feed this program cannot read is an error rather than a quiet nothing.
func weatherIn(dir string) (app.Weather, error) {
	f, err := os.Open(filepath.Join(dir, "weather.json"))
	if errors.Is(err, os.ErrNotExist) {
		return app.Weather{}, nil
	}
	if err != nil {
		return app.Weather{}, fmt.Errorf("opening the weather: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only, and the parse already reported

	now, err := weather.Parse(f)
	if err != nil {
		return app.Weather{}, fmt.Errorf("%s: %w", dir, err)
	}
	return app.Weather{Label: now.Label()}, nil
}

// feedIn reads the fixture's realtime feed, if it has one.
//
// No file means the poller never asked, which is what a missing subscription
// key means. A file that is there must parse: a feed this program cannot read
// draws exactly like a quiet Sunday, so the fixture would pass while saying
// nothing, and the whole claim of a fixture directory is that it decides the
// output.
func feedIn(dir string) (app.Feed, error) {
	path := filepath.Join(dir, "rt.json")
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return app.NoKey(), nil
	}
	live, err := readFeed(path)
	if err != nil {
		return app.Feed{}, err
	}
	return app.Feed{Live: live}, nil
}

// queueIn reads what the script says the endpoint will answer.
//
// Each `feed ok` names a file in the fixture, and the same file can be named
// twice: a feed that answers the same thing again is a feed that answered. A
// refusal names no file and carries its words instead.
func queueIn(dir string, answers []script.Answer) ([]app.Answer, error) {
	out := make([]app.Answer, 0, len(answers))
	for _, a := range answers {
		if a.File == "" {
			out = append(out, app.Answer{Refusal: a.Refusal})
			continue
		}
		live, err := readFeed(filepath.Join(dir, a.File))
		if err != nil {
			return nil, err
		}
		out = append(out, app.Answer{Live: live})
	}
	return out, nil
}

// readFeed parses one realtime feed off disk.
func readFeed(path string) (*realtime.Feed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening the trip updates: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only, and the parse already reported

	live, err := realtime.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return live, nil
}

// updatesIn reads the fixture's updates feed, if it has one.
//
// A file that is there must yield at least one detour. The feed is RSS from a
// content system and the two tags a detour needs are that system's convention,
// so a feed whose tags changed parses cleanly into nothing and draws exactly
// like a week with no detours. Counting what parsed is the only way to tell
// those apart, and a fixture that meant none can leave the file out.
func updatesIn(dir string) (app.Notices, error) {
	f, err := os.Open(filepath.Join(dir, "updates.xml"))
	if errors.Is(err, os.ErrNotExist) {
		return app.Notices{}, nil
	}
	if err != nil {
		return app.Notices{}, fmt.Errorf("opening the updates: %w", err)
	}
	defer f.Close() //nolint:errcheck // read-only, and the parse already reported

	live, err := detour.Parse(f)
	if err != nil {
		return app.Notices{}, fmt.Errorf("%s: %w", dir, err)
	}
	if live.Len() == 0 {
		return app.Notices{}, fmt.Errorf("%s: updates.xml holds no detour; leave the file out to mean none", dir)
	}
	return app.Notices{Live: live}, nil
}

// runScript drives the app through the script. It takes the source rather than
// a cache so a test can supply its own world.
func runScript(ctx context.Context, s *script.Script, src app.Source, kept []app.Pin, outside app.Weather, poller app.Feed, queue []app.Answer, notices app.Notices, opt Options, w *bufio.Writer) error {
	// The zone comes from the script and never from the machine, so the same
	// fixture hashes the same in every timezone.
	k, err := clock.At(s.Date, s.Time, time.FixedZone("", s.Offset))
	if err != nil {
		return err
	}

	a := app.New(src, k, poller)
	a.Pinned(kept)
	a.Outside(outside)
	a.Updates(notices)
	if len(queue) > 0 {
		a.Answers(queue)
	}
	if err := a.Refresh(ctx); err != nil {
		return err
	}
	if err := frame(w, 0, "start", a, s, opt); err != nil {
		return err
	}

	for i, step := range s.Steps {
		// One step is one whole turn of the event loop. A typed word presses
		// several keys and draws one frame, because the person typed once.
		if step.Kind == script.Wait {
			// Time passing is a turn of the loop too. The poller reads the clock
			// on the refresh below and takes every attempt the wait covered.
			a.Wait(step.Secs)
		}
		for _, key := range keys(step) {
			a.Press(key)
		}
		if err := a.Refresh(ctx); err != nil {
			return err
		}
		if err := frame(w, i+1, step.Label, a, s, opt); err != nil {
			return err
		}
		if a.Leaving() {
			// The program ended on that key. The frame above is the last thing a
			// person saw, and this header records that it stopped rather than
			// that it drew an empty screen.
			_, err := fmt.Fprintf(w, "\n--- %d. quit ---\n", i+2)
			return err
		}
	}
	return nil
}

// keys turns a step into the keypresses the event loop would see. Every key
// goes through the same handler, so a replay cannot hold its own opinion of
// what a key does.
func keys(s script.Step) []app.Key {
	switch s.Kind {
	case script.Up:
		return []app.Key{{Kind: app.Up}}
	case script.Down:
		return []app.Key{{Kind: app.Down}}
	case script.Enter:
		return []app.Key{{Kind: app.Enter}}
	case script.Esc:
		return []app.Key{{Kind: app.Esc}}
	case script.Backspace:
		return []app.Key{{Kind: app.Backspace}}
	case script.Key, script.Type:
		out := make([]app.Key, 0, len(s.Text))
		for _, r := range s.Text {
			out = append(out, app.Key{Kind: app.Char, Rune: r})
		}
		return out
	}
	// Wait moves the clock and presses nothing. The fixtures that move a clock
	// arrive with the poll policy.
	return nil
}

func frame(w *bufio.Writer, n int, label string, a *app.App, s *script.Script, opt Options) error {
	if _, err := fmt.Fprintf(w, "\n--- %d. %s ---\n", n, label); err != nil {
		return err
	}
	drawn := render.Draw(a.View(s.Width, s.Height))
	if _, err := w.WriteString(drawn.String()); err != nil {
		return err
	}
	if opt.Styles {
		if _, err := w.WriteString("styles\n" + drawn.Styles()); err != nil {
			return err
		}
	}
	if !opt.Semantic {
		return nil
	}
	if _, err := w.WriteString("semantic\n"); err != nil {
		return err
	}
	return writeSemantic(w, a.Semantic())
}

// writeSemantic writes the decisions the app reached, as JSON with one value
// per line. The vocabulary is the app's, because those are its decisions. What
// belongs here is the encoding.
//
// The keys come back sorted because encoding/json sorts a map, and the other
// implementation builds its objects on a sorted map, so neither side needs an
// ordering step.
func writeSemantic(w io.Writer, decisions map[string]any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// A route long name holds a pair of angle brackets. The default encoder
	// escapes them, and the reference does not, so every fixture carrying a
	// route list would diff on the escape alone. Deleting this line is the
	// regression TestAngleBracketsInANameAreNotEscaped exists to catch.
	enc.SetEscapeHTML(false)
	return enc.Encode(decisions)
}
