// Formatting for the screens: how a row, a stop name, a clock, a wait and a
// route badge are written. Nothing here knows which screen it is drawing for.
package app

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/render"
)

// nothing is what a list with no rows says. A filter that matched none of them
// is a different thing from a list that was empty to begin with.
func nothing(filter string) string {
	if filter != "" {
		return "no matches"
	}
	return "nothing here"
}

// filled is a cell whose column paints its whole room, so the column keeps its
// width whatever the text in it.
func filled(c column, text string, style render.Style) render.Cell {
	return render.Cell{
		At: c.at, Text: text, Style: style, Room: c.room,
		Paints: render.Span{From: c.at, To: c.at + c.room},
	}
}

// fitted is a cell painting only what its text takes, cut to its column's room.
// A room of zero is the end of the row.
func fitted(c column, text string, style render.Style) render.Cell {
	return render.Cell{At: c.at, Text: text, Style: style, Room: c.room}
}

// routeRows draws a route list: a badge in the route's own colours, then its
// long name.
func routeRows(routes []cache.Route, selected int) []render.Row {
	out := make([]render.Row, 0, len(routes))
	for i, r := range routes {
		row := render.Row{Cells: []render.Cell{
			badgeCell(r.ShortName, r.Colour, routeBadge),
			fitted(routeName, r.LongName, muted),
		}}
		markAndSelect(&row, selected == i)
		out = append(out, row)
	}
	return out
}

// markAndSelect marks the row and bolds it when it is the selected one.
func markAndSelect(row *render.Row, selected bool) {
	mark(row)
	if selected {
		// A selected row is bold from end to end and not only where its text
		// is.
		row.Style = render.Style{Bold: true}
	}
}

// mark puts the brand colour in the marker column, which carries it whether or
// not the cursor is there.
func mark(row *render.Row) {
	row.Cells = append([]render.Cell{
		{At: 0, Style: bold(brand), Paints: render.Span{From: 0, To: markerWidth}},
	}, row.Cells...)
}

func matches(filter string, fields ...string) bool {
	if filter == "" {
		return true
	}
	want := strings.ToLower(filter)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), want) {
			return true
		}
	}
	return false
}

// station is a stop named as itself.
//
// A rail platform's name carries the line after the station, and that is noise
// to a person reading a list of stops: HURDMAN O-TRAIN EAST / EST is Hurdman.
// This is how a stop is named everywhere except a search, which has a column
// for the platform and says which one.
func station(name string) string {
	// The cut needs the trailing space: the platform a direction is not written
	// on is named `TUNNEY'S PASTURE O-TRAIN`, and it keeps every word. What goes
	// is the direction after it, as in `HURDMAN O-TRAIN EAST / EST`.
	if i := strings.Index(name, " O-TRAIN "); i >= 0 {
		return name[:i]
	}
	return name
}

// withoutPlatform is a station with its platform taken off the end. It removes
// nothing when the stop has no platform, whatever the name ends in: CANTERBURY
// / AD. 860 holds a municipal address and no platform at all.
func withoutPlatform(name, platform string) string {
	name = station(name)
	if platform == "" {
		return name
	}
	return strings.TrimSuffix(name, " "+platform)
}

// withPlatform is how a search names a stop: the station, then which platform.
//
// The two are not the same as the name in the feed. HURDMAN O-TRAIN EAST / EST
// on platform 2 is drawn HURDMAN 2, and HERON 1A (A) on platform 1A is drawn
// HERON 1A (A) 1A, because its name does not end in its platform.
func withPlatform(name, platform string) string {
	base := withoutPlatform(name, platform)
	if platform == "" {
		return base
	}
	return base + " " + platform
}

// clockAt writes a service-day second as a wall clock. It truncates, because a
// clock reads 12:37 until 12:38 arrives. A service day reaches past 24:00 and
// a row borrowed from yesterday is negative, so the second is folded onto the
// clock first.
func clockAt(sec int) string {
	sec = ((sec % 86400) + 86400) % 86400
	return fmt.Sprintf("%02d:%02d", sec/3600, sec%3600/60)
}

// waitMinutes rounds, because a duration takes the nearest minute. That it
// disagrees with clockAt on the same row is deliberate and not a defect: one
// is a clock and the other is a length of time.
func waitMinutes(secs int) int {
	if secs < 0 {
		return -((-secs + 30) / 60)
	}
	return (secs + 30) / 60
}

// waitText writes a wait in minutes. The minute is padded to two digits past
// the hour, which is what sizes the column.
func waitText(mins int) string {
	// A bus leaving this minute is not a quantity of time, and neither is one
	// the feed says went while you were reading. Both read due, and only the
	// line drawn through the second one tells them apart.
	if mins <= 0 {
		return "due"
	}
	if mins < 60 {
		return strconv.Itoa(mins) + " min"
	}
	return fmt.Sprintf("%dh %02d min", mins/60, mins%60)
}

