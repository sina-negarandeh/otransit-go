package render

import (
	"strconv"
	"strings"
	"testing"
)

// listCols are the columns a list screen uses. Which columns a screen picks is
// the screen's business, so the tests here name their own.
var listCols = []int{3, 37}

// listRow is a two-field row, built the way a screen builds one: cells that name
// their own columns, and not a list of texts poured into a list of columns.
func listRow(primary, secondary string) Row {
	return Row{Cells: []Cell{
		{At: listCols[0], Text: primary},
		{At: listCols[1], Text: secondary},
	}}
}

func lines(t *testing.T, v View) []string {
	t.Helper()
	out := strings.Split(strings.TrimSuffix(Draw(v).String(), "\n"), "\n")
	if len(out) != v.Height {
		t.Fatalf("drew %d lines, want %d", len(out), v.Height)
	}
	return out
}

func screen() View {
	return View{
		Width:  100,
		Height: 14,
		Cursor: true,
		Rows: []Row{
			listRow("Bus", "0 routes running today"),
			listRow("O-Train", "0 lines · scheduled times only"),
		},
		// The first screen asks its question in the trail, where the part that
		// gives way lives, and carries no label.
		Status: Status{Trail: []Crumb{{Text: "What are you taking?"}}, Hints: "type to find a stop · ↑↓ · ↵ · q"},
	}
}

func TestEveryRowIsExactlyTheTerminalWidth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		v    View
	}{
		{"a full screen", screen()},
		{"an empty list", View{Width: 100, Height: 14, Empty: "nothing here"}},
		{"a narrow terminal", func() View { v := screen(); v.Width = 19; return v }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for i, line := range lines(t, tc.v) {
				if n := len([]rune(line)); n != tc.v.Width {
					t.Errorf("row %d is %d runes, want %d: %q", i, n, tc.v.Width, line)
				}
			}
		})
	}
}

func TestAScreenIsARuleTheContentARuleAndAStatusBar(t *testing.T) {
	t.Parallel()
	got := lines(t, screen())
	rule := strings.Repeat("─", 100)
	if got[0] != rule {
		t.Errorf("row 0 = %q, want a rule", got[0])
	}
	if got[12] != rule {
		t.Errorf("row 12 = %q, want a rule", got[12])
	}
	if !strings.HasPrefix(got[13], " What are you taking?") {
		t.Errorf("row 13 = %q, want the status bar", got[13])
	}
}

func TestContentSitsAtTheBottomOfItsArea(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		rows int
		want int // the row the first entry lands on
	}{
		{"one row", 1, 11},
		{"two rows", 2, 10},
		{"the row limit reached", 8, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := View{Width: 100, Height: 14, Cursor: true}
			for i := 0; i < tc.rows; i++ {
				v.Rows = append(v.Rows, listRow("x", ""))
			}
			got := lines(t, v)
			if !strings.HasPrefix(got[tc.want], " ❯ x") {
				t.Errorf("row %d = %q, want the first entry", tc.want, got[tc.want])
			}
			if tc.want > 1 && strings.TrimSpace(got[tc.want-1]) != "" {
				t.Errorf("row %d = %q, want it blank", tc.want-1, got[tc.want-1])
			}
		})
	}
}

func TestTheCursorMarksTheSelectedRowAndNoOther(t *testing.T) {
	t.Parallel()
	v := screen()
	v.Selected = 1
	got := lines(t, v)
	if strings.Count(strings.Join(got, "\n"), "❯") != 1 {
		t.Fatalf("want exactly one cursor, got %d", strings.Count(strings.Join(got, "\n"), "❯"))
	}
	if !strings.HasPrefix(got[11], " ❯ O-Train") {
		t.Errorf("row 11 = %q, want the cursor on O-Train", got[11])
	}
	if !strings.HasPrefix(got[10], "   Bus") {
		t.Errorf("row 10 = %q, want Bus with no cursor", got[10])
	}
}

func TestAListWithNoRowsDrawsItsMessage(t *testing.T) {
	t.Parallel()
	got := lines(t, View{Width: 100, Height: 14, Empty: "nothing here"})
	if !strings.HasPrefix(got[11], "  nothing here") {
		t.Errorf("row 11 = %q, want the message", got[11])
	}
	if strings.Contains(strings.Join(got, ""), "❯") {
		t.Error("an empty list drew a cursor")
	}
}

