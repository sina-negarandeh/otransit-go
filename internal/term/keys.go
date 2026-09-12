package term

import (
	"bufio"
	"io"
	"unicode/utf8"
)

// Kind is a key a person pressed.
type Kind int

// The keys this program understands. Everything else is ignored rather than
// guessed at: a terminal sends sequences for keys nobody here has a use for.
const (
	Rune Kind = iota
	Up
	Down
	Enter
	Esc
	Backspace
	Quit
)

// Key is one keypress. Text carries the character for a Rune.
type Key struct {
	Kind Kind
	Text rune
}

// Keys reads keypresses until the reader ends.
//
// The channel closes when the reader does, which is how the loop learns that the
// terminal went away: a pipe that closed, or a session that ended under the
// program.
func Keys(r io.Reader) <-chan Key {
	out := make(chan Key)
	go func() {
		defer close(out)
		read := bufio.NewReader(r)
		for {
			key, ok := decode(read)
			if !ok {
				return
			}
			if key.Kind == Rune && key.Text == 0 {
				continue
			}
			out <- key
		}
	}()
	return out
}

// decode reads one key.
//
// An escape is ambiguous by nature: it is the esc key, and it is also the first
// byte of the arrow keys. What tells them apart is whether anything follows, and
// the only honest way to ask that is to look. A buffered reader that has more
// bytes in hand answers without waiting, and one that does not blocks, which is
// exactly right for a person pressing esc and nothing else.
func decode(r *bufio.Reader) (Key, bool) {
	first, err := r.ReadByte()
	if err != nil {
		return Key{}, false
	}

	switch first {
	case 0x1b:
		return escaped(r), true
	case '\r', '\n':
		return Key{Kind: Enter}, true
	case 0x7f, 0x08:
		return Key{Kind: Backspace}, true
	case 0x03, 0x04:
		// Interrupt and end of file. A person who types either has asked to
		// leave, and raw mode means the terminal will not do it for them.
		return Key{Kind: Quit}, true
	}
	if first < 0x20 {
		// The other control characters mean nothing here.
		return Key{}, true
	}

	if first < utf8.RuneSelf {
		return Key{Kind: Rune, Text: rune(first)}, true
	}
	// A multi-byte character. Put the first byte back and let the reader take
	// the whole rune, because a stop name can hold one.
	if err := r.UnreadByte(); err != nil {
		return Key{}, false
	}
	text, _, err := r.ReadRune()
	if err != nil {
		return Key{}, false
	}
	return Key{Kind: Rune, Text: text}, true
}

// escaped reads what follows an escape byte. Nothing following means the esc key
// itself.
func escaped(r *bufio.Reader) Key {
	if r.Buffered() == 0 {
		return Key{Kind: Esc}
	}
	bracket, err := r.ReadByte()
	if err != nil {
		return Key{Kind: Esc}
	}
	// The arrows arrive as escape then [ or O, then a letter. Anything else
	// means the escape was the key on its own, and what followed is the next
	// one: a person who presses esc and then types must not lose the letter.
	if bracket != '[' && bracket != 'O' {
		//nolint:errcheck // an unread cannot fail directly after a read, and esc
		// is the answer either way: the worst case loses the letter, not the key.
		r.UnreadByte()
		return Key{Kind: Esc}
	}
	letter, err := r.ReadByte()
	if err != nil {
		return Key{Kind: Esc}
	}
	switch letter {
	case 'A':
		return Key{Kind: Up}
	case 'B':
		return Key{Kind: Down}
	}
	return Key{}
}
