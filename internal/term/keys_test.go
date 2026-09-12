package term

import (
	"strings"
	"testing"
)

// keys is every key the bytes decode to.
func keys(t *testing.T, bytes string) []Key {
	t.Helper()
	var out []Key
	for key := range Keys(strings.NewReader(bytes)) {
		out = append(out, key)
	}
	return out
}

func TestTheKeysThisProgramUnderstands(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		bytes string
		want  []Key
	}{
		{"a letter", "b", []Key{{Kind: Rune, Text: 'b'}}},
		{"a word, one key at a time", "bil", []Key{
			{Kind: Rune, Text: 'b'}, {Kind: Rune, Text: 'i'}, {Kind: Rune, Text: 'l'},
		}},
		// A pole number is written #3009 everywhere in this app, and a stop name
		// can hold a slash: BILLINGS BRIDGE / BANK.
		{"a hash and a slash", "#/", []Key{
			{Kind: Rune, Text: '#'}, {Kind: Rune, Text: '/'},
		}},
		{"return", "\r", []Key{{Kind: Enter}}},
		{"a newline, which a pty sends instead", "\n", []Key{{Kind: Enter}}},
		{"backspace", "\x7f", []Key{{Kind: Backspace}}},
		{"the other backspace", "\x08", []Key{{Kind: Backspace}}},
		{"up and down", "\x1b[A\x1b[B", []Key{{Kind: Up}, {Kind: Down}}},
		// Some terminals send the arrows in application mode, with an O.
		{"up and down in application mode", "\x1bOA\x1bOB", []Key{{Kind: Up}, {Kind: Down}}},
		// An escape with nothing behind it is the key itself, which is how a
		// person leaves a screen.
		{"escape alone", "\x1b", []Key{{Kind: Esc}}},
		{"an interrupt", "\x03", []Key{{Kind: Quit}}},
		{"end of file", "\x04", []Key{{Kind: Quit}}},
		// Left and right are keys this program has no use for. They are dropped
		// rather than guessed at, because guessing would move the selection.
		{"left and right", "\x1b[C\x1b[D", nil},
		{"a tab", "\t", nil},
		{"a character from outside ascii", "é", []Key{{Kind: Rune, Text: 'é'}}},
		{"nothing at all", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := keys(t, tc.bytes)
			if len(got) != len(tc.want) {
				t.Fatalf("%q decoded to %v, want %v", tc.bytes, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("%q key %d = %v, want %v", tc.bytes, i, got[i], tc.want[i])
				}
			}
		})
	}
}

// An escape followed by a letter is the arrow keys and nothing else, so a person
// pressing esc and then typing does not lose the letter.
func TestEscapeThenALetterIsTwoKeys(t *testing.T) {
	t.Parallel()
	got := keys(t, "\x1b")
	if len(got) != 1 || got[0].Kind != Esc {
		t.Fatalf("escape alone decoded to %v, want one esc", got)
	}
	// The same bytes a terminal sends when somebody presses esc and then b fast
	// enough that both arrive together. The escape was the key, and the letter is
	// the next one rather than something the escape ate.
	both := keys(t, "\x1bb")
	want := []Key{{Kind: Esc}, {Kind: Rune, Text: 'b'}}
	if len(both) != len(want) || both[0] != want[0] || both[1] != want[1] {
		t.Errorf("escape then b decoded to %v, want %v", both, want)
	}
}
