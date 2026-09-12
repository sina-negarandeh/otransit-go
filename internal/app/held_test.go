package app

import (
	"context"
	"testing"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/clock"
)

// The program on a clock the test holds.
//
// Nothing here reads the machine. Every test names the moment it is about, and
// the four builders this replaced each wrapped clock.At with the same zone and
// the same fatal.

// The day every test is on unless it says otherwise, and the time of day most of
// them want: a Saturday morning, where the fixtures' world has service.
const (
	theDay  = "2026-09-12"
	theHour = "08:00"
)

// onTheDay is a clock at one moment of one service day.
func onTheDay(t *testing.T, date, hhmm string) clock.Clock {
	t.Helper()
	k, err := clock.At(date, hhmm, time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("clock.At: %v", err)
	}
	return k
}

// newAppOn is the program on a held clock, refreshed once so it has a first
// screen. Everything else here goes through it.
func newAppOn(t *testing.T, s stub, date, hhmm string, f Feed) *App {
	t.Helper()
	a := New(s, onTheDay(t, date, hhmm), f)
	if err := a.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	return a
}

// newApp is the ordinary case: this world, that morning, no subscription key.
func newApp(t *testing.T, s stub) *App {
	t.Helper()
	return newAppOn(t, s, theDay, theHour, NoKey())
}
