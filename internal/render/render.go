// Package render draws a screen into a buffer of fixed size.
//
// A screen is a rule, a content area, a rule and a status bar. The content
// area is anchored to its bottom, so a list of two rows sits just above the
// lower rule and grows upward.
//
// Nothing here queries anything. A View is a value, so a test can build one
// and read back what was drawn.
package render

import (
	"strings"
)

// The glyphs this package draws. Every one occupies a single cell. A
// double-width glyph would shift a whole row and nothing would say so.
const (
	rule   = '─'
	marker = '❯'
	cut    = '…'
)

// Columns this package fixes. Which column a screen puts a piece of text in
// is the screen's business, and it arrives in a Cell.
const (
	markerCol = 1
	emptyCol  = 2
	// gap separates one part of the status bar from the next.
	gap = 3
	// trailGap is how many cells a trail keeps clear of the hints when it has
	// more than one crumb. Measured against the reference:
	// a trail ending two cells short of them is drawn, and one
	// ending a single cell short loses its leading crumb instead.
	trailGap = 2
	// crumb separates one part of a trail from the next.
	crumb = " › "
	// gapText is the gap as it is drawn. It is written out rather than built,
	// and the test that counts it against gap is what holds the two together.
	gapText = "   "
	// rowLimit is how many entries a screen draws, however tall the terminal
	// is. It does not bound what a screen holds: a search of 43 stops reports
	// all 43 and draws these.
	rowLimit = 8
)

// Cell is one piece of text in a row: where it starts, what it says, and which
// cells its style paints.
//
// A column does not move with the terminal. At 44 cells a cell stays where it is
// and its text gives way instead.
type Cell struct {
	// At is the column the text starts in.
	At   int
	Text string
	// Style paints Paints, or what the text takes when that span is empty.
	Style  Style
	Paints Span
	// Room is the most cells the text may take, and zero means the rest of the
	// row. Text longer than its room is cut with an ellipsis, which is how a
	// column says it gave way. The screen's edge clips instead and says nothing,
	// because a column that had room and ran off the screen did not give way:
	// the screen ended.
	Room int
}

// Span is a range of columns, from inclusive to To exclusive. The zero Span is
// empty, and a cell carrying one paints what its text takes.
type Span struct{ From, To int }

// empty reports whether a span covers no cells.
func (s Span) empty() bool { return s.From >= s.To }

// Row is one entry of a list.
type Row struct {
	Cells []Cell
	// Style is laid under every cell of the row, across its whole width. A
	// selected row is bold from end to end and not only where its text is.
	Style Style
}

// Status is the bar along the bottom: a label, a trail, an extra part, then
// the hints against the right edge. Each begins three cells after the one
// before, so a part carrying a trailing space pushes the next one along.
type Status struct {
	// Label names the screen.
	Label string
	// Trail says what the screen is about, innermost last. Each crumb carries
	// its own style, because a route keeps its badge wherever it sits.
	//
	// Leading crumbs are dropped until what is left fits beside the hints, so
	// a long stop name costs the mode and then the route rather than running
	// into them. The last crumb is always kept.
	Trail []Crumb
	// Extra follows the trail, which is where a filter goes.
	Extra string
	// Hints sit against the right edge.
	Hints string
	// Paint is the styles the bar draws with.
	Paint Paint
}

// Crumb is one part of a status trail.
type Crumb struct {
	Text  string
	Style Style
}

// Paint is how each part of a status bar is drawn. A crumb brings its own.
type Paint struct {
	// Separator styles what sits between crumbs, and the gap before the trail.
	Label, Separator, Extra, Hints Style
}

// View is everything a screen draws.
type View struct {
	Width, Height int
	Rows          []Row
	// Split divides the content area. The rows before it are drawn from the
	// top and the rest are anchored to the bottom, which is how the first
	// screen puts pins above the modes. Selected indexes the whole of Rows.
	Split    int
	Selected int
	// Cursor draws the marker beside the selected row. A departures board is
	// not a list anyone moves through, so it draws none.
	Cursor bool
	// Empty is drawn in place of the list when Rows is empty.
	Empty string
	// EmptyStyle draws that message.
	EmptyStyle Style
	// Rule draws the two rules.
	Rule Style
	// Banner sits at the right of the upper rule, one cell short of the edge,
	// which is where the weather goes.
	Banner      string
	BannerStyle Style
	// Notice is a warning drawn on the first row of the content area, where a
	// screen has something to say before you read the rows. The content is
	// anchored to the bottom, so at every height a fixture uses the two do not
	// meet. Nothing draws a notice and a group of pins at once.
	Notice      string
	NoticeStyle Style
	Status      Status
}

