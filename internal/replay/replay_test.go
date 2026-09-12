package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/app"
	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/script"
)

// No test here calls t.Parallel. One of them writes time.Local, which is
// process wide, and a parallel neighbour reading it would be a data race.

type stub struct{}

func (stub) Routes(_ context.Context, m cache.Mode, _ string) ([]cache.Route, error) {
	if m == cache.Bus {
		return []cache.Route{{ShortName: "44", LongName: "Billings Bridge <> Hurdman"}}, nil
	}
	return nil, nil
}

func (stub) SearchStops(context.Context, string, string) ([]cache.Match, bool, error) {
	return nil, false, nil
}

func (stub) Directions(context.Context, string, string) ([]cache.Direction, error) {
	return nil, nil
}

func (stub) RouteStops(context.Context, string, string, string) ([]cache.Stop, error) {
	return nil, nil
}

func (stub) Departures(context.Context, string, string, string) ([]cache.Departure, error) {
	return nil, nil
}

const src = `cache ../cache.db
date 2026-09-12
time 08:00
offset -04:00
size 100 14
enter
esc
type billings
`

func artifact(t *testing.T, opt Options) string {
	t.Helper()
	s, err := script.Parse("script", strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var out strings.Builder
	w := bufio.NewWriter(&out)
	if err := runScript(context.Background(), s, stub{}, nil, app.Weather{}, app.NoKey(), nil, app.Notices{}, opt, w); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return out.String()
}

func headers(text string) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "--- ") {
			out = append(out, line)
		}
	}
	return out
}

// A frame is one whole turn of the event loop, so a step is one frame. A typed
// word presses eight keys and draws once, because the person typed once.
func TestEachStepDrawsOneFrameAndATypedWordDrawsOne(t *testing.T) {
	got := headers(artifact(t, Options{}))
	want := []string{
		"--- 0. start ---",
		"--- 1. enter ---",
		"--- 2. esc ---",
		"--- 3. type billings ---",
	}
	if !slices.Equal(got, want) {
		t.Errorf("frames =\n%v\nwant\n%v", got, want)
	}
}

// Each frame is checked on its own. Counting the lines of the whole artifact
// passed when one frame gained a row and another lost one, which is the
// compensating-error shape TESTING.md warns about.
func TestEveryFrameIsARuleTheContentARuleAndAStatusBar(t *testing.T) {
	text := artifact(t, Options{})
	rule := strings.Repeat("─", 100)

	frames := strings.Split(text, "\n--- ")
	if len(frames) != 5 {
		t.Fatalf("split into %d parts, want a leading empty one and four frames", len(frames))
	}
	for i, f := range frames[1:] {
		_, body, ok := strings.Cut(f, " ---\n")
		if !ok {
			t.Fatalf("frame %d has no header", i)
		}
		rows := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
		if len(rows) != 14 {
			t.Errorf("frame %d drew %d rows, want 14", i, len(rows))
			continue
		}
		if rows[0] != rule || rows[12] != rule {
			t.Errorf("frame %d is not ruled top and bottom", i)
		}
		if strings.TrimSpace(rows[13]) == "" {
			t.Errorf("frame %d has an empty status bar", i)
		}
		for j, row := range rows {
			if n := len([]rune(row)); n != 100 {
				t.Errorf("frame %d row %d is %d runes, want 100", i, j, n)
			}
		}
	}
}

func TestTheSemanticBlockHoldsExactlyTheAgreedKeys(t *testing.T) {
	text := artifact(t, Options{Semantic: true})
	_, rest, ok := strings.Cut(text, "semantic\n")
	if !ok {
		t.Fatal("no semantic block")
	}
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatal("the semantic block does not close")
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(rest[:end+3]), &got); err != nil {
		t.Fatalf("the semantic block is not JSON: %v", err)
	}

	keys := make([]string, 0, len(got))
	for k := range got {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := []string{"detour", "feed", "filter", "now", "pins", "rows", "screen", "selected", "weather"}
	if !slices.Equal(keys, want) {
		t.Errorf("keys = %v, want %v", keys, want)
	}
	if got["now"] != float64(8*3600) {
		t.Errorf("now = %v, want %d", got["now"], 8*3600)
	}
}

// A route long name carries `<>`. The default encoder writes that as an
// escape, and the reference does not.
func TestAngleBracketsInANameAreNotEscaped(t *testing.T) {
	text := artifact(t, Options{Semantic: true})
	// Spelled from bytes so no editor or shell can turn it back into the
	// character it stands for. An earlier version of this line searched for
	// "<" and passed on any artifact holding the name at all.
	escaped := string([]byte{92, 'u', '0', '0', '3', 'c'})
	if strings.Contains(text, escaped) {
		t.Errorf("the semantic block escaped a bracket as %s", escaped)
	}
	if !strings.Contains(text, "Billings Bridge <> Hurdman") {
		t.Error("the long name is missing from the semantic block")
	}
}

