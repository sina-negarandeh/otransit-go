package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"

	"github.com/sina-negarandeh/otransit-go/internal/term"
)

// The two claims the conformance suite cannot make.
//
// Everything a fixture pins is inside the viewport. These two are about the
// terminal around it: that the program never takes the whole screen, and that it
// gives the terminal back when it leaves. Neither can be seen in an artifact, and
// the first one regressed in the other implementation and took over a terminal,
// which is why they are driven through a real pty against the real binary.
//
// Nothing else is asserted on a pty stream. A terminal repaints only the cells
// that changed, so a stream read end to end runs one frame into the next and
// says little about either. These two claims are about which sequences appear at
// all and about an exit code, and both survive that.

// build makes the binary these tests drive, once.
func build(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "otransit")
	out, err := exec.Command("go", "build", "-o", path, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return path
}

// ended waits for the program to exit, and fails rather than waiting for ever.
//
// Three seconds. The keys go in 120ms apart and the program exits in well under
// one, so a program still running after three is not going to stop. Unbounded,
// this is not a test that fails when the program stops exiting: it is a test that
// hangs, and the whole package then dies on Go's own ten-minute timeout with a
// goroutine dump in place of a sentence.
func ended(t *testing.T, cmd *exec.Cmd) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		//nolint:errcheck // the test has already failed. This is a best effort
		// not to leave a child behind holding a terminal.
		cmd.Process.Kill()
		t.Fatal("the program did not exit")
		return nil
	}
}

// driven runs the binary on a pty, sends keys, and returns everything it wrote
// and how it ended.
func driven(t *testing.T, keys string) (string, error) {
	t.Helper()
	cmd := exec.Command(build(t), "conformance/cache.db")

	tty, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	defer tty.Close() //nolint:errcheck // the child is waited for below

	// Read until the program closes the pty, which it does by exiting.
	var stream strings.Builder
	done := make(chan struct{})
	go func() {
		defer close(done)
		//nolint:errcheck // the copy ends when the child closes the pty, which is
		// how the program exiting reaches this test.
		io.Copy(&stream, tty)
	}()

	// A moment for the first frame, then the keys, one at a time: a person does
	// not paste a walk.
	time.Sleep(300 * time.Millisecond)
	for _, key := range strings.Split(keys, "") {
		if _, err := tty.WriteString(key); err != nil {
			t.Fatalf("writing %q: %v", key, err)
		}
		time.Sleep(120 * time.Millisecond)
	}

	err = ended(t, cmd)
	// The reader ends when the child closes the pty, which is how the program
	// exiting reaches this test. Bounded, because a hang here is a ten-minute
	// timeout and a dump instead of one line.
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the pty stayed open after the program ended")
	}
	return stream.String(), err
}

// The alternate screen is never entered. A program that takes it cannot be
// scrolled back to, and the session a person was in disappears while it runs.
//
// Counted in the stream rather than reasoned about: this is the claim that
// regressed once, and the only way to be sure is to look at what the terminal
// was actually sent.
func TestTheProgramNeverTakesTheWholeScreen(t *testing.T) {
	if _, err := os.Stat("conformance/cache.db"); err != nil {
		t.Skip("no cache slice to browse: conformance is a symlink into the other checkout")
	}

	stream, err := driven(t, "\x1b")
	if err != nil {
		t.Fatalf("the program ended with %v", err)
	}
	for _, banned := range []struct {
		name     string
		sequence string
	}{
		{"the alternate screen", "\x1b[?1049h"},
		{"leaving the alternate screen", "\x1b[?1049l"},
		{"the older alternate screen", "\x1b[?47h"},
		{"leaving the older one", "\x1b[?47l"},
		{"a full clear", "\x1b[2J"},
		{"a scrolling region", "\x1b[r"},
	} {
		if n := strings.Count(stream, banned.sequence); n != 0 {
			t.Errorf("the program wrote %s (%q) %d times, want never",
				banned.name, banned.sequence, n)
		}
	}

	// And it did draw: a test that asserted only absence would pass on a program
	// that wrote nothing at all.
	if !strings.Contains(stream, "What are you taking?") {
		t.Errorf("the first screen was never drawn:\n%q", stream)
	}
}

