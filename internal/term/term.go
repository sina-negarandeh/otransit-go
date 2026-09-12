// Package term is the terminal: raw mode, the keys a person presses, and an
// inline viewport to draw in.
//
// It claims a fixed band of rows at the bottom of the screen and leaves
// everything above alone. It never takes the alternate screen. A person running
// this can scroll back through their session afterwards and find it, and the
// shell prompt they started from is still above it.
//
// Nothing here knows what a screen holds. It is handed the bytes to write.
package term

import (
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// Rows is how tall the viewport is, and it never changes while the program runs.
//
// Eight rows of content, two rules and the status bar. The same eleven rows the
// conformance fixtures are drawn in, which is why what a fixture shows is what a
// person sees.
const Rows = 11

// Session is a terminal put into raw mode, with rows reserved to draw in.
type Session struct {
	in    *os.File
	out   *os.File
	was   *unix.Termios
	width int
}

// Open claims the viewport and puts the terminal into raw mode.
//
// Close undoes both. The two are written as a pair and called as a pair, because
// a program that turned the echo off and failed before turning it back on leaves
// a person typing into a terminal that shows nothing.
func Open(in, out *os.File) (*Session, error) {
	if !Interactive(in, out) {
		return nil, errors.New("this is not a terminal, so there is nothing to draw on")
	}

	was, err := Settings(in)
	if err != nil {
		return nil, fmt.Errorf("reading the terminal's settings: %w", err)
	}
	raw := *was
	// No echo and no line discipline: this program draws every character a
	// person sees, and it answers a keypress rather than a finished line.
	raw.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	raw.Iflag &^= unix.IXON | unix.ICRNL | unix.BRKINT | unix.INPCK | unix.ISTRIP
	// One byte is enough to wake a read, and a read never times out.
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(int(in.Fd()), setTermios, &raw); err != nil {
		return nil, fmt.Errorf("putting the terminal into raw mode: %w", err)
	}

	s := &Session{in: in, out: out, was: was, width: Columns(out)}
	// The band is claimed by moving down through it once, which scrolls the
	// session up by however much it needs. Whatever was on screen stays on
	// screen, higher up.
	//
	// One fewer newline than there are rows: the line the cursor is already on is
	// the first of them. Every draw from here leaves the cursor on the last row,
	// and every draw begins by going back up to the first, so one extra newline
	// here would put every frame a row below its own band.
	if _, err := fmt.Fprint(out, claim); err != nil {
		return nil, s.close(err)
	}
	return s, nil
}

// Width is how wide the terminal was when the program started.
//
// It is read once. The reference cannot resize an inline viewport either, so a
// window that changes size mid-session keeps the width it was opened with rather
// than redrawing at a size half the rows were written for.
func (s *Session) Width() int { return s.width }

// Draw puts one frame in the band.
//
// It begins and ends on the last row, which is the invariant the whole viewport
// rests on: go up to the first row, write every row over what was there, and land
// back where it started. The cursor is hidden while that happens, so a person
// never sees it travel.
func (s *Session) Draw(frame string) error {
	_, err := fmt.Fprint(s.out, hide+toFirstRow+frame+show)
	return err
}

// Close puts the terminal back the way it was found: the settings restored, the
// viewport left blank, the cursor shown, and the shell's next prompt below it.
func (s *Session) Close() error { return s.close(nil) }

// close is the one exit path. It reports the first thing that went wrong and
// does every step whatever happened before it, because a terminal left in raw
// mode is worse than a message nobody reads.
func (s *Session) close(err error) error {
	blank := ""
	for i := 0; i < Rows; i++ {
		blank += clearRow
		if i < Rows-1 {
			blank += "\r\n"
		}
	}
	if _, wErr := fmt.Fprint(s.out, hide+toFirstRow+blank+"\r\n"+show); wErr != nil && err == nil {
		err = wErr
	}
	if tErr := unix.IoctlSetTermios(int(s.in.Fd()), setTermios, s.was); tErr != nil && err == nil {
		err = fmt.Errorf("putting the terminal back: %w", tErr)
	}
	return err
}

// Interactive reports whether both ends are a terminal. A program whose output
// is a file has nothing to draw on and nobody to read it.
func Interactive(in, out *os.File) bool {
	return isTerminal(in) && isTerminal(out)
}

func isTerminal(f *os.File) bool {
	_, err := Settings(f)
	return err == nil
}

// Settings is how a terminal is set up: its echo, its line discipline, and the
// rest of what a termios holds.
//
// The ioctl that reads them is a different number on every unix, and this is the
// only place that knows which. Asking a terminal about itself from anywhere else
// means naming Darwin's number or Linux's, and the one that compiles here is not
// the one that compiles on the machine CI runs on.
func Settings(f *os.File) (*unix.Termios, error) {
	return unix.IoctlGetTermios(int(f.Fd()), getTermios)
}

// Columns is how many cells across the terminal is, and eighty when it will not
// say. Eighty is the width a terminal has had since before anyone asked.
func Columns(out *os.File) int {
	size, err := unix.IoctlGetWinsize(int(out.Fd()), unix.TIOCGWINSZ)
	if err != nil || size.Col == 0 {
		return 80
	}
	return int(size.Col)
}

func newlines(n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = '\n'
	}
	return string(out)
}

// claim moves down through the band once, which is how its rows are taken, and
// toFirstRow is the move back up that every draw begins with.
//
// The two have to agree. One newline too many here and every frame is drawn a row
// below its own band, over the line the program was started from.
var (
	claim      = newlines(Rows - 1)
	toFirstRow = "\r" + fmt.Sprintf(up, Rows-1)
)

// The escapes this package writes. There is no alternate screen among them.
const (
	hide     = "\x1b[?25l"
	show     = "\x1b[?25h"
	up       = "\x1b[%dA"
	clearRow = "\x1b[K"
)

// Reader is what a session reads keys from. A test hands in bytes of its own.
type Reader interface{ io.Reader }
