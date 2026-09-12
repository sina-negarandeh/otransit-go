package render

import (
	"strconv"
	"strings"
)

// Style is how a cell is drawn.
//
// The zero value is the terminal's own colours with nothing turned on, and a
// run of it is left out of the artifact: what is compared is what the program
// chose, not the absence of a choice.
type Style struct {
	// Fg and Bg are hex colours like "#da3839", or empty for the terminal's.
	Fg, Bg string
	// The modifiers, written after a plus in the order b d i u r x.
	Bold, Dim, Italic, Underline, Reverse, Strike bool
}

// plain reports whether the style asks for nothing.
func (s Style) plain() bool { return s == Style{} }

// String writes a style the way the artifact does: a foreground, a background
// after a slash when there is one, then the modifiers after a plus. A style
// with no foreground writes a dash, because a run can be a modifier alone.
func (s Style) String() string {
	var b strings.Builder
	if s.Fg == "" {
		b.WriteString("-")
	} else {
		b.WriteString(s.Fg)
	}
	if s.Bg != "" {
		b.WriteString("/" + s.Bg)
	}

	mods := ""
	for _, m := range []struct {
		on   bool
		mark string
	}{
		{s.Bold, "b"}, {s.Dim, "d"}, {s.Italic, "i"},
		{s.Underline, "u"}, {s.Reverse, "r"}, {s.Strike, "x"},
	} {
		if m.on {
			mods += m.mark
		}
	}
	if mods != "" {
		b.WriteString("+" + mods)
	}
	return b.String()
}

// merge lays one style over another. A field the top style leaves empty keeps
// what is underneath, and a modifier it turns on stays on.
func (s Style) merge(over Style) Style {
	if over.Fg != "" {
		s.Fg = over.Fg
	}
	if over.Bg != "" {
		s.Bg = over.Bg
	}
	s.Bold = s.Bold || over.Bold
	s.Dim = s.Dim || over.Dim
	s.Italic = s.Italic || over.Italic
	s.Underline = s.Underline || over.Underline
	s.Reverse = s.Reverse || over.Reverse
	s.Strike = s.Strike || over.Strike
	return s
}

// Styles writes the styles block: one line per row that carries any style, as
// maximal runs.
//
// Merging every neighbour that matches leaves one way to write a row, so two
// implementations are compared on the styles they chose and on nothing else.
func (b *Buffer) Styles() string {
	var out strings.Builder
	for row := range b.styles {
		line := runsOf(b.styles[row], b.w)
		if line == "" {
			continue
		}
		out.WriteString(pad(row) + "  " + line + "\n")
	}
	return out.String()
}

// pad right-aligns a row number in three cells, which is how the artifact
// writes it.
func pad(row int) string {
	n := strconv.Itoa(row)
	for len(n) < 3 {
		n = " " + n
	}
	return n
}

func runsOf(styles []Style, width int) string {
	var runs []string
	for at := 0; at < width; {
		if styles[at].plain() {
			at++
			continue
		}
		end := at + 1
		for end < width && styles[end] == styles[at] {
			end++
		}
		runs = append(runs, strconv.Itoa(at)+".."+strconv.Itoa(end)+" "+styles[at].String())
		at = end
	}
	return strings.Join(runs, "  ")
}
