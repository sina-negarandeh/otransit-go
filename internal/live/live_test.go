package live

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/app"
	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/clock"
	"github.com/sina-negarandeh/otransit-go/internal/render"
	"github.com/sina-negarandeh/otransit-go/internal/term"
)

// No test here reads the machine's clock, its terminal or the real cache. The
// loop is handed a world the test built, the same way a replay is.

// pane records what the loop drew.
//
// The loop runs in a goroutine of its own in some of these tests, and the test
// reads the frames while it does, so the two are held apart by a lock. The race
// detector is in the gate for exactly this, and it found this one.
type pane struct {
	mu     sync.Mutex
	width  int
	frames []string
}

func (p *pane) Width() int { return p.width }

func (p *pane) Draw(frame string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.frames = append(p.frames, frame)
	return nil
}

// drawn is every frame so far, copied, so a reader holds something the loop
// cannot change under it.
func (p *pane) drawn() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.frames)
}

// world is a cache with one route and one stop, built in memory.
type world struct{}

func (world) Routes(_ context.Context, m cache.Mode, _ string) ([]cache.Route, error) {
	if m == cache.Bus {
		return []cache.Route{{ShortName: "44", LongName: "Billings Bridge <> Hurdman"}}, nil
	}
	return nil, nil
}

func (world) SearchStops(_ context.Context, query, _ string) ([]cache.Match, bool, error) {
	if query == "" {
		return nil, false, nil
	}
	return []cache.Match{{Stop: cache.Stop{ID: "s1", Code: "3034", Name: "BILLINGS BRIDGE 3B"}}}, false, nil
}

func (world) Directions(context.Context, string, string) ([]cache.Direction, error) {
	return []cache.Direction{{Headsign: "Hurdman", Trips: 8}}, nil
}

func (world) RouteStops(context.Context, string, string, string) ([]cache.Stop, error) {
	return []cache.Stop{{ID: "s1", Code: "3034", Name: "BILLINGS BRIDGE 3B"}}, nil
}

func (world) Stop(_ context.Context, id string) (cache.Stop, bool, error) {
	return cache.Stop{ID: id, Code: "3034", Name: "BILLINGS BRIDGE 3B"}, true, nil
}

func (world) Departures(context.Context, string, string, string) ([]cache.Departure, error) {
	return []cache.Departure{{Trip: "t1", Route: "44", Headsign: "Hurdman", Scheduled: 8*3600 + 22*60}}, nil
}

// finished is the loop's answer, or a failure when it does not come.
//
// A test that waits for ever on a loop that will not end does not fail: it hangs,
// and Go's own timeout ends it ten minutes later with a goroutine dump and no
// word about which test was waiting. The mutation campaign is what makes the
// difference count, because a mutation that stops the loop leaving is caught
// either way and only one of the two says so in a minute.
func finished(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(4 * time.Second):
		t.Fatal("the loop is still running")
		return nil
	}
}

// happened waits for something a fetch beside the loop is expected to do.
func happened(t *testing.T, what <-chan struct{}, which string) {
	t.Helper()
	select {
	case <-what:
	case <-time.After(4 * time.Second):
		t.Fatalf("%s never happened", which)
	}
}

// held is the program on a clock that does not move unless a test moves it,
// and the band it draws on.
func held(t *testing.T) (*app.App, *pane) {
	t.Helper()
	return pinned(t), &pane{width: 80}
}

// pinned is that program without a band, for the tests that never draw.
func pinned(t *testing.T) *app.App {
	t.Helper()
	k, err := clock.At("2026-09-11", "08:00", time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("clock.At: %v", err)
	}
	return app.New(world{}, k, app.NoKey())
}

// text is a frame with its escapes taken out, which is what a person sees.
func text(frame string) string {
	var out strings.Builder
	for i := 0; i < len(frame); i++ {
		if frame[i] == 0x1b {
			for i < len(frame) && !strings.ContainsRune("ABCDEFGHJKSTfmnsulh", rune(frame[i])) {
				i++
			}
			continue
		}
		out.WriteByte(frame[i])
	}
	return out.String()
}