// listHints is what every list screen offers.
//
// No q. On a list every letter goes to the filter, so q cannot quit there, and
// a hint that offers a key the screen does not have is worse than no hint.
const listHints = "↑↓ · ↵ · esc"

// treeRows draws a list under a guide, which is what a screen reached by
// drilling looks like.
// treeRows draws a list under a guide, which is what a screen reached by
// drilling looks like. The guide takes the route's own colour.
func treeRows(rows []Row, selected int, colour string) []render.Row {
	out := make([]render.Row, 0, len(rows))
	for i, r := range rows {
		row := render.Row{Cells: []render.Cell{
			filled(treeGuide, "│", guideStyle(colour)),
			// The name has another column after it, so it is cut to the room
			// between them rather than to the rest of the row. Only those two
			// are told apart when a terminal runs out: a column's room says the
			// text was cut, and the rest of the row is where the screen ended.
			fitted(treeLabel, r.Primary, plainText),
			fitted(treeCode, r.Secondary, muted),
		}}
		markAndSelect(&row, selected == i)
		out = append(out, row)
	}
	return out
}

// badge writes a route with a cell of air on each side, which is why a trail
// carrying one has a wider gap around it than the separator alone would give.
// It is not padded to a width: route 1 is written " 1 " and not "  1 ".
func badge(route string) string {
	return " " + route + " "
}

func trips(n int) string {
	if n == 1 {
		return "1 trip today"
	}
	return strconv.Itoa(n) + " trips today"
}

// pinRow draws a pin: the stop, its pole number, and the next departure.
func pinRow(c choice, selected bool, cols pinColumns) render.Row {
	row := render.Row{Cells: []render.Cell{
		filled(cols.name, c.row.Primary, plainText),
		filled(cols.pole, c.row.Secondary, faint),
	}}

	if c.next == nil {
		// Nothing left today. The note sits where the departure would, one cell
		// before the route's own column.
		row.Cells = append(row.Cells, render.Cell{
			At: cols.badge.at - badgePad - 1, Text: "none left today", Style: muted,
		})
		markAndSelect(&row, selected)
		return row
	}

	text, paint := noteOf(*c.next)
	row.Cells = append(row.Cells,
		badgeCell(c.next.Route, c.next.Colour, cols.badge),
		filled(cols.headsign, c.next.Headsign, muted),
		waitCell(*c.next, cols.wait),
		fitted(cols.note, text, paint))
	markAndSelect(&row, selected)
	return row
}

// rowJSON is how the artifact writes one row of a list.
func rowJSON(r Row) map[string]any {
	return map[string]any{"primary": r.Primary, "secondary": r.Secondary}
}

// listVocabulary is what a screen that is a plain list reports. It is called
// and not inherited, so a screen cannot end up with it by accident.
func listVocabulary(out map[string]any, rows []Row) {
	entries := []any{}
	for _, r := range rows {
		entries = append(entries, rowJSON(r))
	}
	out["rows"] = entries
	out["pins"] = []any{}
}

// nullable writes an empty string as a JSON null, because a pin made from a
// stop has no route and that is not the same as a route named "".
func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// emptyPaint draws the message a list with no rows shows. Its style starts at
// the left edge rather than at the text, which is how the artifact records it.
var emptyPaint = muted

// searchRow draws one stop a search found.
//
// The name arrives in two pieces: the station, and the platform after it. They
// are drawn side by side so the row reads as one name and the platform still
// carries its own colour.
func searchRow(m cache.Match, selected bool, c searchColumns) render.Row {
	// The platform keeps its cells and the station gives way in front of it, so
	// a narrow row reads BILLINGS BR… 4D: which platform is the answer a person
	// came here for, and the station is the part they can already guess.
	room := c.name.room
	if m.Platform != "" {
		room -= 1 + len([]rune(m.Platform))
	}
	base := render.Cut(withoutPlatform(m.Name, m.Platform), room)

	row := render.Row{Cells: []render.Cell{
		fitted(c.name, base, plainText),
	}}
	if m.Platform != "" {
		row.Cells = append(row.Cells, render.Cell{
			At:    c.name.at + len([]rune(base)),
			Text:  " " + m.Platform,
			Style: bold(brand),
		})
	}
	row.Cells = append(row.Cells,
		filled(c.code, "#"+m.Code, faint),
		filled(c.toward, "→ "+m.Toward, muted),
		// The routes end the row and still have a column of their own, so a
		// screen that ends inside it clips them rather than marking them cut.
		fitted(c.routes, strings.Join(m.Routes, ", "), muted))
	markAndSelect(&row, selected)
	return row
}

