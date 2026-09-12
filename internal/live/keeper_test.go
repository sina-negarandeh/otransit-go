package live

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/app"
	"github.com/sina-negarandeh/otransit-go/internal/term"
)

// The pins file is the only thing this program leaves behind. These are about
// the file: when it is read, when it is written, and what happens when it is not
// there or cannot be read.

func TestAFileThatIsNotThereIsNoPinsRatherThanAnError(t *testing.T) {
	t.Parallel()
	// The first run on a machine. There is nothing to read and nothing is wrong.
	k := &keeper{path: filepath.Join(t.TempDir(), "otransit", "pins")}

	held, err := k.read()
	if err != nil {
		t.Errorf("read of a missing file = %v, want no error", err)
	}
	if len(held) != 0 {
		t.Errorf("pins = %+v, want none", held)
	}
}

func TestThePinsInTheFileAreTheOnesTheProgramStartsWith(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "pins")
	if err := os.WriteFile(path, []byte("9030\t8544\tALTA VISTA\t44\tBillings Bridge\n"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	k := &keeper{path: path}
	got, err := k.read()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(got) != 1 || got[0].Stop != "9030" || got[0].Route != "44" {
		t.Errorf("pins = %+v, want the one in the file", got)
	}
}

func TestAFileThatWillNotParseGivesUpWhatItCanAndSaysWhy(t *testing.T) {
	t.Parallel()
	// A person edits this file. One line they got wrong must not cost them the
	// lines above it, and the program must say which line.
	path := filepath.Join(t.TempDir(), "pins")
	if err := os.WriteFile(path, []byte("9030\t8544\tALTA VISTA\n7581\t1869\tTRANSITWAY\t44\n"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	k := &keeper{path: path}
	got, err := k.read()
	if err == nil {
		t.Fatal("a file with a bad line was read without complaint")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("the error is %q, and it has to name the line", err)
	}
	if len(got) != 1 {
		t.Errorf("pins = %+v, want the one line that parsed", got)
	}
}

func TestAPinIsOnDiskBeforeTheProgramEnds(t *testing.T) {
	t.Parallel()
	// The way out is not always this program's decision: a terminal that closes
	// takes the process with it. So a pin is written when it is made.
	path := filepath.Join(t.TempDir(), "otransit", "pins")
	k := &keeper{path: path}
	a := pinned(t)
	a.Pinned([]app.Pin{{Stop: "9030", Code: "8544", Name: "ALTA VISTA", Route: "44", Headsign: "Billings Bridge"}})

	if err := k.wrote(a); err != nil {
		t.Fatalf("wrote: %v", err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file was not written: %v", err)
	}
	if want := "9030\t8544\tALTA VISTA\t44\tBillings Bridge\n"; string(text) != want {
		t.Errorf("the file holds %q, want %q", text, want)
	}
}

func TestATurnThatMovedNoPinTouchesNoDisk(t *testing.T) {
	t.Parallel()
	// The loop turns every second. A file rewritten every second is a file that
	// wears out a disk and loses a race with a person editing it.
	path := filepath.Join(t.TempDir(), "pins")
	k := &keeper{path: path}
	a := pinned(t)

	if err := k.wrote(a); err != nil {
		t.Fatalf("wrote: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("a program with no pins wrote a file")
	}

	a.Pinned([]app.Pin{{Stop: "9030"}})
	if err := k.wrote(a); err != nil {
		t.Fatalf("wrote: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the pin was not written: %v", err)
	}
	if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
		t.Fatalf("setting the time: %v", err)
	}

	// Nothing changed, so nothing is written.
	if err := k.wrote(a); err != nil {
		t.Fatalf("wrote: %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("the file was written again for a turn that moved no pin")
	}
}

func TestAProgramWithNoFileToWriteToKeepsWorking(t *testing.T) {
	t.Parallel()
	// A platform with no config directory, and every test that runs a loop.
	// Neither is a reason to refuse to browse.
	var none *keeper
	a := pinned(t)
	if _, err := none.read(); err != nil {
		t.Errorf("read = %v, want no error", err)
	}
	if err := none.wrote(a); err != nil {
		t.Errorf("wrote = %v, want no error", err)
	}
}

func TestAFileThatCouldNotBeReadIsNeverWrittenOver(t *testing.T) {
	t.Parallel()
	// The pins are the one thing here a person cannot rebuild. A file that will
	// not open is not a file with no pins in it: it is a file whose pins are
	// unknown, and writing this session's over it would destroy them.
	dir := t.TempDir()
	path := filepath.Join(dir, "pins")
	if err := os.WriteFile(path, []byte("9030\t8544\tALTA VISTA\n"), 0o000); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	k := &keeper{path: path}
	if _, err := k.read(); err == nil {
		t.Fatal("a file nobody can read was read")
	}

	a := pinned(t)
	a.Pinned([]app.Pin{{Stop: "s1", Code: "3034", Name: "A NEW PIN"}})
	if err := k.wrote(a); err != nil {
		t.Fatalf("wrote: %v", err)
	}

	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if string(text) != "9030\t8544\tALTA VISTA\n" {
		t.Errorf("the file now holds %q, and it held one pin nobody could read", text)
	}
}

func TestALineNobodyCanParseSurvivesTheSession(t *testing.T) {
	t.Parallel()
	// One bad line costs that line for this session. It must not cost it on
	// disk: the only file the keeper could write is one with the line missing,
	// and a person who mistyped a pin can fix a typo but cannot get a deleted
	// line back.
	path := filepath.Join(t.TempDir(), "pins")
	held := "9030\t8544\tALTA VISTA\n7581\t1869\tTRANSITWAY\t44\n"
	if err := os.WriteFile(path, []byte(held), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	k := &keeper{path: path}
	if _, err := k.read(); err == nil {
		t.Fatal("the bad line was accepted")
	}

	a := pinned(t)
	a.Pinned([]app.Pin{{Stop: "s1", Code: "3034", Name: "A NEW PIN"}})
	if err := k.wrote(a); err != nil {
		t.Fatalf("wrote: %v", err)
	}

	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if string(text) != held {
		t.Errorf("the file now holds %q, want the two lines a person wrote", text)
	}
}

func TestThePinsFileIsReplacedRatherThanOverwritten(t *testing.T) {
	t.Parallel()
	// A write that stopped half way through would leave a truncated line and no
	// pins, so the new file is built beside the old one and moved over it. A
	// rename puts a different file at the name, and that is what is asserted: the
	// absence of a leftover would be just as true of a write that never made one.
	path := filepath.Join(t.TempDir(), "pins")
	if err := os.WriteFile(path, []byte("9030\t8544\tALTA VISTA\n"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	k := &keeper{path: path, held: []app.Pin{{Stop: "9030", Code: "8544", Name: "ALTA VISTA"}}}
	a := pinned(t)
	a.Pinned([]app.Pin{{Stop: "7581", Code: "1869", Name: "TRANSITWAY"}})
	if err := k.wrote(a); err != nil {
		t.Fatalf("wrote: %v", err)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if os.SameFile(before, after) {
		t.Error("the same file is still at the name, so it was written in place")
	}
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if want := "7581\t1869\tTRANSITWAY\n"; string(text) != want {
		t.Errorf("the file holds %q, want %q", text, want)
	}
	if _, err := os.Stat(path + ".new"); err == nil {
		t.Error("the file it was built in is still there")
	}
}

func TestAPinThatCannotBeWrittenDownStillWorksForThisSession(t *testing.T) {
	t.Parallel()
	// A config directory nobody can write is not worth ending a session over.
	// The pin works for this run, and the person is told on the way out, where
	// the message lands in the shell rather than under a band about to go.
	blocked := filepath.Join(t.TempDir(), "in-the-way")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("writing the file in the way: %v", err)
	}

	a, pane := held(t)
	a.Pinned([]app.Pin{{Stop: "s1", Code: "3034", Name: "A PIN"}})
	// Buffered, so a loop that died takes the keys with it instead of hanging the
	// test on a send nobody will read.
	keys := make(chan term.Key, 8)
	done := make(chan error, 1)
	go func() {
		done <- (session{
			app: a, screen: pane, keys: keys, now: time.Now,
			keep: &keeper{path: filepath.Join(blocked, "otransit", "pins")},
		}).browse(context.Background())
	}()

	// The first turn fails to write. Three more keys follow it, and the screen
	// they draw is what says the loop outlived the failure.
	keys <- term.Key{Kind: term.Rune, Text: 'b'}
	keys <- term.Key{Kind: term.Rune, Text: 'i'}
	keys <- term.Key{Kind: term.Rune, Text: 'l'}
	waitFor(t, pane, "/bil")

	// And it leaves the way a person leaves, which is the path the message has to
	// survive: esc off the search, then esc off the first screen.
	keys <- term.Key{Kind: term.Esc}
	keys <- term.Key{Kind: term.Esc}

	err := finished(t, done)
	if err == nil {
		t.Fatal("the session ended without saying the pins could not be written")
	}
	if !strings.Contains(err.Error(), "pins") {
		t.Errorf("the error is %q, and it has to name what could not be written", err)
	}
}

func TestThePinsAreReadBeforeAnythingTouchesTheTerminal(t *testing.T) {
	t.Parallel()
	// A word about the pins can only be read before the band exists, because
	// every row of the band is painted by the first frame and repainted by every
	// frame after it. This proves the order without a terminal: Run is given a
	// cache that is not there, so it fails at the step after the pins, and
	// whatever reached the file was written before that.
	dir := t.TempDir()
	pins := filepath.Join(dir, "pins")
	if err := os.WriteFile(pins, []byte("7581\t1869\tTRANSITWAY\t44\n"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	said, err := os.CreateTemp(dir, "said")
	if err != nil {
		t.Fatalf("making the pipe stand-in: %v", err)
	}
	defer said.Close() //nolint:errcheck // read back below, and the test owns it

	if err := Run(context.Background(), filepath.Join(dir, "no-cache.db"), pins, said, said); err == nil {
		t.Fatal("Run worked with no cache and no terminal")
	}

	text, err := os.ReadFile(said.Name())
	if err != nil {
		t.Fatalf("reading what was said: %v", err)
	}
	if !strings.Contains(string(text), "pins line 1") {
		t.Errorf("nothing said the pins would not parse, only %q", text)
	}
}