func TestTheStatusBarPutsTheLabelLeftTheFieldAfterItAndTheHintsRight(t *testing.T) {
	t.Parallel()
	v := View{Width: 100, Height: 14, Status: Status{
		Label: "Which route?", Trail: []Crumb{{Text: "Bus"}}, Hints: "↑↓ · ↵ · esc · q",
	}}
	bar := []rune(lines(t, v)[13])
	if got := string(bar[1:13]); got != "Which route?" {
		t.Errorf("label at 1 = %q", got)
	}
	if got := string(bar[16:19]); got != "Bus" {
		t.Errorf("field at 16 = %q", got)
	}
	if got := string(bar[83:99]); got != "↑↓ · ↵ · esc · q" {
		t.Errorf("hints at 83 = %q", got)
	}
	if bar[99] != ' ' {
		t.Errorf("the last cell is %q, want a space", bar[99])
	}
}

func TestTextTooLongForTheTerminalEndsInAnEllipsis(t *testing.T) {
	t.Parallel()
	v := screen()
	v.Width = 44
	got := lines(t, v)
	if !strings.HasSuffix(got[10], "…") {
		t.Errorf("row 10 = %q, want it to end in an ellipsis", got[10])
	}
	if !strings.HasPrefix(got[10], " ❯ Bus") {
		t.Errorf("row 10 = %q, want the entry kept", got[10])
	}
}

// A screen degrades from the top as the terminal gets shorter. The upper rule
// survives longest, then one row of content, then the lower rule, then the
// status bar. Every row past those four widens the content area.
//
// Every fixture is fourteen rows tall, so no diff can reach this. The ladder
// below was read off the reference implementation at heights 1 to 5 rather than
// reasoned about. An earlier version of this drew nothing at all below three
// rows, which was invented behaviour pinned by a test.
func TestAShortTerminalKeepsTheTopOfTheScreenAndLosesTheBottom(t *testing.T) {
	t.Parallel()
	rule := strings.Repeat("─", 100)

	for _, tc := range []struct {
		h       int
		rules   []int // rows that must be a full rule
		content []int // rows that must hold a list entry
		status  int   // row that must hold the status bar, or -1
	}{
		{h: 1, rules: []int{0}, content: nil, status: -1},
		{h: 2, rules: []int{0}, content: []int{1}, status: -1},
		{h: 3, rules: []int{0, 2}, content: []int{1}, status: -1},
		{h: 4, rules: []int{0, 2}, content: []int{1}, status: 3},
		{h: 5, rules: []int{0, 3}, content: []int{1, 2}, status: 4},
	} {
		t.Run(strconv.Itoa(tc.h), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{
				Width:  100,
				Height: tc.h,
				Rows:   []Row{listRow("Bus", ""), listRow("O-Train", "")},
				Cursor: true,
				Status: Status{Label: "What are you taking?"},
			})

			for _, row := range tc.rules {
				if got[row] != rule {
					t.Errorf("row %d = %q, want a rule", row, got[row])
				}
			}
			want := []string{" ❯ Bus", "   O-Train"}
			for i, row := range tc.content {
				if !strings.HasPrefix(got[row], want[i]) {
					t.Errorf("row %d = %q, want it to start %q", row, got[row], want[i])
				}
			}
			if tc.status >= 0 && !strings.HasPrefix(got[tc.status], " What are you taking?") {
				t.Errorf("row %d = %q, want the status bar", tc.status, got[tc.status])
			}
			if tc.status < 0 {
				for i, line := range got {
					if strings.Contains(line, "What are you") {
						t.Errorf("height %d drew a status bar on row %d", tc.h, i)
					}
				}
			}
		})
	}
}

// A list longer than the content area used to keep the first rows and drop the
// rest, so a selection past the last visible row drew no cursor at all.
func TestTheCursorStaysOnScreenWhenTheListIsLongerThanTheArea(t *testing.T) {
	t.Parallel()
	var rows []Row
	for i := 0; i < 30; i++ {
		rows = append(rows, listRow("stop"+strconv.Itoa(i), ""))
	}
	for _, selected := range []int{0, 5, 10, 11, 20, 29} {
		t.Run(strconv.Itoa(selected), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{Width: 100, Height: 14, Rows: rows, Selected: selected, Cursor: true})
			joined := strings.Join(got, "\n")
			if n := strings.Count(joined, "❯"); n != 1 {
				t.Fatalf("cursors drawn = %d, want exactly one", n)
			}
			want := "stop" + strconv.Itoa(selected)
			for _, line := range got {
				if strings.Contains(line, "❯") {
					if !strings.Contains(line, want+" ") && !strings.HasSuffix(strings.TrimRight(line, " "), want) {
						t.Errorf("cursor is on %q, want it on %q", strings.TrimSpace(line), want)
					}
					return
				}
			}
		})
	}
}

