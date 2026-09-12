// Where each screen puts its text.
//
// Most columns do not move with the terminal. Three rows have more fields than
// the narrow end of a terminal can hold, and those share out what there is by
// integer arithmetic against a floor and a ceiling. The arithmetic is written
// down in conformance/README.md, and both implementations reproduce it exactly,
// non-monotonic where the divisions and the clamps disagree.
package app

// column is where a field starts and how much room it has.
type column struct{ at, room int }

// The fixed columns. These do not move with the terminal, and each one carries
// the room it has: a column with another after it holds the cells between them,
// and a column that ends the row leaves its room at zero and takes what is left.
//
// The three row shapes that can run out of room are below.
var (
	// The two fields of a row on the first screen.
	modeLabel = column{at: 3, room: 37 - columnGap - 3}
	modeCount = column{at: 37}

	// The two fields of a route row.
	routeBadge = column{at: 5, room: badgeWidth}
	routeName  = column{at: 11}

	// The three fields of a row on a list reached by drilling: the guide down
	// the left, the name, and the pole number.
	treeGuide = column{at: 3, room: guideWidth}
	treeLabel = column{at: 5, room: 39 - columnGap - 5}
	treeCode  = column{at: 39}
)

const (
	// boardRows is how many departures a board draws, which is the whole of the
	// content area.
	boardRows = 8
	// boardFetch is how many it asks the cache for. A prediction moves a row
	// along the axis, so the board is re-ordered after the query: a bus running
	// late can truly arrive after one scheduled behind it. Without the headroom
	// the board can leave out the next bus a person could actually catch.
	boardFetch = boardRows * 3
	// pinFetch is how many a pin row asks for. It draws the first one, and the
	// same re-ordering decides which that is.
	pinFetch = 8
	// maxPins is how many boards may be kept. The first screen is what decides
	// it: two rows go to the modes and one to the blank between the two groups,
	// and that blank is what says they are different kinds of thing. A cap one
	// higher would let the pins meet the modes exactly when somebody used the
	// feature properly.
	maxPins = boardRows - 3

	// markerWidth is how many cells the cursor column covers. The style sits
	// there whether or not the cursor is in it.
	markerWidth = 3
	// afterMidnightWidth is the note a borrowed trip carries, drawn in the
	// status column's own colour so the two read as one run.
	afterMidnightWidth = 14
	// home is the key that goes back to the first screen. It is not a letter,
	// so it can mean something on a list where every letter goes to the filter.
	home = '/'
	// badgePad is how far a route badge reaches left of its number, and
	// badgeWidth how wide the whole badge is.
	badgePad   = 2
	badgeWidth = 5
	// guideWidth is the tree line and the cell after it.
	guideWidth = 2
)

// headsignRoom is how wide the headsign column of a board is. It is the only
// field there that gives way, and above 71 cells it has stopped giving.
func headsignRoom(width int) int { return clamp(width-47, 8, 24) }

// boardColumns is the fields of a departures board at one terminal width.
//
// A board reached by drilling leaves the headsign out, because the direction
// already chose it, and then nothing on the row can run out of room: those
// columns are the same at every width. A board reached from a search shows the
// headsign, which is the one field there that gives way, and every column after
// it moves along.
//
// A headsign with no room is a board that leaves it out. That is the whole of
// the difference between the two, and it is decided here rather than carried
// into the row as a flag that shifts what every later column means.
type boardColumns struct {
	badge, headsign, clock, wait, note column
}

func boardLayout(width int, drilled bool) boardColumns {
	const (
		badge  = 5
		first  = 11
		clockW = 5
		waitW  = 10
		noteW  = 10
		// The gap before the countdown and before the note, one wider than the
		// gap between the columns before them.
		air = 3
	)

	c := boardColumns{badge: column{badge, badge}}
	c.clock = column{first, clockW}
	if !drilled {
		room := headsignRoom(width)
		c.headsign = column{first, room}
		c.clock = column{first + room + columnGap, clockW}
	}
	// A right-hand column is named by where it ends.
	c.wait = column{c.clock.at + clockW + air + waitW, waitW}
	c.note = column{c.wait.at + air, noteW}
	return c
}

// columnGap is the blank between one column and the next, wherever a layout is
// worked out rather than written down.
const columnGap = 2

// clamp holds v between a floor and a ceiling.
func clamp(v, low, high int) int { return max(low, min(high, v)) }

// searchColumns is the four fields of a search row at one terminal width.
//
// They compete for the room and three of them give way as it runs out. Each
// width is integer arithmetic against a floor and a ceiling, and two divisions
// meeting a clamp do not have to move in step with the terminal: the name is
// thirteen cells at 49 and twelve at 50. That is what the other implementation
// draws at every width measured from 44 to 105, and a port that smoothed it out
// would be wrong at nine widths in twenty.
//
// No fixture is both narrow and on a search screen, which is the reason to
// reproduce the arithmetic rather than keep columns that agree at a hundred
// cells and nowhere else.
type searchColumns struct {
	name, code, toward, routes column
}

func searchLayout(width int) searchColumns {
	const code = 6
	toward := clamp(width*24/100, 10, 22)
	routes := clamp(width*22/100, 8, 26)
	// The name takes what is left of the row, which is what makes it the field
	// that moves when the other two land on a bound.
	name := clamp(width-(markerWidth+3*columnGap+code+toward+routes), 12, 36)

	var c searchColumns
	c.name = column{markerWidth, name}
	c.code = column{c.name.at + name + columnGap, code}
	c.toward = column{c.code.at + code + columnGap, toward}
	c.routes = column{c.toward.at + toward + columnGap, routes}
	return c
}

// pinColumns is the six fields of a pin row at one terminal width.
//
// The name and the destination divide what the fixed fields leave, and the name
// wins the wider half because the name identifies the pin: a person scanning
// their pins is looking for the stop, and the destination only tells two pins on
// one stop apart.
type pinColumns struct {
	name, pole, badge, headsign, wait, note column
}

func pinLayout(width int) pinColumns {
	const (
		pole  = 6
		badge = 5
		wait  = 10
		note  = 9
	)
	// Five gaps between the six fields, and the cursor's column in front.
	share := width - (markerWidth + pole + badge + wait + note + 6)
	name := clamp(share*58/100, 10, 28)
	headsign := clamp(share-name, 6, 20)

	var c pinColumns
	c.name = column{markerWidth, name}
	c.pole = column{c.name.at + name + 1, pole}
	// The badge is the column its number sits in, and its colour reaches
	// badgePad cells left of that.
	c.badge = column{c.pole.at + pole + badgePad, badge}
	c.headsign = column{c.pole.at + pole + badge + 1, headsign}
	// A right-hand column is named by where it ends.
	c.wait = column{c.headsign.at + headsign + 1 + wait, wait}
	c.note = column{c.wait.at + 2, note}
	return c
}