// Nothing in a replay may read the machine. The same fixture must produce the
// same bytes in every timezone, so the artifact is a function of the directory.
//
// The date matters. On an ordinary day `now` is measured from that day's own
// origin and comes out the same in every zone, so an artifact built from
// time.Local matches one built from the script's offset and the test proves
// nothing. That is what the first version of this test did. 2026-03-08 is the
// day Toronto loses an hour: at 01:00 the script's fixed -05:00 gives 3600 and
// a machine on Toronto time gives 7200.
const changeover = `cache ../cache.db
date 2026-03-08
time 01:00
offset -05:00
size 100 14
`

func TestTheArtifactDoesNotDependOnTheMachineZone(t *testing.T) {
	toronto, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Skipf("no zone database: %v", err)
	}

	was := time.Local
	t.Cleanup(func() { time.Local = was })

	build := func() string {
		s, err := script.Parse("script", strings.NewReader(changeover))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		var out strings.Builder
		w := bufio.NewWriter(&out)
		if err := runScript(context.Background(), s, stub{}, nil, app.Weather{}, app.NoKey(), nil, app.Notices{}, Options{Semantic: true}, w); err != nil {
			t.Fatalf("runScript: %v", err)
		}
		if err := w.Flush(); err != nil {
			t.Fatalf("Flush: %v", err)
		}
		return out.String()
	}

	time.Local = toronto
	onDST := build()
	time.Local = time.UTC
	onUTC := build()

	if onDST != onUTC {
		t.Error("the artifact changed with the machine zone")
	}
	if !strings.Contains(onDST, `"now": 3600`) {
		t.Errorf("want the offset the script names, which puts 01:00 at 3600 seconds; a machine on Toronto time gives 7200")
	}
}

// The styles observation adds a block to every frame, and adds it after the
// rows rather than in place of them: a flag adds to the text and never replaces
// it.
func TestTheStylesObservationAddsABlockToEveryFrame(t *testing.T) {
	plain := artifact(t, Options{})
	styled := artifact(t, Options{Styles: true})

	if len(styled) <= len(plain) {
		t.Errorf("the styles artifact is %d bytes and the text is %d", len(styled), len(plain))
	}
	if n := strings.Count(styled, "\nstyles\n"); n != 4 {
		t.Errorf("%d styles blocks, want one per frame", n)
	}
	for _, line := range strings.Split(plain, "\n") {
		if line != "" && !strings.Contains(styled, line) {
			t.Fatalf("the styles artifact dropped a text line: %q", line)
		}
	}
}

// One line per row that carries a style, in row order, and a row that carries
// none is left out.
func TestAStylesBlockNamesItsRowsInOrder(t *testing.T) {
	text := artifact(t, Options{Styles: true})
	_, rest, ok := strings.Cut(text, "\nstyles\n")
	if !ok {
		t.Fatal("no styles block")
	}
	block, _, _ := strings.Cut(rest, "\n\n")

	last := -1
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		row, _, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			t.Fatalf("line %q is not a row and its runs", line)
		}
		n, err := strconv.Atoi(row)
		if err != nil {
			t.Fatalf("line %q does not begin with a row number", line)
		}
		if n <= last {
			t.Errorf("row %d came after row %d", n, last)
		}
		last = n
	}
}