// Buffer is a grid of cells. Every row holds exactly Width runes.
type Buffer struct {
	w, h   int
	cells  [][]rune
	styles [][]Style
}

// newBuffer returns a buffer of spaces.
func newBuffer(w, h int) *Buffer {
	b := &Buffer{w: w, h: h, cells: make([][]rune, h), styles: make([][]Style, h)}
	for i := range b.cells {
		b.cells[i] = []rune(strings.Repeat(" ", w))
		b.styles[i] = make([]Style, w)
	}
	return b
}

// text writes s at row and col. Text that would run past the right edge is cut,
// and the last cell it keeps becomes an ellipsis.
func (b *Buffer) text(row, col int, s string) {
	b.styled(row, col, s, Style{}, 0)
}

// paint lays a style over the cells from first up to last, leaving the text
// alone.
func (b *Buffer) paint(row, first, last int, style Style) {
	if row < 0 || row >= b.h || style.plain() {
		return
	}
	if first < 0 {
		first = 0
	}
	if last > b.w {
		last = b.w
	}
	for i := first; i < last; i++ {
		b.styles[row][i] = b.styles[row][i].merge(style)
	}
}

// styled writes s at row and col in one style. width widens the styled region
// past the text, which is how a badge gets air on each side.
//
// The screen's edge clips and leaves no mark. What says a piece of text was
// shortened is its column, and Cut is applied there: a note that fits its ten
// cells and then runs off a narrow screen reads sc, not s and an ellipsis.
func (b *Buffer) styled(row, col int, s string, style Style, width int) {
	if row < 0 || row >= b.h || col >= b.w {
		return
	}
	// A column can come out negative when a right-hand cell is placed past the
	// terminal. Writing from the left edge keeps the row the right width.
	if col < 0 {
		col = 0
	}
	// The row itself bounds the write: copy stops at the end of the destination,
	// and the style loop below stops at the same edge. Text that runs past the
	// edge is therefore clipped and leaves no mark, which is what a column that
	// had room and ran off a narrow screen should look like.
	runes := []rune(s)
	copy(b.cells[row][col:], runes)

	if style.plain() {
		return
	}
	span := len(runes)
	if width > span {
		span = width
	}
	for i := col; i < col+span && i < b.w; i++ {
		b.styles[row][i] = b.styles[row][i].merge(style)
	}
}

// fill writes r across the whole of row in one style.
func (b *Buffer) fill(row int, r rune, style Style) {
	if row < 0 || row >= b.h {
		return
	}
	for i := range b.cells[row] {
		b.cells[row][i] = r
		b.styles[row][i] = b.styles[row][i].merge(style)
	}
}

// under lays a style beneath the whole of row, leaving the text alone.
func (b *Buffer) under(row int, style Style) {
	if row < 0 || row >= b.h || style.plain() {
		return
	}
	for i := range b.styles[row] {
		b.styles[row][i] = b.styles[row][i].merge(style)
	}
}

// String returns the buffer, one line per row, each ending in a newline.
func (b *Buffer) String() string {
	var out strings.Builder
	for _, row := range b.cells {
		out.WriteString(string(row)) //nolint:errcheck // a Builder write cannot fail
		out.WriteByte('\n')          //nolint:errcheck // a Builder write cannot fail
	}
	return out.String()
}

// layout names the rows of a screen at one height.
//
// A screen is built greedily from the top. The upper rule comes first, then a
// single row of content, then the lower rule, then the status bar. Every row
// past those four widens the content area. A terminal too short for all of
// them keeps what comes first and loses the rest.
//
// Every fixture is fourteen rows tall, so no comparison reaches this ladder.
// It was read off the reference implementation at heights 1 to 5.
type layout struct {
	// first and last bound the content area. last sits below first when there
	// is no room for any content at all.
	first, last int
	// top, bottom and status are row numbers, or -1 where the screen is too
	// short to hold that part.
	top, bottom, status int
}

func newLayout(h int) layout {
	// first above last is an empty content area.
	l := layout{first: 1, last: 0, top: -1, bottom: -1, status: -1}
	if h < 1 {
		return l
	}

	l.top = 0
	left := h - 1

	rows := 0
	if left > 0 {
		rows, left = 1, left-1
	}
	bottom := left > 0
	if bottom {
		left--
	}
	status := left > 0
	if status {
		left--
	}
	// Whatever is left over belongs to the content area.
	rows += left

	l.last = rows
	if bottom {
		l.bottom = 1 + rows
	}
	if status {
		l.status = 2 + rows
	}
	return l
}