// boardRow draws one departure. A board reached by drilling leaves the headsign
// out, because the direction already chose it, and its columns close up.
// alert marks the warning line.
//
// It is one cell wide by Unicode's measure, which is all the width test can
// check. Both it and the dash below are East Asian Width Ambiguous rather than
// Narrow, so a terminal set to an East Asian locale draws them two cells wide
// and shifts the row. The other implementation draws the same two.
const alert = "⚠"

// warning is the detour line as it is drawn, and nothing at all when there is no
// detour. The artifact reports the words without the glyph, because the glyph is
// how a screen says it and not part of what was said.
func warning(detour string) string {
	if detour == "" {
		return ""
	}
	return alert + " " + detour
}

// said is what the feed said about a departure, for the artifact: when it
// expects the trip, and how far off the table that is in whole minutes.
//
// Both are nothing when the feed never named the trip. Nothing is not zero: zero
// is a trip it named and put on time, and the artifact keeps the two apart.
func said(d Departure) (live, late any) {
	if !d.Predicted {
		return nil, nil
	}
	return d.Expected, waitMinutes(d.Expected - d.Scheduled)
}

// gone is what stands where a countdown would, on a trip that is not running.
// There is no counting down to a bus that will not come.
const gone = "—"

// noteOf is what a row says about where its time came from, and the colour says
// how much of it to believe.
//
// Within a minute either way is on time: the feed reports to the second and a
// bus is not late by fifteen of them.
func noteOf(d Departure) (string, render.Style) {
	if d.Cancelled {
		return "cancelled", dueNow
	}
	if !d.Predicted {
		return "sched", faint
	}
	switch off := waitMinutes(d.Expected - d.Scheduled); {
	case off > 1:
		return strconv.Itoa(off) + " late", unbold(dueSoon)
	case off < -1:
		return strconv.Itoa(-off) + " early", muted
	default:
		return "on time", comingUp
	}
}

// clockPaint is how bright the time on a row is. A prediction is the best answer
// this program has and reads plainly. A schedule is dim. A cancelled row keeps
// the time it was going to run at, with a line through it.
func clockPaint(d Departure) render.Style {
	switch {
	case d.Cancelled:
		return strikeOut(muted)
	case d.Predicted:
		return plainText
	default:
		return muted
	}
}

// waitCell is the countdown, or the dash that stands in for one.
//
// A countdown is read against the times above and below it, so it is the one
// field named by where it ends rather than where it starts. Its text is cut here
// and not by the renderer, because where it starts depends on how long it is.
func waitCell(d Departure, c column) render.Cell {
	text, style := waitText(d.Wait), waitPaint(d.Wait)
	if d.Cancelled {
		// The dash is not a time, so it does not take the colour the time it
		// replaced would have had.
		text, style = gone, muted
	}
	text = render.Cut(text, c.room)
	return render.Cell{
		At: c.at - len([]rune(text)), Text: text, Style: style, Room: c.room,
		Paints: render.Span{From: c.at - c.room, To: c.at},
	}
}

func boardRow(d Departure, c boardColumns) render.Row {
	row := render.Row{Cells: []render.Cell{
		badgeCell(d.Route, d.Colour, c.badge),
	}}
	// A board that was drilled to gave its headsign no room, because the
	// direction it was reached through already named it.
	if c.headsign.room > 0 {
		row.Cells = append(row.Cells, filled(c.headsign, d.Headsign, muted))
	}

	note, notePaint := noteOf(d)
	row.Cells = append(row.Cells,
		filled(c.clock, clockAt(d.Expected), clockPaint(d)),
		waitCell(d, c.wait),
		filled(c.note, note, notePaint))

	if d.AfterMidnight {
		// A trip from the service day that ended says so, or a departure at
		// 24:39 reads as a quarter past one in the afternoon.
		row.Cells = append(row.Cells, filled(
			column{at: c.note.at + c.note.room, room: afterMidnightWidth},
			"after midnight", faint))
	}
	return row
}

// routeOf is the route with that number, out of the ones a screen is showing.
func routeOf(routes []cache.Route, short string) cache.Route {
	for _, r := range routes {
		if r.ShortName == short {
			return r
		}
	}
	return cache.Route{ShortName: short}
}

// badgeCell draws a route badge in a row: five cells in the route's own
// colours, with the number centred and the odd space on the left. textCol is
// where a one or two character route sits, which is two cells into the badge.
func badgeCell(route, colour string, c column) render.Cell {
	first := c.at - badgePad
	left := (c.room - len([]rune(route)) + 1) / 2
	if left < 0 {
		left = 0
	}
	return render.Cell{
		At: first + left, Text: route, Style: badgeStyle(colour),
		Paints: render.Span{From: first, To: first + c.room},
	}
}