// A cache that is not there is an input, not a broken invariant, and the error
// has to name the file. Left to SQLite it first appeared as an errno from the
// middle of the first query, which named nothing a person could act on.
func TestAMissingCacheNamesTheFileAndNotAnErrno(t *testing.T) {
	dir := t.TempDir()
	world := "cache ../missing.db\ndate 2026-09-12\ntime 08:00\noffset -04:00\nsize 100 14\n"
	if err := os.WriteFile(filepath.Join(dir, "script"), []byte(world), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	err := Run(context.Background(), dir, Options{}, io.Discard)
	if err == nil {
		t.Fatal("Run = nil error, want one")
	}
	for _, want := range []string{dir, "../missing.db"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "(14)") {
		t.Errorf("error %q leaks the SQLite errno", err)
	}
}

func (stub) Stop(context.Context, string) (cache.Stop, bool, error) {
	return cache.Stop{}, false, nil
}

// A fixture's pins reach the program. A replay reads them and never writes
// them back, so a script that presses p cannot edit the world it is replaying.
func TestAFixtureSPinsReachTheProgram(t *testing.T) {
	dir := t.TempDir()
	world := "cache ../cache.db\ndate 2026-09-12\ntime 08:00\noffset -04:00\nsize 100 14\n"
	if err := os.WriteFile(filepath.Join(dir, "script"), []byte(world), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	pins := "7581\t1869\tTRANSITWAY / TERMINAL\n"
	if err := os.WriteFile(filepath.Join(dir, "pins"), []byte(pins), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := pinsIn(dir)
	if err != nil {
		t.Fatalf("pinsIn: %v", err)
	}
	if len(got) != 1 || got[0].Stop != "7581" {
		t.Errorf("pinsIn = %+v, want the one pin", got)
	}

	// A fixture with no pins file has none, and that is not an error.
	bare := t.TempDir()
	none, err := pinsIn(bare)
	if err != nil || none != nil {
		t.Errorf("pinsIn on a fixture with no pins = %+v, %v", none, err)
	}
}

// The pins a fixture holds reach the screen. Reading the file is one thing and
// handing it to the program is another, and the second was untested.
func TestPinsReadFromAFixtureReachTheFirstScreen(t *testing.T) {
	s, err := script.Parse("script", strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var out strings.Builder
	w := bufio.NewWriter(&out)
	kept := []app.Pin{{Stop: "s1"}}
	if err := runScript(context.Background(), s, pinned{}, kept, app.Weather{}, app.NoKey(), nil, app.Notices{}, Options{}, w); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if !strings.Contains(out.String(), "A PINNED STOP") {
		t.Error("the first screen does not draw the fixture's pin")
	}
}

// pinned is a world with one stop that a pin can resolve to.
type pinned struct{ stub }

func (pinned) Stop(context.Context, string) (cache.Stop, bool, error) {
	return cache.Stop{ID: "s1", Code: "1869", Name: "A PINNED STOP"}, true, nil
}

func (pinned) Departures(context.Context, string, string, string) ([]cache.Departure, error) {
	return []cache.Departure{{Route: "44", Headsign: "Hurdman", Scheduled: 9 * 3600}}, nil
}

// A weather file that is there must parse. A fixture's whole claim is that the
// directory decides the output, so a feed this program cannot read is an error
// and not a quiet nothing: a silent one draws a rule with no weather on it,
// which is what a fixture with no feed draws.
func TestAWeatherFileThatWillNotParseIsAnError(t *testing.T) {
	dir := t.TempDir()
	world := "cache ../cache.db\ndate 2026-09-11\ntime 08:00\noffset -04:00\nsize 100 14\n"
	if err := os.WriteFile(filepath.Join(dir, "script"), []byte(world), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "weather.json"), []byte(`{"properties":{}}`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if _, err := weatherIn(dir); err == nil {
		t.Error("weatherIn on a feed with no current conditions = nil error, want one")
	}

	// A fixture with no weather file has none, and that is not an error.
	bare := t.TempDir()
	got, err := weatherIn(bare)
	if err != nil || got != (app.Weather{}) {
		t.Errorf("weatherIn on a fixture with no weather = %+v, %v", got, err)
	}
}

// A realtime feed that will not parse is an error, for the same reason a
// weather file is. A broken parse and a quiet Sunday draw the same screen: no
// predictions, and every row reading sched. A fixture would pass while proving
// nothing.
func TestATripUpdateFileThatWillNotParseIsAnError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "rt.json"), []byte(`{"Entity":`), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := feedIn(dir); err == nil {
		t.Error("feedIn on a feed that is cut off = nil error, want one")
	}

	// A fixture with no feed has a poller that never asked, which is what a
	// missing subscription key means. That is not an error.
	got, err := feedIn(t.TempDir())
	if err != nil || got.Live != nil {
		t.Errorf("feedIn on a fixture with no feed = %+v, %v", got, err)
	}
}

// An updates file that parses into no detours is an error. The feed is RSS from a
// content system and the two tags a detour needs are that system's convention, so
// a feed whose tags changed parses cleanly into nothing and draws exactly like a
// week with none. A fixture that means none leaves the file out.
func TestAnUpdatesFileWithNoDetoursIsAnError(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"a general message only", `<rss><channel><item><title>Note</title>` +
			`<category>General Message</category></item></channel></rss>`},
		{"a detour naming no routes", `<rss><channel><item><title>Note</title>` +
			`<category>Detours</category></item></channel></rss>`},
		{"an empty channel", `<rss><channel></channel></rss>`},
		{"cut off", `<rss><channel><item><title>Note`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "updates.xml"), []byte(tc.body), 0o600); err != nil {
				t.Fatalf("WriteFile: %v", err)
			}
			if _, err := updatesIn(dir); err == nil {
				t.Error("updatesIn = nil error, want one")
			}
		})
	}

	// A fixture with no updates file has no detours, and that is not an error.
	got, err := updatesIn(t.TempDir())
	if err != nil || got != (app.Notices{}) {
		t.Errorf("updatesIn on a fixture with no updates = %+v, %v", got, err)
	}
}

// A wait step moves the clock, and the poller reads the clock. Without the move
// a replay runs every frame at the same instant: the countdowns never change, the
// cadence never comes round, and the artifact looks settled rather than still.
func TestAWaitStepMovesTheClock(t *testing.T) {
	const src = `cache ../cache.db
date 2026-09-12
time 08:00
offset -04:00
size 100 14
wait 90s
wait 30s
`
	s, err := script.Parse("script", strings.NewReader(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var out strings.Builder
	w := bufio.NewWriter(&out)
	if err := runScript(context.Background(), s, stub{}, nil, app.Weather{}, app.NoKey(), nil, app.Notices{},
		Options{Semantic: true}, w); err != nil {
		t.Fatalf("runScript: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	var got []string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.Contains(line, `"now"`) {
			got = append(got, strings.TrimSpace(line))
		}
	}
	want := []string{`"now": 28800,`, `"now": 28890,`, `"now": 28920,`}
	if !slices.Equal(got, want) {
		t.Errorf("the frames are at %v, want %v", got, want)
	}
}
