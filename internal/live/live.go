// Package live runs the program on a real terminal against a real clock.
//
// It is the counterpart of package replay. The same app, the same screens and the
// same renderer, with the machine supplying what a script supplies there: the
// keys a person presses, and the time it is now.
//
// Nothing here decides what a screen shows. What belongs here is the turn of the
// loop: read a key or notice the clock, bring the screen up to date, draw it.
package live

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/app"
	"github.com/sina-negarandeh/otransit-go/internal/cache"
	"github.com/sina-negarandeh/otransit-go/internal/clock"
	"github.com/sina-negarandeh/otransit-go/internal/feeds"
	"github.com/sina-negarandeh/otransit-go/internal/render"
	"github.com/sina-negarandeh/otransit-go/internal/term"
)

// tick is how often the program notices that time has passed.
//
// A second, because the countdowns are in whole minutes and a person watching
// one turn over should see it turn over when it does. Nothing is asked of the
// cache that a keypress would not also ask.
const tick = time.Second

// Run browses the cache until the person is done.
//
// The terminal is put into raw mode and a band of rows is claimed at the bottom
// of the screen. Both are undone on the way out, whatever the way out was.
//
// pins is the file the kept boards live in, beside the config and not beside the
// cache: deleting the cache is how a person recovers from a bad ingest, and it
// must not take their pins with it.
func Run(ctx context.Context, path, pins string, in, out *os.File) (err error) {
	// The pins first, because a word about them can only be read before the band
	// exists. Every row of the band is painted by the first frame and repainted
	// by every frame after it, so anything written there is gone unseen.
	kept := &keeper{path: pins}
	held, pinErr := kept.read()
	if pinErr != nil {
		// A file a person edits by hand can be wrong, and it is theirs to fix.
		if _, err := fmt.Fprintf(out, "otransit: %v\n", pinErr); err != nil {
			return err
		}
	}

	c, err := cache.Open(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, c.Close()) }()

	// A key means predictions, and no key means scheduled times only. The status
	// bar says which, so nothing here has to.
	key := feeds.Key()
	poller := app.NoKey()
	if key != "" {
		poller = app.Polling()
	}

	a := app.New(c, clock.Of(time.Now()), poller)
	a.Pinned(held)

	screen, err := term.Open(in, out)
	if err != nil {
		return err
	}
	// Paired with Open, and deferred at once. Every path out of this function
	// runs it, including a panic, because a terminal left in raw mode shows a
	// person nothing of what they type.
	defer func() { err = errors.Join(err, screen.Close()) }()

	beat := time.NewTicker(tick)
	defer beat.Stop()

	return session{
		app:    a,
		screen: screen,
		keys:   term.Keys(in),
		beat:   beat.C,
		now:    time.Now,
		keep:   kept,
		// The two that are asked once: a detour lasts days and the weather
		// changes by the hour, while a session lasts a minute. Started here and
		// waited for nowhere, so the first screen draws before either answers.
		weather: once(ctx, outside),
		notices: once(ctx, published),
		trips:   asking(key),
	}.browse(ctx)
}

// A session is what one turn of the loop needs: the program, the band it draws
// on, where keys and time come from, and where the pins are written down.
type session struct {
	app    *app.App
	screen viewport
	keys   <-chan term.Key
	beat   <-chan time.Time
	now    func() time.Time
	// keep is told after any turn that moved the pins. Nothing is written when
	// it is nil, which is how a test runs a loop that keeps nothing.
	keep *keeper
	// weather and notices each arrive once, or never. A nil channel blocks for
	// ever, which is how a loop runs with neither.
	weather <-chan app.Weather
	notices <-chan app.Notices
	// trips is the realtime endpoint: how to ask it, and whether it has been
	// asked. A zero one never asks, which is what no subscription key means.
	trips poller
}

// viewport is what a loop draws on. A test hands in its own, because what the
// loop does with a key is not a fact about terminals.
type viewport interface {
	Width() int
	Draw(frame string) error
}

// browse is the loop: read a key or notice the clock, bring the screen up to
// date, draw it, and stop when the program has been asked to stop.
//
// The clock arrives as a function rather than being read here, for the same
// reason the replay's clock comes from a script: a screen must not be able to
// tell which kind of program it is in.
func (s session) browse(ctx context.Context) error {
	if err := s.app.Refresh(ctx); err != nil {
		return err
	}
	if err := draw(s.screen, s.app); err != nil {
		return err
	}

	// unsaved is the first pin that could not be written down. A config
	// directory nobody can write is not worth ending a session over: the pin
	// works for this run, and the person is told on the way out, where the
	// message lands in the shell rather than under a band about to be blanked.
	var unsaved error
	for {
		s.trips.askIfOwed(ctx, s.app)

		select {
		case <-ctx.Done():
			return unsaved

		case key, open := <-s.keys:
			if !open {
				// The terminal went away: a pipe closed, or the session ended
				// under the program. That is a way of being done too.
				return unsaved
			}
			if key.Kind == term.Quit {
				return unsaved
			}
			s.app.Press(pressed(key))

		case <-s.beat:
			// Time passing is a turn of the loop as much as a keypress is.
			s.app.Retime(clock.Of(s.now()))

		case answer := <-s.trips.answers:
			s.trips.inFlight = false
			s.app.Heard(answer)

		case outside := <-s.weather:
			s.app.Outside(outside)
			// Asked once, so the channel is done with. A nil one never fires
			// again, and the loop has one branch fewer to consider.
			s.weather = nil

		case notices := <-s.notices:
			s.app.Updates(notices)
			s.notices = nil
		}

		if err := s.app.Refresh(ctx); err != nil {
			return err
		}
		if err := draw(s.screen, s.app); err != nil {
			return err
		}
		// Written as it happens rather than on the way out, because the way out
		// is not always this program's decision: a terminal that closes takes
		// the process with it, and a pin a person made must already be on disk.
		if err := s.keep.wrote(s.app); err != nil && unsaved == nil {
			unsaved = err
		}
		if s.app.Leaving() {
			return unsaved
		}
	}
}

// pressed turns a key the terminal read into a key the program understands.
//
// Two vocabularies meet here and nowhere else: what a terminal sends is a fact
// about terminals, and what a key means is a fact about this program.
func pressed(k term.Key) app.Key {
	switch k.Kind {
	case term.Up:
		return app.Key{Kind: app.Up}
	case term.Down:
		return app.Key{Kind: app.Down}
	case term.Enter:
		return app.Key{Kind: app.Enter}
	case term.Esc:
		return app.Key{Kind: app.Esc}
	case term.Backspace:
		return app.Key{Kind: app.Backspace}
	}
	return app.Key{Kind: app.Char, Rune: k.Text}
}

// draw puts the screen in the viewport, at the width the terminal had when the
// program started and the height the viewport always has.
func draw(screen viewport, a *app.App) error {
	return screen.Draw(render.Draw(a.View(screen.Width(), term.Rows)).ANSI())
}