// The loop draws once before any key, because a person who starts the program
// should see something without pressing anything.
func TestTheLoopDrawsBeforeTheFirstKey(t *testing.T) {
	t.Parallel()
	a, screen := held(t)
	keys := make(chan term.Key)
	close(keys)

	if err := (session{app: a, screen: screen, keys: keys, beat: nil, now: time.Now}).browse(context.Background()); err != nil {
		t.Fatalf("browse: %v", err)
	}
	drawn := screen.drawn()
	if len(drawn) != 1 {
		t.Fatalf("drew %d frames, want the first one", len(drawn))
	}
	if got := text(drawn[0]); !strings.Contains(got, "What are you taking?") {
		t.Errorf("the first frame is\n%s", got)
	}
}

// A key is one turn of the loop: the program answers it and draws again.
func TestEachKeyDrawsAgain(t *testing.T) {
	t.Parallel()
	a, screen := held(t)
	keys := make(chan term.Key, 3)
	for _, k := range []term.Key{{Kind: term.Enter}, {Kind: term.Down}, {Kind: term.Esc}} {
		keys <- k
	}
	close(keys)

	if err := (session{app: a, screen: screen, keys: keys, beat: nil, now: time.Now}).browse(context.Background()); err != nil {
		t.Fatalf("browse: %v", err)
	}
	drawn := screen.drawn()
	if len(drawn) != 4 {
		t.Fatalf("drew %d frames, want one before the keys and one for each", len(drawn))
	}
	// Enter opened the route list, and esc came back to the first screen.
	if got := text(drawn[1]); !strings.Contains(got, "Which route?") {
		t.Errorf("after enter the frame is\n%s", got)
	}
	if got := text(drawn[3]); !strings.Contains(got, "What are you taking?") {
		t.Errorf("after esc the frame is\n%s", got)
	}
}

// Esc on the first screen ends the loop. The frame it drew first is the last
// thing a person saw, so the drawing comes before the leaving.
func TestEscOnTheFirstScreenEndsTheLoop(t *testing.T) {
	t.Parallel()
	a, screen := held(t)
	// More keys than the loop should read: it must stop at the escape.
	keys := make(chan term.Key, 3)
	keys <- term.Key{Kind: term.Esc}
	keys <- term.Key{Kind: term.Enter}
	keys <- term.Key{Kind: term.Enter}

	done := make(chan error, 1)
	go func() {
		done <- (session{app: a, screen: screen, keys: keys, beat: nil, now: time.Now}).browse(context.Background())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("browse: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the loop did not end on esc")
	}
	if drawn := screen.drawn(); len(drawn) != 2 {
		t.Errorf("drew %d frames, want the first and the one esc drew", len(drawn))
	}
}

// An interrupt ends the loop wherever it is, and the program draws nothing more.
func TestAnInterruptEndsTheLoopAtOnce(t *testing.T) {
	t.Parallel()
	a, screen := held(t)
	keys := make(chan term.Key, 2)
	keys <- term.Key{Kind: term.Quit}
	keys <- term.Key{Kind: term.Enter}

	done := make(chan error, 1)
	go func() {
		done <- (session{app: a, screen: screen, keys: keys, beat: nil, now: time.Now}).browse(context.Background())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("browse: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the loop did not end on an interrupt")
	}
	if drawn := screen.drawn(); len(drawn) != 1 {
		t.Errorf("drew %d frames, want only the one before the interrupt", len(drawn))
	}
}

// The clock moving is a turn of the loop as much as a key is. Nobody pressed
// anything here and the countdown changed, which is the whole reason a live
// program has a clock at all.
func TestTimePassingRedrawsTheCountdown(t *testing.T) {
	t.Parallel()
	a, screen := held(t)
	// Down to the board, where a countdown is on screen.
	keys := make(chan term.Key, 3)
	keys <- term.Key{Kind: term.Rune, Text: 'b'}
	keys <- term.Key{Kind: term.Enter}
	beat := make(chan time.Time, 1)

	done := make(chan error, 1)
	go func() {
		done <- (session{app: a, screen: screen, keys: keys, beat: beat, now: laterBy(10 * time.Minute)}).browse(context.Background())
	}()

	// Wait for the board, then let the clock move on without a key.
	waitFor(t, screen, "22 min")
	beat <- time.Time{}
	waitFor(t, screen, "12 min")
	close(keys)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the loop did not end")
	}
}