// A departures board is not a list anyone moves through, so it draws no
// cursor even though a row is selected.
func TestAViewWithNoCursorDrawsNoMarker(t *testing.T) {
	t.Parallel()
	rows := []Row{listRow("44", "Hurdman"), listRow("48", "Carleton")}
	for _, tc := range []struct {
		name   string
		cursor bool
		want   int
	}{
		{"a list", true, 1},
		{"a board", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{Width: 100, Height: 14, Rows: rows, Selected: 1, Cursor: tc.cursor})
			if n := strings.Count(strings.Join(got, "\n"), "❯"); n != tc.want {
				t.Errorf("drew %d cursors, want %d", n, tc.want)
			}
		})
	}
}

// A screen draws eight entries however tall the terminal is, and they sit at
// the bottom of the content area. What a screen holds is not bounded by this:
// a search of 43 stops reports all 43 and draws eight of them.
func TestAScreenDrawsAtMostEightEntriesWhateverTheHeight(t *testing.T) {
	t.Parallel()
	var rows []Row
	for i := 0; i < 30; i++ {
		rows = append(rows, listRow("stop"+strconv.Itoa(i), ""))
	}
	for _, h := range []int{14, 20, 40} {
		t.Run(strconv.Itoa(h), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{Width: 100, Height: h, Rows: rows, Cursor: true})
			drawn := 0
			for _, line := range got {
				if strings.Contains(line, "stop") {
					drawn++
				}
			}
			if drawn != 8 {
				t.Errorf("height %d drew %d entries, want 8", h, drawn)
			}
			// The last content row is the one above the lower rule.
			if !strings.Contains(got[h-3], "stop") {
				t.Errorf("height %d left the bottom content row empty", h)
			}
		})
	}
}

// The first screen puts pins at the top of the content area and the modes at
// the bottom, and one selection moves through both.
func TestRowsBeforeTheSplitAreDrawnFromTheTop(t *testing.T) {
	t.Parallel()
	v := View{
		Width: 100, Height: 14, Cursor: true, Split: 1,
		Rows: []Row{listRow("PINNED STOP", ""), listRow("Bus", ""), listRow("O-Train", "")},
	}
	for _, tc := range []struct {
		selected int
		onRow    int
	}{
		{0, 1},  // the pin, at the top
		{1, 10}, // Bus, at the bottom
		{2, 11},
	} {
		t.Run(strconv.Itoa(tc.selected), func(t *testing.T) {
			t.Parallel()
			v := v
			v.Selected = tc.selected
			got := lines(t, v)
			if !strings.HasPrefix(got[1], " ❯ PINNED STOP") && !strings.HasPrefix(got[1], "   PINNED STOP") {
				t.Errorf("row 1 = %q, want the pin at the top", got[1])
			}
			if !strings.Contains(got[10], "Bus") || !strings.Contains(got[11], "O-Train") {
				t.Errorf("the modes are not at the bottom: %q %q", got[10], got[11])
			}
			if !strings.HasPrefix(got[tc.onRow], " ❯") {
				t.Errorf("the cursor is not on row %d: %q", tc.onRow, got[tc.onRow])
			}
		})
	}
}

// At pairs a column with each text. A row that names more texts than there are
// columns is a mistake in the screen, and it says so rather than panicking on
// the first row it draws.
// A trail gives up its leading crumbs rather than running into the hints. A
// long stop name costs the mode, then the route, then the direction, and the
// last crumb is kept whatever the width.
func TestATrailDropsItsLeadingCrumbsToFitBesideTheHints(t *testing.T) {
	t.Parallel()
	trail := []Crumb{{Text: "Bus"}, {Text: " 44 "}, {Text: "toward Hurdman"}, {Text: "ALTA VISTA / PLEASANT PARK"}}
	hints := "no key · scheduled only · p pin · esc · q"

	for _, tc := range []struct {
		width int
		want  string
	}{
		{120, "Bus ›  44  › toward Hurdman › ALTA VISTA / PLEASANT PARK"},
		{110, " 44  › toward Hurdman › ALTA VISTA / PLEASANT PARK"},
		{100, "ALTA VISTA / PLEASANT PARK"},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{
				Width: tc.width, Height: 14,
				Status: Status{Label: "Departures", Trail: trail, Hints: hints},
			})
			bar := []rune(got[13])
			// The trail starts three cells after the label, which starts at 1.
			start := 1 + len("Departures") + 3
			field := strings.TrimRight(string(bar[start:len(bar)-1-len([]rune(hints))]), " ")
			if field != tc.want {
				t.Errorf("at %d the trail is %q, want %q", tc.width, field, tc.want)
			}
		})
	}
}

