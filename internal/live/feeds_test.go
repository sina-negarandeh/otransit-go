package live

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/app"
	"github.com/sina-negarandeh/otransit-go/internal/clock"
	"github.com/sina-negarandeh/otransit-go/internal/detour"
	"github.com/sina-negarandeh/otransit-go/internal/realtime"
	"github.com/sina-negarandeh/otransit-go/internal/term"
)

// What the loop does with what a feed said. The fetching itself is not here: it
// is a thin wrapper around net/http, and no test in this project reaches a
// socket.

func TestTheWeatherReachesTheRuleWhenItArrives(t *testing.T) {
	t.Parallel()
	// The first screen is drawn before the weather answers, so it has to arrive
	// as a turn of the loop like any other.
	a, pane := held(t)
	weather := make(chan app.Weather, 1)
	keys := make(chan term.Key)

	done := make(chan error, 1)
	go func() {
		done <- (session{app: a, screen: pane, keys: keys, weather: weather, now: time.Now}).browse(context.Background())
	}()

	waitFor(t, pane, "What are you taking?")
	weather <- app.Weather{Label: "⛆ light rain · 21°"}
	waitFor(t, pane, "⛆ light rain · 21°")

	close(keys)
	if err := finished(t, done); err != nil {
		t.Fatalf("browse: %v", err)
	}
}

func TestADetourReachesTheScreenAboutItsRoute(t *testing.T) {
	t.Parallel()
	a, pane := held(t)
	notices := make(chan app.Notices, 1)
	keys := make(chan term.Key)

	done := make(chan error, 1)
	go func() {
		done <- (session{app: a, screen: pane, keys: keys, notices: notices, now: time.Now}).browse(context.Background())
	}()

	waitFor(t, pane, "What are you taking?")
	notices <- app.Notices{Live: detoured(t)}
	// Into the bus routes, then the directions of the 44, which is the screen a
	// detour is drawn on.
	keys <- term.Key{Kind: term.Enter}
	keys <- term.Key{Kind: term.Enter}
	waitFor(t, pane, "Road closed")

	close(keys)
	if err := finished(t, done); err != nil {
		t.Fatalf("browse: %v", err)
	}
}

func TestAPredictionIsAskedForOnceAndThenOnTheCadence(t *testing.T) {
	t.Parallel()
	// The loop turns every second and an attempt is owed every twenty-five, so
	// the guard is what stops a fetch a second: one in flight at a time, and the
	// answer says when the next one is owed.
	// One instant, held: the clock the loop reads and the clock the app started
	// with are the same, so a beat moves nothing and only the cadence decides.
	instant := time.Date(2026, 9, 12, 8, 0, 0, 0, time.FixedZone("", -4*3600))
	a := app.New(world{}, clock.Of(instant), app.Polling())
	pane := &pane{width: 80}
	keys := make(chan term.Key)
	beat := make(chan time.Time)

	asked := make(chan struct{}, 8)
	ask := func(context.Context) app.Answer {
		asked <- struct{}{}
		return app.Answer{Live: predicting(t)}
	}

	done := make(chan error, 1)
	go func() {
		done <- (session{
			app: a, screen: pane, keys: keys, beat: beat,
			now:   func() time.Time { return instant },
			trips: poller{ask: ask, answers: make(chan app.Answer, 1)},
		}).browse(context.Background())
	}()

	// The first attempt is owed at once, because the program has never asked.
	happened(t, asked, "the first attempt")
	// The note is a board's, which is the screen it is about.
	keys <- term.Key{Kind: term.Rune, Text: 'b'}
	keys <- term.Key{Kind: term.Enter}
	waitFor(t, pane, "live 0s")

	// A turn of the clock that does not reach the cadence asks nothing.
	beat <- time.Now()
	select {
	case <-asked:
		t.Error("a second attempt was made before it was owed")
	case <-time.After(100 * time.Millisecond):
	}

	close(keys)
	if err := finished(t, done); err != nil {
		t.Fatalf("browse: %v", err)
	}
}

func TestARefusalIsSaidOutLoudAndTheNextAttemptBacksOff(t *testing.T) {
	t.Parallel()
	// Silence reads as a quiet Sunday, so a feed that said no has to say so.
	a := app.New(world{}, clock.Of(time.Now()), app.Polling())
	pane := &pane{width: 80}
	keys := make(chan term.Key)

	done := make(chan error, 1)
	go func() {
		done <- (session{
			app: a, screen: pane, keys: keys, now: time.Now,
			trips: poller{
				answers: make(chan app.Answer, 1),
				ask: func(context.Context) app.Answer {
					return app.Answer{Refusal: "the feed answered 503"}
				},
			},
		}).browse(context.Background())
	}()

	keys <- term.Key{Kind: term.Rune, Text: 'b'}
	keys <- term.Key{Kind: term.Enter}
	waitFor(t, pane, "live: the feed answered 503")

	close(keys)
	if err := finished(t, done); err != nil {
		t.Fatalf("browse: %v", err)
	}
}