// rows is the height of the content area.
func (l layout) rows() int { return l.last - l.first + 1 }

// Draw renders v.
func Draw(v View) *Buffer {
	b := newBuffer(v.Width, v.Height)
	l := newLayout(v.Height)

	if l.top >= 0 {
		b.fill(l.top, rule, v.Rule)
		if air := " " + v.Banner; v.Banner != "" && v.Width-len([]rune(air)) >= 1 {
			// Against the right edge, with a cell of air before it. The air is
			// drawn and not left as rule, and the style covers it, so the two
			// read as one run.
			//
			// It needs a rule to sit on: a terminal with room for the label and
			// nothing else draws the rule plain. Cutting the label instead would
			// leave a shorter, wronger condition, and this program would rather
			// say nothing than say something it does not mean.
			b.styled(l.top, v.Width-len([]rune(air)), air, v.BannerStyle, 0)
		}
	}
	if l.bottom >= 0 {
		b.fill(l.bottom, rule, v.Rule)
	}
	if v.Notice != "" && l.rows() > 0 {
		// The style starts at the left edge and not at the glyph, which is how
		// the empty-list message is drawn too.
		b.styled(l.first, 0, " "+v.Notice, v.NoticeStyle, 0)
		// The row is spent. The content is anchored to the bottom and never
		// reaches it at the heights a fixture uses, but a group drawn from the
		// top would, and one that shared this row would lose its first entry
		// under the warning.
		l.first++
	}
	drawContent(b, v, l)
	if l.status >= 0 {
		drawStatus(b, v.Status, l.status, v.Width)
	}
	return b
}

// drawContent fills the content area. Rows before the split are drawn from the
// top and the rest are anchored to the bottom.
func drawContent(b *Buffer, v View, l layout) {
	if l.rows() <= 0 {
		return
	}
	if len(v.Rows) == 0 {
		if v.Empty != "" {
			// The style starts at the left edge, not at the text.
			b.styled(l.last, 0, strings.Repeat(" ", emptyCol), v.EmptyStyle, 0)
			b.styled(l.last, emptyCol, v.Empty, v.EmptyStyle, 0)
		}
		return
	}

	row := l.first
	for i, r := range v.Rows[:v.Split] {
		if row > l.last {
			return
		}
		drawRow(b, r, row, v.Cursor && i == v.Selected)
		row++
	}

	rest := v.Rows[v.Split:]
	room := l.last - row + 1
	if room > rowLimit {
		room = rowLimit
	}
	if room <= 0 {
		return
	}

	shown, selected := window(rest, v.Selected-v.Split, room)
	first := l.last - len(shown) + 1
	for i, r := range shown {
		drawRow(b, r, first+i, v.Cursor && i == selected)
	}
}

func drawRow(b *Buffer, r Row, row int, cursor bool) {
	b.under(row, r.Style)
	if cursor {
		b.text(row, markerCol, string(marker))
	}
	for _, c := range r.Cells {
		// The room, or the rest of the row when the column did not say. Cutting
		// at the end of a row is what leaves the ellipsis a reader sees on a
		// narrow terminal.
		room := c.Room
		if room == 0 {
			room = b.w - c.At
		}
		text := Cut(c.Text, room)

		// A column wider than the text in it says so. Whoever owns the column
		// works that span out, because only they know how far it reaches.
		paints := c.Paints
		if paints.empty() {
			paints = Span{From: c.At, To: c.At + len([]rune(text))}
		}
		b.text(row, c.At, text)
		b.paint(row, paints.From, paints.To, c.Style)
	}
}

// window returns the rows that fit and where the selection sits among them.
//
// No fixture fills the content area, so which rows a longer list shows is not
// settled by the contract. What is settled is that the cursor must be on one
// of them: keeping the first rows and dropping the rest drew a list with no
// cursor anywhere once the selection passed the last visible row.
func window(rows []Row, selected, room int) ([]Row, int) {
	if len(rows) <= room {
		return rows, selected
	}
	start := 0
	if selected >= room {
		start = selected - room + 1
	}
	if start > len(rows)-room {
		start = len(rows) - room
	}
	return rows[start : start+room], selected - start
}

