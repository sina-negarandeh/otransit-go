// What the loop does with a feed, and nothing about fetching one.
//
// Two of the three are asked once at launch and arrive whenever they arrive. The
// third is asked again and again, so the loop drives it and this says only what
// an answer amounts to. Every request, timeout and header is in package feeds.
package live

import (
	"context"

	"github.com/sina-negarandeh/otransit-go/internal/app"
	"github.com/sina-negarandeh/otransit-go/internal/feeds"
	"github.com/sina-negarandeh/otransit-go/internal/realtime"
)

// once starts a fetch and hands back the channel its answer will arrive on.
//
// A fetch that fails sends nothing: every one of these is something the screen
// can do without, and a feed that is down must not be a program that will not
// start. The channel holds one, so the fetch never waits for a loop that has
// already ended.
func once[T any](ctx context.Context, fetch func(context.Context) (T, bool)) <-chan T {
	out := make(chan T, 1)
	go func() {
		if found, ok := fetch(ctx); ok {
			out <- found
		}
	}()
	return out
}

// outside is the weather, as the rule carries it.
func outside(ctx context.Context) (app.Weather, bool) {
	now, err := feeds.Weather(ctx)
	if err != nil {
		return app.Weather{}, false
	}
	return app.Weather{Label: now.Label()}, true
}

// published is what the updates feed says, for the screens about a route.
func published(ctx context.Context) (app.Notices, bool) {
	live, err := feeds.Detours(ctx)
	if err != nil {
		return app.Notices{}, false
	}
	return app.Notices{Live: live}, true
}

// A poller is the realtime endpoint, as the loop sees it: how to ask, where the
// answer arrives, and whether one is already on its way.
//
// The three belong together. The loop turns every second and an attempt is owed
// every twenty-five, so something has to know that one is in flight, and a bare
// bool beside the loop is that knowledge in the wrong place.
type poller struct {
	// ask is nil without a subscription key, and then nothing is ever asked and
	// every time on screen is a scheduled one.
	ask func(context.Context) app.Answer
	// answers holds one, so a fetch never waits for a loop that has ended.
	answers  chan app.Answer
	inFlight bool
}

// asking is the poller for a key, and the poller that never asks when there is
// no key to ask with.
func asking(key string) poller {
	if key == "" {
		return poller{}
	}
	return poller{
		ask: func(ctx context.Context) app.Answer {
			return answered(feeds.Trips(ctx, key))
		},
		answers: make(chan app.Answer, 1),
	}
}

// askIfOwed puts the question if one is due and none is on its way.
//
// The fetch runs beside the loop, because a board must keep drawing and keys
// must keep working while a slow endpoint thinks about it. Nothing but the loop
// touches the app.
func (p *poller) askIfOwed(ctx context.Context, a *app.App) {
	if p.ask == nil || p.inFlight || !a.Owed() {
		return
	}
	p.inFlight = true
	go func() { p.answers <- p.ask(ctx) }()
}

// answered is what a fetch amounts to.
//
// A refusal is an answer: the poller keeps whatever it last heard and says out
// loud that the feed said no, because silence there reads as a quiet Sunday. Kept
// apart from the fetch itself so it can be tested, which is the whole of why the
// fetch takes no argument here.
func answered(live *realtime.Feed, err error) app.Answer {
	if err != nil {
		return app.Answer{Refusal: err.Error()}
	}
	return app.Answer{Live: live}
}