// Esc on the first screen ends the program, and it ends cleanly: the cursor is
// shown again and the terminal is out of raw mode.
//
// No fixture reaches this. The first screen is the floor, so esc there has
// nowhere to go back to, and a person pressing it has asked to be done.
func TestEscOnTheFirstScreenExitsCleanly(t *testing.T) {
	if _, err := os.Stat("conformance/cache.db"); err != nil {
		t.Skip("no cache slice to browse")
	}

	stream, err := driven(t, "\x1b")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("the program exited %d, want 0", exit.ExitCode())
		}
		t.Fatalf("the program ended with %v", err)
	}
	// The cursor is hidden while a frame is drawn and shown again after. The last
	// thing written has to be the showing, or a person is left with no cursor.
	if !strings.HasSuffix(stream, "\x1b[?25h") {
		t.Errorf("the stream does not end by showing the cursor:\n%q", tail(stream))
	}
}

// The terminal is given back the way it was found.
//
// The program turns the echo off to draw every character a person sees itself. A
// program that failed to turn it back on leaves somebody typing into a terminal
// that shows them nothing, and a shell that looks broken. Read off the pty after
// the child has gone, because that is where the settings outlive it.
func TestTheTerminalIsGivenBack(t *testing.T) {
	if _, err := os.Stat("conformance/cache.db"); err != nil {
		t.Skip("no cache slice to browse")
	}

	cmd := exec.Command(build(t), "conformance/cache.db")
	tty, err := pty.Start(cmd)
	if err != nil {
		t.Fatalf("pty.Start: %v", err)
	}
	defer tty.Close() //nolint:errcheck // the child is waited for below

	before, err := term.Settings(tty)
	if err != nil {
		t.Fatalf("reading the terminal before: %v", err)
	}
	if before.Lflag&unix.ECHO == 0 {
		t.Fatal("the pty started with its echo already off, so this proves nothing")
	}

	go func() {
		//nolint:errcheck // drained so the child is never blocked writing
		io.Copy(io.Discard, tty)
	}()
	time.Sleep(300 * time.Millisecond)
	if _, err := tty.WriteString("\x1b"); err != nil {
		t.Fatalf("writing esc: %v", err)
	}
	if err := ended(t, cmd); err != nil {
		t.Fatalf("the program ended with %v", err)
	}

	after, err := term.Settings(tty)
	if err != nil {
		t.Fatalf("reading the terminal after: %v", err)
	}
	if after.Lflag&unix.ECHO == 0 {
		t.Error("the echo is still off, so a person is typing into a terminal that shows nothing")
	}
	if after.Lflag&unix.ICANON == 0 {
		t.Error("the terminal is still reading a key at a time rather than a line")
	}
	if *after != *before {
		t.Errorf("the terminal came back with different settings:\nbefore %+v\nafter  %+v", before, after)
	}
}

// The program leaves when the terminal goes away, rather than reading a closed
// pipe forever.
func TestTheProgramLeavesWhenTheTerminalDoes(t *testing.T) {
	if _, err := os.Stat("conformance/cache.db"); err != nil {
		t.Skip("no cache slice to browse")
	}
	if _, err := driven(t, "\x04"); err != nil {
		t.Errorf("end of file ended the program with %v, want a clean exit", err)
	}
}

func tail(s string) string {
	if len(s) < 120 {
		return s
	}
	return "..." + s[len(s)-120:]
}

// A letter on a list is filtered, and the real binary has to agree.
//
// Two keys end the program: esc on the first screen, and q on a departures
// board. Everywhere else a letter belongs to the filter. This is the claim a
// unit test cannot make, because the thing that broke was the program a person
// runs: q quit on the first keystroke of QUEENSWAY, and j and k moved the cursor
// instead of finding JOCKVALE or KANATA.
func TestALetterOnTheFirstScreenDoesNotEndTheProgram(t *testing.T) {
	if _, err := os.Stat("conformance/cache.db"); err != nil {
		t.Skip("no cache slice to browse")
	}

	// Three letters, then esc to leave the search and esc again to be done.
	stream, err := driven(t, "qjk\x1b\x1b")
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("the program exited %d, want 0", exit.ExitCode())
		}
		t.Fatalf("the program ended with %v", err)
	}

	// The screen drawn after the last letter proves the program outlived all
	// three, and that each one reached the filter rather than a command.
	if !strings.Contains(stream, "/qjk") {
		t.Errorf("the filter never read qjk:\n%q", tail(stream))
	}
	if !strings.Contains(stream, "0 found") {
		t.Errorf("the search the letters opened never drew:\n%q", tail(stream))
	}
}