// The last crumb does not stay whatever the width. It is cut to the room beside
// the hints, and when there is none it goes too, which leaves a bar that names
// nothing. Every line below is the reference at that width, on the board the
// platforms fixture walks to.
//
// A leading crumb goes whole before the last one loses a cell, so at 60 the pole
// number is gone and the name is cut in the same frame.
func TestTheLastCrumbIsCutAndThenGoes(t *testing.T) {
	t.Parallel()
	const hints = "no key · scheduled only · p pin · esc · q"
	trail := []Crumb{{Text: "#7275"}, {Text: "CANTERBURY / AD. 860"}}

	for _, tc := range []struct {
		width int
		want  string
	}{
		{100, " Departures   #7275 › CANTERBURY / AD. 860                " + hints + " "},
		{60, " Departures   CA… " + hints + " "},
		{58, " Departures   … " + hints + " "},
		{57, " Departures    " + hints + " "},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{
				Width: tc.width, Height: 14,
				Status: Status{Label: "Departures", Trail: trail, Hints: hints},
			})[13]
			if got != tc.want {
				t.Errorf("at %d the bar is\n%q, want\n%q", tc.width, got, tc.want)
			}
		})
	}
}

// A run is written once, however many neighbours share it. Merging every
// matching neighbour leaves one way to write a row, which is what makes two
// implementations comparable on the styles they chose.
func TestRunsAreMaximal(t *testing.T) {
	t.Parallel()
	red := Style{Fg: "#da3839"}
	b := newBuffer(10, 1)
	for i := 2; i < 7; i++ {
		b.paint(0, i, i+1, red)
	}
	if got, want := strings.TrimSpace(b.Styles()), "0  2..7 #da3839"; got != want {
		t.Errorf("Styles() = %q, want %q", got, want)
	}
}

func TestAStyleIsWrittenAsAColourThenItsModifiers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		style Style
		want  string
	}{
		{"a colour alone", Style{Fg: "#da3839"}, "#da3839"},
		{"bold as well", Style{Fg: "#da3839", Bold: true}, "#da3839+b"},
		{"a background too", Style{Fg: "#ffffff", Bg: "#0057b8", Bold: true}, "#ffffff/#0057b8+b"},
		{"a modifier with no colour", Style{Bold: true}, "-+b"},
		{"every modifier, in order", Style{
			Bold: true, Dim: true, Italic: true, Underline: true, Reverse: true, Strike: true,
		}, "-+bdiurx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.style.String(); got != tc.want {
				t.Errorf("String() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A row that carries no style is left out of the block, because what is
// compared is what the program chose and not the absence of a choice.
func TestARowWithNoStyleIsLeftOut(t *testing.T) {
	t.Parallel()
	b := newBuffer(10, 3)
	b.paint(1, 0, 4, Style{Fg: "#da3839"})
	if got, want := strings.TrimSpace(b.Styles()), "1  0..4 #da3839"; got != want {
		t.Errorf("Styles() = %q, want only row 1", got)
	}
}

// A column keeps its painted width whatever text sits in it. A wait is written
// right against its right edge, and the colour still covers the whole column,
// so two boards with different waits report the same run.
func TestAColumnKeepsItsPaintedWidth(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, text string }{
		{"a short wait", "due"},
		{"a long one", "16h 09 min"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newBuffer(60, 1)
			drawRow(b, Row{Cells: []Cell{
				// A countdown is named by where it ends, so its text starts ten
				// cells earlier and the column paints all ten.
				{At: 55 - len([]rune(tc.text)), Text: tc.text,
					Paints: Span{From: 45, To: 55}, Style: Style{Fg: "#6d6e70"}},
			}}, 0, false)
			if got, want := strings.TrimSpace(b.Styles()), "0  45..55 #6d6e70"; got != want {
				t.Errorf("Styles() = %q, want %q", got, want)
			}
		})
	}
}

// A badge reaches left of its number, so the colour has air on each side.
func TestABadgeIsPaintedWiderThanItsNumber(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, route string }{
		{"two digits", "44"},
		{"one", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newBuffer(20, 1)
			drawRow(b, Row{Cells: []Cell{
				{At: 5, Text: tc.route, Paints: Span{From: 3, To: 8},
					Style: Style{Fg: "#ffffff", Bg: "#0057b8", Bold: true}},
			}}, 0, false)
			if got, want := strings.TrimSpace(b.Styles()), "0  3..8 #ffffff/#0057b8+b"; got != want {
				t.Errorf("Styles() = %q, want %q", got, want)
			}
		})
	}
}

