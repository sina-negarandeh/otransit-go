package render

import (
	"strconv"
	"strings"
)

// The escapes this program writes, and the whole list of them.
//
// There is no alternate screen here and there never will be. This program draws
// inline, under whatever the terminal already holds, and a program that takes
// the whole screen cannot be scrolled back to.
const (
	// reset turns everything off again. It is written at the end of every run
	// rather than tracked, so no row can inherit a colour from the row above it.
	reset = "\x1b[0m"
	// Up moves the cursor to the top of the viewport, and hide and show bracket
	// a redraw so a person never sees it move.
	up   = "\x1b[%dA"
	hide = "\x1b[?25l"
	show = "\x1b[?25h"
	// clearRow wipes from the cursor to the end of the line. A row is drawn over
	// what was there, and the tail is cleared rather than padded, so a shorter
	// row cannot leave the end of a longer one behind.
	clearRow = "\x1b[K"
)

// ANSI writes the buffer the way a terminal is told to draw it: every row, with
// the escape that turns a style on where a run begins and the one that turns it
// off where the run ends.
//
// This is the third way to write one buffer out. String is what a person reads in
// a diff, Styles is what two implementations are compared on, and this is what a
// terminal receives. All three read the same cells, so a screen cannot look one
// way in an artifact and another on a terminal.
func (b *Buffer) ANSI() string {
	var out strings.Builder
	for row := range b.cells {
		if row > 0 {
			// A carriage return as well as a newline. In raw mode the terminal
			// adds no return of its own, and without this every row would start
			// one further right than the last.
			out.WriteString("\r\n")
		}
		b.ansiRow(&out, row)
		out.WriteString(clearRow)
	}
	return out.String()
}

// ansiRow writes one row as runs of one style.
func (b *Buffer) ansiRow(out *strings.Builder, row int) {
	for at := 0; at < b.w; {
		style := b.styles[row][at]
		end := at + 1
		for end < b.w && b.styles[row][end] == style {
			end++
		}

		if on := sgr(style); on != "" {
			out.WriteString(on)
			out.WriteString(string(b.cells[row][at:end]))
			out.WriteString(reset)
		} else {
			out.WriteString(string(b.cells[row][at:end]))
		}
		at = end
	}
}

// sgr is the escape that turns a style on, and nothing at all for a style that
// asks for nothing.
func sgr(s Style) string {
	var codes []string
	for _, m := range []struct {
		on   bool
		code string
	}{
		{s.Bold, "1"}, {s.Dim, "2"}, {s.Italic, "3"},
		{s.Underline, "4"}, {s.Reverse, "7"}, {s.Strike, "9"},
	} {
		if m.on {
			codes = append(codes, m.code)
		}
	}
	if rgb, ok := channels(s.Fg); ok {
		codes = append(codes, "38;2;"+rgb)
	}
	if rgb, ok := channels(s.Bg); ok {
		codes = append(codes, "48;2;"+rgb)
	}
	if len(codes) == 0 {
		return ""
	}
	return "\x1b[" + strings.Join(codes, ";") + "m"
}

// channels reads a colour written #da3839 as the three numbers an escape wants.
//
// Truecolour and not a palette index: the feed gives a route its own colour, and
// rounding route 44's blue to the nearest of sixteen would draw two routes the
// same.
func channels(hex string) (string, bool) {
	if len(hex) != 7 || hex[0] != '#' {
		return "", false
	}
	n, err := strconv.ParseUint(hex[1:], 16, 32)
	if err != nil {
		return "", false
	}
	return strconv.FormatUint(n>>16&0xff, 10) + ";" +
		strconv.FormatUint(n>>8&0xff, 10) + ";" +
		strconv.FormatUint(n&0xff, 10), true
}
