// Package logo draws the startup mark: the circle on a pole you look for at an
// O-Train entrance, with the name set beside it.
//
// It goes to stdout before the inline viewport is claimed, so it lands in
// scrollback and is never repainted.
//
// Every cell is a space carrying a background colour, and never a block glyph.
// SF Mono's U+2588 does not fill the cell and the font has no shade characters
// at all, so macOS substitutes a font with different metrics and the picture
// shears. Paint is drawn by the terminal, not by the font.
package logo

import (
	"fmt"
	"io"
	"math"
	"strings"
)

// The mark comes out of these five, so it is derived rather than drawn.
const (
	// ringRows is how tall the ring is. Its width follows from aspect.
	ringRows = 7
	// thickness is how much of the radius the ring itself takes.
	thickness = 0.42
	poleRows  = 2
	poleW     = 2
	// aspect is two because a terminal cell is about twice as tall as it is
	// wide. Without it the ring is an ellipse.
	aspect = 2.0
	// gutter is the blank between the mark and the words beside it.
	gutter = 3
)

// The colours. The ground is the same grey as the two rules the app draws: a
// mark standing on a different grey would not look like it was standing on the
// app.
const (
	red = "218;56;57"
	// poleGrey and muted are the same grey under two names, because they are
	// two different jobs: the pole is painted in one and the tagline is written
	// in the other.
	poleGrey = "109;110;112"
	muted    = "109;110;112"
	ground   = "58;59;61"
)

const (
	name         = "otransit"
	tagline      = "OC Transpo schedules in your terminal"
	taglineShort = "OC Transpo schedules"
)

const (
	reset = "\x1b[0m"
	bold  = "\x1b[1m"
)

// Print writes the mark for the banner above the viewport.
//
// It draws no rule. The viewport's top edge is the rule the pole stands on, and
// that one repaints, carries the weather, and stays on screen after the mark
// has scrolled away.
func Print(w io.Writer, width int) error {
	_, err := io.WriteString(w, render(width))
	return err
}

// PrintAlone writes the mark with the rule it stands on, for when nothing
// follows it.
//
// `otransit logo` opens no viewport, so nothing else will draw that line.
// Without this the pole ends in mid air, which is the whole design failing on
// the one command whose only job is to show it.
func PrintAlone(w io.Writer, width int) error {
	_, err := io.WriteString(w, render(width)+rule(width))
	return err
}

// A cell of the grid is empty, part of the ring, or part of the pole.
type cell byte

const (
	empty cell = iota
	ring
	pole
)

// build is the mark as a grid of cells: the ring, and the pole hanging below it.
func build() [][]cell {
	ry := float64(ringRows) / 2
	rx := ry * aspect
	width := int(math.Round(rx * 2))
	cy, cx := float64(ringRows-1)/2, float64(width-1)/2

	grid := make([][]cell, 0, ringRows+poleRows)
	for y := range ringRows {
		row := make([]cell, width)
		for x := range width {
			dx := (float64(x) - cx) / rx
			dy := (float64(y) - cy) / ry
			if d := math.Sqrt(dx*dx + dy*dy); d >= 1-thickness && d <= 1 {
				row[x] = ring
			}
		}
		grid = append(grid, row)
	}

	// The pole hangs from the middle of the ring. The odd half cell goes left,
	// which is where the mark's own centre is.
	start := int(cx - poleW/2 + 0.5)
	for range poleRows {
		row := make([]cell, width)
		for x := start; x < min(start+poleW, width); x++ {
			row[x] = pole
		}
		grid = append(grid, row)
	}
	return grid
}

// render is the mark as it will appear, escapes and all. It is separate from
// Print so a test can read it: what Print writes goes to stdout before the
// viewport exists, where nothing else can see it.
func render(width int) string {
	grid := build()
	markW := len(grid[0])

	// The mark, the gutter, the tagline, and a margin so the words do not end
	// against the edge.
	if width < markW+gutter+len([]rune(tagline))+4 {
		// The short tagline is deliberate. This line exists because the terminal
		// is too narrow for the mark, so it is too narrow for the full tagline.
		return "\n" + fg(red) + bold + name + reset + "  " + fg(muted) + taglineShort + reset + "\n"
	}

	// The words sit against the middle of the ring and ignore the pole, which
	// hangs below the picture.
	nameRow := ringRows/2 - 1

	var b strings.Builder
	b.WriteString("\n")
	for y, row := range grid {
		b.WriteString("  ")
		// A terminal keeps the colour it was given, so the colour is named only
		// where it changes. An empty cell has no colour rather than a colour of
		// its own, which is what lets the mark sit on whatever the terminal's
		// own background is, and a row starts with none.
		on := ""
		for _, c := range row {
			want := colourOf(c)
			if want != on {
				if want == "" {
					b.WriteString(reset)
				} else {
					b.WriteString(bg(want))
				}
				on = want
			}
			b.WriteByte(' ')
		}
		b.WriteString(reset)
		b.WriteString(strings.Repeat(" ", gutter))

		switch y {
		case nameRow:
			b.WriteString(fg(red) + bold + name + reset)
		case nameRow + 2:
			b.WriteString(fg(muted) + tagline + reset)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// colourOf is what a cell is painted in, and empty for a cell that is painted
// in nothing.
func colourOf(c cell) string {
	switch c {
	case ring:
		return red
	case pole:
		return poleGrey
	}
	return ""
}

// rule is the line the pole stands on, the full width of the terminal.
func rule(width int) string {
	if width <= 0 {
		return ""
	}
	return fg(ground) + strings.Repeat("─", width) + reset + "\n"
}

func fg(colour string) string { return fmt.Sprintf("\x1b[38;2;%sm", colour) }
func bg(colour string) string { return fmt.Sprintf("\x1b[48;2;%sm", colour) }