func TestAProgramWithNoKeyNeverAsks(t *testing.T) {
	t.Parallel()
	// No key is not an error. It means scheduled times only, and the status bar
	// says so.
	a, pane := held(t)
	keys := make(chan term.Key)

	done := make(chan error, 1)
	go func() {
		done <- (session{app: a, screen: pane, keys: keys, now: time.Now}).browse(context.Background())
	}()

	keys <- term.Key{Kind: term.Rune, Text: 'b'}
	keys <- term.Key{Kind: term.Enter}
	waitFor(t, pane, "no key · scheduled only")

	close(keys)
	if err := finished(t, done); err != nil {
		t.Fatalf("browse: %v", err)
	}
}

// detoured is one published notice about route 44.
func detoured(t *testing.T) *detour.Feed {
	t.Helper()
	f, err := detour.Parse(strings.NewReader(`<rss><channel><item>
		<title>Road closed at Bank</title>
		<category>Detours</category>
		<category>affectedRoutes-44</category>
	</item></channel></rss>`))
	if err != nil {
		t.Fatalf("detour.Parse: %v", err)
	}
	return f
}

// predicting is a feed that says one thing about one trip, stamped now.
func predicting(t *testing.T) *realtime.Feed {
	t.Helper()
	f, err := realtime.Parse(strings.NewReader(`{"Entity":[{"TripUpdate":{
		"Trip":{"TripId":"t1"},
		"StopTimeUpdate":[{"StopId":"s1","Arrival":{"Time":1,"HasTime":true}}]}}]}`))
	if err != nil {
		t.Fatalf("realtime.Parse: %v", err)
	}
	return f
}

func TestAFetchThatFailedIsARefusalAndNotSilence(t *testing.T) {
	t.Parallel()
	// The endpoint being down is news. A board holding predictions shows their
	// age instead, but a board with none has to say why, because saying nothing
	// there reads as a quiet Sunday.
	got := answered(nil, errors.New("the feed answered 503"))
	if got.Live != nil {
		t.Errorf("a failed fetch produced a feed: %+v", got)
	}
	if got.Refusal != "the feed answered 503" {
		t.Errorf("Refusal = %q, want what the fetch said", got.Refusal)
	}
}

func TestAFetchThatWorkedIsTheFeedAndNoRefusal(t *testing.T) {
	t.Parallel()
	live := predicting(t)
	got := answered(live, nil)
	if got.Live != live {
		t.Errorf("Live = %+v, want the feed that arrived", got.Live)
	}
	if got.Refusal != "" {
		t.Errorf("Refusal = %q, want none", got.Refusal)
	}
}

func TestOnlyOneAttemptIsInFlightAtATime(t *testing.T) {
	t.Parallel()
	// The loop turns every second and the endpoint can take longer than that. A
	// second fetch started while the first is still out gathers a queue of them,
	// each asking about a board the person may already have left.
	//
	// The clock is held where an attempt is always owed, which is the state that
	// makes the guard the only thing standing between one fetch and a hundred.
	instant := time.Date(2026, 9, 12, 8, 0, 0, 0, time.FixedZone("", -4*3600))
	a := app.New(world{}, clock.Of(instant), app.Polling())
	pane := &pane{width: 80}
	keys := make(chan term.Key)
	beat := make(chan time.Time)

	asked := make(chan struct{}, 8)
	answer := make(chan struct{})
	ask := func(context.Context) app.Answer {
		asked <- struct{}{}
		// Still out: the endpoint has not answered.
		<-answer
		return app.Answer{Live: predicting(t)}
	}

	done := make(chan error, 1)
	go func() {
		done <- (session{
			app: a, screen: pane, keys: keys, beat: beat,
			now:   func() time.Time { return instant },
			trips: poller{ask: ask, answers: make(chan app.Answer, 1)},
		}).browse(context.Background())
	}()

	happened(t, asked, "the first attempt")

	// Three turns of the clock while the first attempt is still out. Every one of
	// them is a turn where an attempt is owed.
	for range 3 {
		beat <- instant
	}
	select {
	case <-asked:
		t.Error("a second attempt went out while the first had not come back")
	case <-time.After(200 * time.Millisecond):
	}

	close(answer)
	close(keys)
	if err := finished(t, done); err != nil {
		t.Fatalf("browse: %v", err)
	}
}
