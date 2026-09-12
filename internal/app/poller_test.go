package app

import (
	"strings"
	"testing"

	"github.com/sina-negarandeh/otransit-go/internal/realtime"
)

// When the poller asks, and when a new day makes it ask again.
func TestANewServiceDayIsOwedAnAttemptAtOnce(t *testing.T) {
	t.Parallel()
	// A service day starts its clock at zero. An attempt owed at half past eight
	// is owed at 30,600 seconds, and after midnight the clock reads 300: without
	// this, the poller would never ask again for the rest of the day.
	a := New(stub{}, onTheDay(t, "2026-09-12", "08:30"), Polling())
	a.Heard(Answer{Live: anything(t)})
	if a.Owed() {
		t.Fatal("an attempt is owed in the same second one was answered")
	}

	a.Retime(onTheDay(t, "2026-09-13", "00:05"))
	if !a.Owed() {
		t.Error("the new day is not owed an attempt, so the poller has stopped asking")
	}
}

func TestTheCadenceHoldsWithinOneDay(t *testing.T) {
	t.Parallel()
	// The same clock change, inside one day, leaves the cadence alone: an answer
	// twenty-five seconds ago is not owed another until it is due.
	a := New(stub{}, onTheDay(t, "2026-09-12", "08:30"), Polling())
	a.Heard(Answer{Live: anything(t)})

	a.Retime(onTheDay(t, "2026-09-12", "08:30"))
	if a.Owed() {
		t.Error("an attempt is owed before the cadence has run")
	}
}

// anything is a feed that parsed. What it says does not matter here: what
// matters is that an answer arrived.
func anything(t *testing.T) *realtime.Feed {
	t.Helper()
	f, err := realtime.Parse(strings.NewReader(`{"Entity":[]}`))
	if err != nil {
		t.Fatalf("realtime.Parse: %v", err)
	}
	return f
}