// piece is one part of the left of a status bar: the separator that comes
// before it, then the text.
//
// The separator belongs to the piece and not to the piece before it, because a
// piece that is dropped takes its separator with it, and whichever piece is
// first draws none.
type piece struct {
	sep      string
	sepStyle Style
	text     string
	style    Style
}

// tail is everything on the left of a status bar that follows the label: the
// crumbs of the trail, and then the filter. It is the part of the bar that
// gives way, so what a narrowing terminal does to it lives here as methods.
type tail []piece

// tailOf reads the tail off a status bar.
func tailOf(s Status) tail {
	var out tail
	for _, c := range s.Trail {
		out = append(out, piece{sep: crumb, sepStyle: s.Paint.Separator, text: c.Text, style: c.Style})
	}
	if s.Extra != "" {
		// Spaces and not a crumb mark, and they take no colour. What was typed
		// is not another step of the trail.
		out = append(out, piece{sep: gapText, text: s.Extra, style: s.Paint.Extra})
	}
	return out
}

// drawStatus draws the bar at the foot of a screen.
//
// The bar gives way from the right as the terminal narrows, and this ladder was
// measured off the reference rather than reasoned out. The hints keep the place
// they want until the left side reaches them, and the edge then takes what is
// past it with nothing to say so.
//
// The part next to the hints is the one that shortens. A trail drops its
// leading crumbs one at a time, and whatever is left is cut to the cells
// between where it starts and where the hints are. The label never shortens:
// every screen that has one has a trail after it, and the one screen that has
// no trail asks its question in the trail's place instead.
func drawStatus(b *Buffer, s Status, row, width int) {
	// Where the hints would rather be: one cell short of the right edge, which
	// matches the cell of margin on the left.
	want := width
	if s.Hints != "" {
		want = width - 1 - len([]rune(s.Hints))
	}

	tail := tailOf(s)
	col := 1
	if s.Label != "" {
		// The label is drawn whole, and only the edge takes anything off it.
		// What gives way is the tail, which is why the first screen puts its
		// question there and carries no label at all.
		b.styled(row, col, s.Label, s.Paint.Label, 0)
		col += len([]rune(s.Label))
		// The three cells after the label are always spent, because the column
		// the trail starts in does not move: not with the terminal, and not
		// with whether a crumb survived to sit there.
		//
		// They are the trail's first separator, so a bar with a trail paints
		// them. A bar whose only field is what was typed has no trail to
		// separate and leaves them bare.
		if len(s.Trail) > 0 {
			b.styled(row, col, gapText, s.Paint.Separator, 0)
		}
		col += gap
	}

	tail = tail.fit(col, want)
	if len(tail) == 1 {
		// One piece left is the one beside the hints, so it is cut to the room
		// between them. Cut to nothing it draws nothing, and the column it
		// would have started at is where the hints go.
		tail[0].text = Cut(tail[0].text, want-col-1)
	}
	for i, p := range tail {
		if i > 0 {
			b.styled(row, col, p.sep, p.sepStyle, 0)
			col += len([]rune(p.sep))
		}
		b.styled(row, col, p.text, p.style, 0)
		col += len([]rune(p.text))
	}

	if s.Hints != "" {
		// Their place until the left side arrives, and one cell past it after
		// that. The style runs to the edge, so the margin reads as one run.
		at := max(col+1, want)
		b.styled(row, at, s.Hints, s.Paint.Hints, width-at)
	}
}

// Cut is as much of s as fits in room, ending in an ellipsis when it had to
// give something up. Nothing fits in no room at all.
//
// Exported because a column narrow enough to cut its text is decided by whoever
// owns the columns, and the mark it leaves is decided here. One rule, so a name
// cut to its column and a name cut by the terminal edge read the same.
func Cut(s string, room int) string {
	runes := []rune(s)
	switch {
	case room <= 0:
		return ""
	case len(runes) <= room:
		return s
	default:
		return string(append(runes[:room-1:room-1], cut))
	}
}

// fit drops the leading pieces of a tail, one at a time, until what is left
// clears the hints. The last one is never dropped here: it is the piece the
// screen is about, and drawStatus cuts it to the room instead.
func (t tail) fit(col, want int) tail {
	for len(t) > 1 {
		if col+t.cells()+trailGap <= want {
			break
		}
		t = t[1:]
	}
	return t
}

// cells is how many cells a tail takes, separators included.
func (t tail) cells() int {
	n := 0
	for i, p := range t {
		// Runes, not bytes: the crumb mark is three cells and five bytes.
		if i > 0 {
			n += len([]rune(p.sep))
		}
		n += len([]rune(p.text))
	}
	return n
}