// laterBy is a clock reading one fixed instant, that far past the held one.
func laterBy(d time.Duration) func() time.Time {
	return func() time.Time {
		return time.Date(2026, 9, 11, 8, 0, 0, 0, time.FixedZone("", -4*3600)).Add(d)
	}
}

// waitFor waits for a frame holding what it was told to look for.
func waitFor(t *testing.T, screen *pane, want string) {
	t.Helper()
	for i := 0; i < 200; i++ {
		for _, frame := range screen.drawn() {
			if strings.Contains(text(frame), want) {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no frame held %q", want)
}

// The frame the loop draws is the frame the renderer draws, at the width the
// terminal gave and the height the viewport always has.
func TestTheLoopDrawsTheViewportsOwnSize(t *testing.T) {
	t.Parallel()
	a, screen := held(t)
	screen.width = 44
	keys := make(chan term.Key)
	close(keys)
	if err := (session{app: a, screen: screen, keys: keys, beat: nil, now: time.Now}).browse(context.Background()); err != nil {
		t.Fatalf("browse: %v", err)
	}

	drawn := screen.drawn()
	rows := strings.Split(text(drawn[0]), "\r\n")
	if len(rows) != term.Rows {
		t.Fatalf("drew %d rows, want %d", len(rows), term.Rows)
	}
	for i, row := range rows {
		if n := len([]rune(row)); n != 44 {
			t.Errorf("row %d is %d cells, want 44: %q", i, n, row)
		}
	}
	// And it is the same bytes the renderer produces for that screen.
	want := render.Draw(a.View(44, term.Rows)).ANSI()
	if drawn[0] != want {
		t.Errorf("the loop drew something other than the renderer's own frame")
	}
}

// Each key a terminal sends means the key a person pressed. Up moves up: a
// mapping written the other way round would pass every test that only counted
// frames, and a person would find the cursor going the wrong way.
func TestEachKeyMeansWhatThePersonPressed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		keys []term.Key
		want int
	}{
		{"down moves down", []term.Key{{Kind: term.Down}}, 1},
		{"down then up comes back", []term.Key{{Kind: term.Down}, {Kind: term.Up}}, 0},
		{"up at the top stays", []term.Key{{Kind: term.Up}}, 0},
		{"down twice on two rows stops at the second", []term.Key{{Kind: term.Down}, {Kind: term.Down}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, screen := held(t)
			keys := make(chan term.Key, len(tc.keys))
			for _, k := range tc.keys {
				keys <- k
			}
			close(keys)

			if err := (session{app: a, screen: screen, keys: keys, beat: nil, now: time.Now}).browse(context.Background()); err != nil {
				t.Fatalf("browse: %v", err)
			}
			if got := a.State().Selected; got != tc.want {
				t.Errorf("Selected = %d, want %d", got, tc.want)
			}
		})
	}
}

// Enter, esc and a letter each reach the program as themselves.
func TestTheOtherKeysReachTheProgramAsThemselves(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		keys   []term.Key
		screen string
		filter string
	}{
		{"enter opens", []term.Key{{Kind: term.Enter}}, "routes", ""},
		{"a letter types", []term.Key{{Kind: term.Rune, Text: 'b'}}, "search", "b"},
		{"backspace undoes it", []term.Key{{Kind: term.Rune, Text: 'b'}, {Kind: term.Rune, Text: 'i'}, {Kind: term.Backspace}}, "search", "b"},
		{"esc comes back", []term.Key{{Kind: term.Enter}, {Kind: term.Esc}}, "mode", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, screen := held(t)
			keys := make(chan term.Key, len(tc.keys))
			for _, k := range tc.keys {
				keys <- k
			}
			close(keys)

			if err := (session{app: a, screen: screen, keys: keys, beat: nil, now: time.Now}).browse(context.Background()); err != nil {
				t.Fatalf("browse: %v", err)
			}
			got := a.State()
			if got.Screen != tc.screen || got.Filter != tc.filter {
				t.Errorf("ended on %q with %q typed, want %q and %q",
					got.Screen, got.Filter, tc.screen, tc.filter)
			}
		})
	}
}
