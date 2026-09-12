// The realtime poller: when it asks, what it keeps, and what it says for itself.
//
// The policy here is the program's own. A replay hands over what the endpoint
// will answer and never when the app will ask, so a port that polls twice as
// often runs out of answers and a port that never polls leaves them unused.
// Either way the artifact says so.
package app

import "github.com/sina-negarandeh/otransit-go/internal/realtime"

// How often the poller asks, in seconds on the service day.
//
// A refusal backs the next attempt off to a minute and each refusal after it
// doubles that. One answer puts the cadence back where it was rather than
// halving the backoff, because a feed that answered is not a feed recovering.
const (
	cadence = 25
	backoff = 60
)

// A driver is what makes the poller ask.
type driver int

const (
	// unasked: nothing asks. A fixture with a feed file and no queue hands over
	// data and no cadence, and a program with no subscription key never asks at
	// all.
	unasked driver = iota
	// queue: a replay hands the answers over, in order, as they come due.
	queue
	// network: a running program asks for them.
	network
)

// Answer is one thing the endpoint said. A nil Live is a refusal, and Refusal
// is what it said when it refused.
type Answer struct {
	Live    *realtime.Feed
	Refusal string
}

// Feed is the realtime poller: what it last heard, and when it will ask again.
//
// Live is the last answer that arrived, and it stays on screen while the feed is
// down: a stale prediction carrying an honest age beats no prediction, and the
// age is on screen for a person to judge. A nil Live with nothing queued is a
// poller that never asked, which is what a missing subscription key means.
type Feed struct {
	Live *realtime.Feed

	// answers is what the endpoint will say, in order. A replay queues them.
	// Nothing else does: a running program has a network here.
	answers []Answer
	// driven says what makes this poller ask. Nothing is both a queue and a
	// network, and a fixture carrying only a feed file is neither: it hands over
	// data and no cadence, and then there are no attempts to count.
	driven driver
	// refusal is what the last attempt said when it was refused.
	refusal string
	// due is when the next attempt is owed, in seconds on the service day. An
	// attempt that comes due with nothing queued stays owed: the count stops
	// climbing and due holds in the past, which is what a feed gone quiet looks
	// like from inside.
	due      int
	requests int
	// failures counts the refusals since the last answer, which is what the
	// backoff doubles on.
	failures int
}

// NoKey is the poller with no subscription key. It never asks for a
// prediction, so every time on screen is a scheduled one.
func NoKey() Feed { return Feed{driven: unasked} }

// Polling is the poller with a key and a network. The first attempt is owed at
// once, because a program that has just started has never asked.
func Polling() Feed { return Feed{driven: network} }

// Owed reports that an attempt has come due. A program with a network asks when
// this says so, and what it hears says when the next one is owed.
func (a *App) Owed() bool { return a.feed.driven == network && a.clock.Now() >= a.feed.due }

// Heard records what the endpoint said, and works out when to ask again.
//
// The moment it is recorded at is now and not when the attempt fell due, which is
// the one thing that differs from a replay: an answer can only be had after it
// arrives, so the cadence starts from there rather than drifting into the past.
func (a *App) Heard(answer Answer) { a.feed.record(answer, a.clock.Now()) }

// Answers queues what the endpoint will say and starts the cadence now. The
// first attempt is owed immediately, because a program that has just started
// has never asked.
func (a *App) Answers(said []Answer) {
	a.feed.answers, a.feed.driven, a.feed.due = said, queue, a.clock.Now()
}

// poll runs every attempt that has come due, in the order they came due.
//
// A wait covers every interval it spans, so a minute and a half at the
// twenty-five second cadence is three attempts and not one. Each is recorded at
// the moment it fell due rather than the moment the clock was read, which is
// what keeps the cadence from drifting a little later on every step.
func (f *Feed) poll(now int) {
	for f.driven == queue && now >= f.due && len(f.answers) > 0 {
		answer := f.answers[0]
		f.answers = f.answers[1:]
		// At the moment it fell due, and not the moment the clock was read.
		f.record(answer, f.due)
	}
}

// record is one attempt and its answer: what the poller now holds, and when the
// next attempt is owed.
//
// One rule for both kinds of poller. A replay and a running program differ in
// where an answer comes from and in nothing else, and the cadence is the part a
// fixture pins.
func (f *Feed) record(answer Answer, at int) {
	f.requests++
	if answer.Live != nil {
		f.Live, f.refusal, f.failures = answer.Live, "", 0
		f.due = at + cadence
		return
	}
	f.refusal = answer.Refusal
	f.failures++
	f.due = at + backoff<<(f.failures-1)
}

// report is what the artifact says about the poller. A poller with no cadence
// to report says only what it heard.
func (f *Feed) report(note string, now int) map[string]any {
	if f.driven != queue {
		return map[string]any{"note": note}
	}
	return map[string]any{
		"note":     note,
		"due_in":   max(0, f.due-now),
		"requests": f.requests,
		"failures": f.failures,
	}
}

// detourFor is what the updates feed says about a route, or nothing when it
// says nothing about that one. A route is on a detour's list or it is not.
func (a *App) detourFor(route string) string {
	if a.notices.Live == nil {
		return ""
	}
	return a.notices.Live.For(route)
}

// note is what the poller says for itself, in the words the status bar uses.
//
// What arrived wins over what was refused: a board holding predictions shows
// their age and not the refusal that followed, because the age is the thing a
// person judges them by. A refusal with nothing behind it has to be said out
// loud, though, because silence there reads as a quiet Sunday.
//
// A missing key is not an error either. It means scheduled times only.
func (a *App) note() string {
	switch {
	case a.feed.Live != nil:
		return a.feed.Live.Note(a.clock.Epoch())
	case a.feed.refusal != "":
		return "live: " + a.feed.refusal
	}
	return "no key · scheduled only"
}