// The status bar gives way from the right, and every line below was measured
// off the reference at that width. The reference is the only place this ladder
// is written down, and a rule this fiddly is worth pinning whole: the bar is
// compared cell for cell, so a test that checked one field would pass while the
// hints beside it sat a column out.
//
// A screen with a trail shortens the trail and leaves the label alone.
func TestATrailIsThePartThatGivesWay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width int
		want  string
	}{
		{39, " Which route?   Bus   ↑↓ · ↵ · esc · q "},
		{38, " Which route?   Bus  ↑↓ · ↵ · esc · q "},
		{37, " Which route?   Bus ↑↓ · ↵ · esc · q "},
		{36, " Which route?   B… ↑↓ · ↵ · esc · q "},
		{35, " Which route?   … ↑↓ · ↵ · esc · q "},
		{34, " Which route?    ↑↓ · ↵ · esc · q "},
		{33, " Which route?    ↑↓ · ↵ · esc · q"},
		{30, " Which route?    ↑↓ · ↵ · esc "},
		{25, " Which route?    ↑↓ · ↵ ·"},
		{19, " Which route?    ↑↓"},
		{13, " Which route?"},
		{12, " Which route"},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{
				Width: tc.width, Height: 14,
				Status: Status{Label: "Which route?", Trail: []Crumb{{Text: "Bus"}}, Hints: "↑↓ · ↵ · esc · q"},
			})[13]
			if got != tc.want {
				t.Errorf("at %d the bar is\n%q, want\n%q", tc.width, got, tc.want)
			}
		})
	}
}

// The first screen asks its question where the part that gives way sits, so the
// question is what a narrowing terminal takes. Below thirty-five cells it is
// gone and the bar is the hints alone.
func TestTheQuestionOnTheFirstScreenGivesWay(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width int
		want  string
	}{
		{56, " What are you taking?  type to find a stop · ↑↓ · ↵ · q "},
		{55, " What are you taking? type to find a stop · ↑↓ · ↵ · q "},
		{54, " What are you takin… type to find a stop · ↑↓ · ↵ · q "},
		{40, " What… type to find a stop · ↑↓ · ↵ · q "},
		{37, " W… type to find a stop · ↑↓ · ↵ · q "},
		{36, " … type to find a stop · ↑↓ · ↵ · q "},
		{35, "  type to find a stop · ↑↓ · ↵ · q "},
		{34, "  type to find a stop · ↑↓ · ↵ · q"},
		{30, "  type to find a stop · ↑↓ · ↵"},
		{19, "  type to find a st"},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{
				Width: tc.width, Height: 14,
				Status: Status{
					Trail: []Crumb{{Text: "What are you taking?"}},
					Hints: "type to find a stop · ↑↓ · ↵ · q",
				},
			})[13]
			if got != tc.want {
				t.Errorf("at %d the bar is\n%q, want\n%q", tc.width, got, tc.want)
			}
		})
	}
}

// What was typed sits last, so it outlives the trail beside it. The trail goes
// whole, in one step, before the filter gives up a cell.
func TestTheFilterOutlivesTheTrailBesideIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width int
		want  string
	}{
		{44, " Which route?   Bus   /4   ↑↓ · ↵ · esc · q "},
		{43, " Which route?   Bus   /4  ↑↓ · ↵ · esc · q "},
		{42, " Which route?   /4       ↑↓ · ↵ · esc · q "},
		{37, " Which route?   /4  ↑↓ · ↵ · esc · q "},
		{36, " Which route?   /4 ↑↓ · ↵ · esc · q "},
		{35, " Which route?   … ↑↓ · ↵ · esc · q "},
		{34, " Which route?    ↑↓ · ↵ · esc · q "},
		{19, " Which route?    ↑↓"},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{
				Width: tc.width, Height: 14,
				Status: Status{
					Label: "Which route?", Trail: []Crumb{{Text: "Bus"}},
					Extra: "/4", Hints: "↑↓ · ↵ · esc · q",
				},
			})[13]
			if got != tc.want {
				t.Errorf("at %d the bar is\n%q, want\n%q", tc.width, got, tc.want)
			}
		})
	}
}

