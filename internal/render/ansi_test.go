package render

import (
	"strings"
	"testing"
)

// A style becomes an escape, and a run of the terminal's own colours becomes
// nothing at all: what is written is what the program chose.
func TestAStyleBecomesTheEscapeThatTurnsItOn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		style Style
		want  string
	}{
		{"nothing at all", Style{}, ""},
		{"a colour", Style{Fg: "#da3839"}, "\x1b[38;2;218;56;57m"},
		{"a colour and bold", Style{Fg: "#e6e6e6", Bold: true}, "\x1b[1;38;2;230;230;230m"},
		{"a badge, which has both", Style{Fg: "#ffffff", Bg: "#0057b8", Bold: true},
			"\x1b[1;38;2;255;255;255;48;2;0;87;184m"},
		{"dim and struck through, which is a bus that has gone",
			Style{Fg: "#6d6e70", Strike: true}, "\x1b[9;38;2;109;110;112m"},
		{"a modifier with no colour", Style{Bold: true}, "\x1b[1m"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sgr(tc.style); got != tc.want {
				t.Errorf("sgr(%v) = %q, want %q", tc.style, got, tc.want)
			}
		})
	}
}

// The three ways to write a buffer out read the same cells, so a screen cannot
// look one way in an artifact and another on a terminal.
func TestTheTerminalFormCarriesTheSameTextAsTheArtifact(t *testing.T) {
	t.Parallel()
	v := View{
		Width: 30, Height: 4, Cursor: true,
		Rows:   []Row{{Cells: []Cell{{At: 3, Text: "Bus", Style: Style{Fg: "#e6e6e6"}}}}},
		Rule:   Style{Fg: "#3a3b3d"},
		Status: Status{Label: "Which way?", Hints: "q", Paint: Paint{Label: Style{Bold: true}}},
	}
	b := Draw(v)

	// Every escape stripped out of the terminal form leaves the artifact form,
	// row for row.
	stripped := strings.ReplaceAll(escapes.Replace(b.ANSI()), "\r\n", "\n")
	want := strings.TrimSuffix(b.String(), "\n")
	if stripped != want {
		t.Errorf("the terminal form reads\n%q\nand the artifact form reads\n%q", stripped, want)
	}
}

// escapes takes the escapes back out, which is how a test compares what was
// drawn with what a diff would have shown.
var escapes = strings.NewReplacer(
	reset, "", clearRow, "", hide, "", show, "",
	"\x1b[38;2;230;230;230m", "", "\x1b[38;2;58;59;61m", "", "\x1b[1m", "",
)

// The alternate screen is never entered. This program draws under what the
// terminal already holds, and a program that took the whole screen could not be
// scrolled back to.
func TestNothingHereEntersTheAlternateScreen(t *testing.T) {
	t.Parallel()
	b := Draw(View{
		Width: 40, Height: 11, Cursor: true,
		Rows:   []Row{{Cells: []Cell{{At: 3, Text: "Bus", Style: Style{Fg: "#e6e6e6"}}}}},
		Rule:   Style{Fg: "#3a3b3d"},
		Status: Status{Label: "Which way?", Hints: "q", Paint: Paint{Hints: Style{Fg: "#3a3b3d"}}},
	})
	for _, banned := range []string{"\x1b[?1049h", "\x1b[?1049l", "\x1b[?47h", "\x1b[?47l", "smcup", "rmcup"} {
		if strings.Contains(b.ANSI(), banned) {
			t.Errorf("the terminal form holds %q", banned)
		}
	}
}

// One row, byte for byte. The strip-the-escapes tests above check that the text
// is right; this one checks the escapes themselves, which is where a colour that
// bleeds into the next run or a tail left behind by a shorter row would live.
func TestARowIsWrittenRunByRun(t *testing.T) {
	t.Parallel()
	b := newBuffer(10, 1)
	drawRow(b, Row{Cells: []Cell{
		{At: 0, Text: "ab", Style: Style{Fg: "#ff0000"}},
		{At: 4, Text: "cd", Style: Style{Bold: true}},
	}}, 0, false)

	// Red, the text, off. Two plain spaces. Bold, the text, off. The rest of the
	// row, and then the end of it cleared.
	want := "\x1b[38;2;255;0;0mab" + reset + "  " + "\x1b[1mcd" + reset + "    " + clearRow
	if got := b.ANSI(); got != want {
		t.Errorf("the row is\n%q\nwant\n%q", got, want)
	}
}

// Every row ends by clearing what is past it, because a row is drawn over
// whatever was there and a shorter row would otherwise leave the end of a longer
// one behind.
func TestEveryRowClearsWhatIsPastIt(t *testing.T) {
	t.Parallel()
	b := Draw(View{Width: 20, Height: 4, Rule: Style{Fg: "#3a3b3d"},
		Status: Status{Label: "Which way?", Hints: "q"}})

	rows := strings.Split(b.ANSI(), "\r\n")
	if len(rows) != 4 {
		t.Fatalf("wrote %d rows, want 4", len(rows))
	}
	for i, row := range rows {
		if !strings.HasSuffix(row, clearRow) {
			t.Errorf("row %d ends %q, want it to clear to the end of the line", i, row[max(0, len(row)-12):])
		}
	}
}

// A style is turned off where its run ends. A run that left its colour on would
// paint everything after it, and on the next row too.
func TestAStyleIsTurnedOffWhereItsRunEnds(t *testing.T) {
	t.Parallel()
	b := newBuffer(6, 1)
	drawRow(b, Row{Cells: []Cell{{At: 0, Text: "ab", Style: Style{Fg: "#ff0000"}}}}, 0, false)

	got := b.ANSI()
	on := strings.Index(got, "\x1b[38;2;255;0;0m")
	off := strings.Index(got, reset)
	if on < 0 || off < 0 {
		t.Fatalf("the row is %q, want a colour turned on and off", got)
	}
	if off < on {
		t.Errorf("the row is %q, want the colour turned off after it was turned on", got)
	}
	if strings.Count(got, reset) != 1 {
		t.Errorf("the row is %q, want the one run turned off once", got)
	}
}
