package logo

import (
	"bytes"
	"strings"
	"testing"
)

func TestTheRingIsRoundInCellsRatherThanInColumns(t *testing.T) {
	t.Parallel()
	// A terminal cell is about twice as tall as it is wide, so a ring drawn on
	// equal radii is an ellipse. The whole picture comes out of five constants,
	// and this is the picture they give.
	want := strings.Join([]string{
		"...RRRRRRRR...",
		".RRRRR..RRRRR.",
		"RRR........RRR",
		"RRR........RRR",
		"RRR........RRR",
		".RRRRR..RRRRR.",
		"...RRRRRRRR...",
		"......PP......",
		"......PP......",
	}, "\n")

	if got := picture(build()); got != want {
		t.Errorf("the mark is\n%s\nwant\n%s", got, want)
	}
}

// picture writes a grid the way a person reads it.
func picture(grid [][]cell) string {
	var b strings.Builder
	for y, row := range grid {
		if y > 0 {
			b.WriteByte('\n')
		}
		for _, c := range row {
			b.WriteByte(map[cell]byte{empty: '.', ring: 'R', pole: 'P'}[c])
		}
	}
	return b.String()
}

func TestEveryCellIsPaintedAndNoGlyphIsDrawn(t *testing.T) {
	t.Parallel()
	// The cells are spaces carrying a background colour. A block glyph does not
	// fill the cell in every font, and a font without one is substituted for a
	// font with different metrics, which shears the picture. Paint is the
	// terminal's job and not the font's.
	out := render(100)
	for _, glyph := range []string{"█", "▓", "▒", "░", "▄", "▀"} {
		if strings.Contains(out, glyph) {
			t.Errorf("the mark draws %q, and every cell must be a painted space", glyph)
		}
	}
	if !strings.Contains(out, "\x1b[48;2;218;56;57m") {
		t.Error("the ring is not painted in the brand red as a background")
	}
}

func TestAColourIsWrittenOnlyWhereItChanges(t *testing.T) {
	t.Parallel()
	// A terminal keeps the colour it was given, so a mark that names it for
	// every cell is fourteen escapes a row instead of two. The top row is three
	// empty cells, eight of ring, then three empty again.
	row := strings.Split(render(100), "\n")[1]

	if n := strings.Count(row, "\x1b[48;2;218;56;57m"); n != 1 {
		t.Errorf("the top row names the ring colour %d times, want once", n)
	}
	// One reset ends the ring, and one ends the row.
	if n := strings.Count(row, "\x1b[0m"); n != 2 {
		t.Errorf("the top row resets %d times, want twice", n)
	}
	if got := len(stripped(row)); got != 2+14+3 {
		t.Errorf("the top row is %d cells wide, want two of indent, fourteen of mark and three of gutter", got)
	}
}

// stripped is a line with its escapes taken out, which is what a terminal draws.
func stripped(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); i++ {
		if line[i] == '\x1b' {
			for i < len(line) && line[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(line[i])
	}
	return b.String()
}

func TestTheNameAndTheTaglineSitAgainstTheMiddleOfTheRing(t *testing.T) {
	t.Parallel()
	// Two rows apart, against the ring's middle, and both ignore the pole: the
	// pole hangs below the picture and the words are about the ring.
	lines := strings.Split(render(100), "\n")

	for i, line := range lines {
		holds := strings.Contains(line, "otransit")
		if want := i == 3; holds != want {
			t.Errorf("line %d %s the name, and it belongs on line 3", i, held(holds))
		}
		holds = strings.Contains(line, "OC Transpo schedules in your terminal")
		if want := i == 5; holds != want {
			t.Errorf("line %d %s the tagline, and it belongs on line 5", i, held(holds))
		}
	}
}

func held(yes bool) string {
	if yes {
		return "holds"
	}
	return "does not hold"
}

func TestAMarkWithNothingAfterItDrawsTheRuleItStandsOn(t *testing.T) {
	t.Parallel()
	// `otransit logo` opens no viewport, so nothing else draws the line. Moving
	// the rule into the viewport once left this command showing a pylon standing
	// on nothing, which is the whole design failing on the one command whose only
	// job is to show it.
	for _, width := range []int{40, 200} {
		var out bytes.Buffer
		if err := PrintAlone(&out, width); err != nil {
			t.Fatalf("PrintAlone: %v", err)
		}
		rule := "\x1b[38;2;58;59;61m" + strings.Repeat("─", width) + "\x1b[0m\n"
		if !strings.HasSuffix(out.String(), rule) {
			t.Errorf("at %d cells the mark does not end on its rule:\n%q", width, out.String())
		}
	}
}

func TestTheBannerDrawsNoRuleBecauseTheViewportIsOne(t *testing.T) {
	t.Parallel()
	// The viewport's top edge is what the pole stands on. It repaints, it carries
	// the weather, and it stays on screen after the mark has scrolled away. A
	// second rule here would be drawn once and then sit above the first.
	var out bytes.Buffer
	if err := Print(&out, 100); err != nil {
		t.Fatalf("Print: %v", err)
	}
	if strings.Contains(out.String(), "─") {
		t.Errorf("the banner draws a rule of its own:\n%q", out.String())
	}
}

func TestATerminalTooNarrowForTheMarkGetsOneLine(t *testing.T) {
	t.Parallel()
	// The mark needs its fourteen cells, a gutter, the tagline and a margin. The
	// short tagline below is deliberate: this line exists because the terminal
	// is too narrow, so the full tagline would not fit either.
	for _, tc := range []struct {
		name  string
		width int
		full  bool
	}{
		{"one cell short", 57, false},
		{"exactly enough", 58, true},
		{"a phone", 40, false},
		{"nothing at all", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out := render(tc.width)
			ring := strings.Contains(out, "\x1b[48;2;218;56;57m")
			if ring != tc.full {
				t.Errorf("at %d cells the mark %s a ring", tc.width, held(ring))
			}
			if tc.full {
				return
			}
			if !strings.Contains(out, "OC Transpo schedules\x1b[0m") {
				t.Errorf("at %d cells the one line does not carry the short tagline:\n%q", tc.width, out)
			}
			if n := strings.Count(out, "\n"); n != 2 {
				t.Errorf("at %d cells the mark is %d lines, want one with a blank line above it", tc.width, n)
			}
		})
	}
}