// A bar with a filter and no trail keeps the three cells after the label all
// the same, and the filter gives way in that column. The search screen is the
// one that reaches this, and its gap takes no colour because it has no trail.
func TestAFilterWithNoTrailGivesWayInTheTrailsColumn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width int
		want  string
	}{
		{46, " Transit stops   /ott  2 found · ↑↓ · ↵ · esc "},
		{45, " Transit stops   /ott 2 found · ↑↓ · ↵ · esc "},
		{44, " Transit stops   /o… 2 found · ↑↓ · ↵ · esc "},
		{43, " Transit stops   /… 2 found · ↑↓ · ↵ · esc "},
		{42, " Transit stops   … 2 found · ↑↓ · ↵ · esc "},
		{41, " Transit stops    2 found · ↑↓ · ↵ · esc "},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			got := lines(t, View{
				Width: tc.width, Height: 14,
				Status: Status{Label: "Transit stops", Extra: "/ott", Hints: "2 found · ↑↓ · ↵ · esc"},
			})[13]
			if got != tc.want {
				t.Errorf("at %d the bar is\n%q, want\n%q", tc.width, got, tc.want)
			}
		})
	}
}

// The three cells after the label are the trail's first separator, so they take
// its colour. A bar whose only field is what was typed has no trail to separate
// and leaves them bare. The cells stay spent either way, because the trail's
// column does not move with the terminal or with what is on the bar.
func TestTheGapAfterALabelIsPaintedOnlyForATrail(t *testing.T) {
	t.Parallel()
	blue := Style{Fg: "#0000ff"}
	paint := Paint{Separator: Style{Fg: "#3a3b3d"}, Extra: Style{Fg: "#da3839"}}

	for _, tc := range []struct {
		name  string
		trail []Crumb
		want  string
	}{
		{"a trail", []Crumb{{Text: "cd", Style: blue}}, "0  3..6 #3a3b3d  6..8 #0000ff  11..13 #da3839"},
		{"a trail too long to draw", []Crumb{{Text: "a very long crumb", Style: blue}}, "0  3..6 #3a3b3d  6..8 #da3839"},
		{"the filter alone", nil, "0  6..8 #da3839"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newBuffer(20, 1)
			drawStatus(b, Status{
				Label: "ab", Trail: tc.trail, Extra: "/x", Hints: "q", Paint: paint,
			}, 0, 20)
			if got := strings.TrimSpace(b.Styles()); got != tc.want {
				t.Errorf("Styles() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The hints carry their colour to the right edge, so the cell of margin past
// them reads as one run with them and not as a hole. These two runs are the
// too-narrow fixture at nineteen cells, where the hints start where the left
// side left off rather than where they would rather be.
func TestTheHintsPaintTheMarginPastThem(t *testing.T) {
	t.Parallel()
	faint := Style{Fg: "#3a3b3d"}

	list := Status{
		Label: "Which route?", Trail: []Crumb{{Text: "Bus"}}, Hints: "↑↓ · ↵ · esc · q",
		Paint: Paint{Label: Style{Fg: "#e6e6e6", Bold: true}, Separator: faint, Hints: faint},
	}

	for _, tc := range []struct {
		name  string
		width int
		bar   Status
		want  string
	}{
		// Thirty-four cells is the widest terminal that drops the trail, so the
		// hints sit where the left side left off and still hold a margin.
		{"one cell of margin", 34, list, "0  1..13 #e6e6e6+b  13..16 #3a3b3d  17..34 #3a3b3d"},
		// At nineteen the edge takes the margin and two thirds of the hints.
		{"no margin left", 19, list, "0  1..13 #e6e6e6+b  13..16 #3a3b3d  17..19 #3a3b3d"},
		// The first screen, whose question is gone at this width, so the bar is
		// the hints and the two cells of margin they start after.
		{"a question and nothing left of it", 19, Status{
			Trail: []Crumb{{Text: "What are you taking?", Style: Style{Fg: "#e6e6e6", Bold: true}}},
			Hints: "type to find a stop · ↑↓ · ↵ · q", Paint: Paint{Hints: faint},
		}, "0  2..19 #3a3b3d"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			b := newBuffer(tc.width, 1)
			drawStatus(b, tc.bar, 0, tc.width)
			if got := strings.TrimSpace(b.Styles()); got != tc.want {
				t.Errorf("Styles() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The gap is written out where it is drawn and counted where it is measured.
// Two spellings of one width can drift, and a bar that measured three cells
// while drawing two would put every column after it one to the left.
func TestTheGapIsAsWideAsItIsDrawn(t *testing.T) {
	t.Parallel()
	if got := len([]rune(gapText)); got != gap {
		t.Errorf("gapText is %d cells, want gap, which is %d", got, gap)
	}
}

// A notice sits on the top row of the content area, and its colour starts at the
// left edge rather than at the text, which is how the empty-list message is drawn
// too. The content stays anchored to the bottom, so the two do not meet.
func TestANoticeSitsOnTheTopRowOfTheContent(t *testing.T) {
	t.Parallel()
	amber := Style{Fg: "#ffc107"}
	got := lines(t, View{
		Width: 30, Height: 14, Cursor: true,
		Rows:        []Row{listRow("Bus", "2 routes")},
		Notice:      "⚠ Bridge closed",
		NoticeStyle: amber,
		Status:      Status{Label: "Which way?", Hints: "q"},
	})
	if want := " ⚠ Bridge closed" + strings.Repeat(" ", 14); got[1] != want {
		t.Errorf("row 1 = %q, want %q", got[1], want)
	}
	// The row the content would have used if the notice had taken its place.
	if !strings.HasPrefix(got[11], " ❯ Bus") {
		t.Errorf("row 11 = %q, want the entry still at the bottom", got[11])
	}

	// Read off a drawn screen, not built here: the colour has to start at the
	// edge, and a run that started at the glyph would draw the same text.
	runs := Draw(View{
		Width: 30, Height: 14,
		Notice:      "⚠ Bridge closed",
		NoticeStyle: amber,
	}).Styles()
	if !strings.Contains(runs, "1  0..16 #ffc107") {
		t.Errorf("the styles are\n%s\nwant a run 1  0..16 #ffc107", runs)
	}
}

// The banner needs a rule to sit on. A terminal with room for the label and
// nothing else draws the rule plain, because a rule with no weather on it and a
// rule whose feed was missing have to look the same either way.
func TestTheBannerNeedsARuleCellToSitOn(t *testing.T) {
	t.Parallel()
	const label = "⛆ light rain · 21°"
	// The label, and the cell of air before it.
	room := len([]rune(label)) + 1

	for _, tc := range []struct {
		width int
		want  string
	}{
		{room + 2, "── " + label},
		{room + 1, "─ " + label},
		{room, strings.Repeat("─", room)},
		{room - 1, strings.Repeat("─", room-1)},
	} {
		t.Run(strconv.Itoa(tc.width), func(t *testing.T) {
			t.Parallel()
			top := lines(t, View{Width: tc.width, Height: 14, Rule: Style{Fg: "#3a3b3d"}, Banner: label})[0]
			if top != tc.want {
				t.Errorf("at %d the rule is %q, want %q", tc.width, top, tc.want)
			}
		})
	}
}

// A notice takes its row. The content is anchored to the bottom and does not
// reach it at any height a fixture uses, but the group before the split is drawn
// from the top, and sharing the row would lose its first entry under the warning.
func TestANoticeTakesItsRowFromTheGroupDrawnFromTheTop(t *testing.T) {
	t.Parallel()
	got := lines(t, View{
		Width: 30, Height: 14, Cursor: true,
		Rows:   []Row{listRow("Pinned", "#3034"), listRow("Bus", "2 routes")},
		Split:  1,
		Notice: "⚠ Bridge closed", NoticeStyle: Style{Fg: "#ffc107"},
		Status: Status{Label: "Which way?", Hints: "q"},
	})
	if !strings.HasPrefix(got[1], " ⚠ Bridge closed") {
		t.Errorf("row 1 = %q, want the warning", got[1])
	}
	if !strings.HasPrefix(got[2], " ❯ Pinned") {
		t.Errorf("row 2 = %q, want the pinned row below the warning", got[2])
	}
}
