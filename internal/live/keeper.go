// The pins file: the one thing this program leaves behind.
//
// It lives beside the config file and not beside the cache, because deleting the
// cache is how a person recovers from a bad ingest and these are the only lines
// they cannot get back. The loop asks after every turn, and nothing here decides
// what a pin is: that is the app's, and this is the file.
package live

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sina-negarandeh/otransit-go/internal/app"
)

// A keeper is the pins file: where it is, and what it last held.
//
// It compares before it writes, so a turn that moved no pin touches no disk, and
// a program left running overnight does not rewrite the file every second.
type keeper struct {
	path string
	held []app.Pin
	// unread is why the file could not be taken whole: it would not open, or a
	// line in it would not parse. The keeper writes nothing after that, because
	// the only thing it could write is a file with that line missing, and these
	// are the only lines a person cannot get back.
	unread error
}

// read is the pins in the file.
//
// A missing file is not an error: it is what "no pins yet" looks like, and a
// first run must not fail on it. Anything else is reported, and the pins that
// did parse come back beside it, so a bad line costs that line for this session
// and costs nothing on disk.
func (k *keeper) read() ([]app.Pin, error) {
	if k == nil || k.path == "" {
		return nil, nil
	}
	text, err := os.ReadFile(k.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		// Named, because this is read before the screen exists and printed as the
		// only line a person gets about it.
		k.unread = fmt.Errorf("reading the pins: %w", err)
		return nil, k.unread
	}

	pins, parseErr := app.ParsePins(strings.NewReader(string(text)))
	k.held, k.unread = pins, parseErr
	return pins, parseErr
}

// wrote writes the pins down if they have moved since the last time.
//
// It is written beside the file and moved into place, the way the cache is: this
// is the one thing here a person cannot rebuild, and a write that stopped half
// way through would leave them a truncated line and no pins.
func (k *keeper) wrote(a *app.App) error {
	if k == nil || k.path == "" || k.unread != nil {
		return nil
	}
	pins := a.Pins()
	if slices.Equal(pins, k.held) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o755); err != nil {
		return fmt.Errorf("writing the pins: %w", err)
	}
	beside := k.path + ".new"
	if err := os.WriteFile(beside, []byte(app.RenderPins(pins)), 0o600); err != nil {
		return fmt.Errorf("writing the pins: %w", err)
	}
	if err := os.Rename(beside, k.path); err != nil {
		return fmt.Errorf("writing the pins: %w", errors.Join(err, os.Remove(beside)))
	}
	k.held = pins
	return nil
}
