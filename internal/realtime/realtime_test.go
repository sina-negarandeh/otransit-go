package realtime

import (
	"strconv"
	"strings"
	"testing"
)

// feed builds the shape the endpoint serves. Every test writes its own, because
// a test that read the fixture would be measuring the fixture.
func feed(t *testing.T, body string) *Feed {
	t.Helper()
	f, err := Parse(strings.NewReader(body))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func TestAPredictionIsWhenTheFeedExpectsATripAtAStop(t *testing.T) {
	t.Parallel()
	f := feed(t, `{"Header":{"Timestamp":1000},"Entity":[
		{"TripUpdate":{"Trip":{"TripId":"t1","ScheduleRelationship":0},
		 "StopTimeUpdate":[{"StopId":"s1","Arrival":{"HasTime":true,"Time":1500}},
		                   {"StopId":"s2","Arrival":{"HasTime":true,"Time":1800}}]}}]}`)

	for _, tc := range []struct {
		trip, stop string
		want       int64
		found      bool
	}{
		{"t1", "s1", 1500, true},
		{"t1", "s2", 1800, true},
		// The same stop on another trip, and the same trip at a stop it was
		// not asked about. A feed says nothing about either.
		{"t2", "s1", 0, false},
		{"t1", "s3", 0, false},
	} {
		t.Run(tc.trip+" at "+tc.stop, func(t *testing.T) {
			t.Parallel()
			got, ok := f.Arrival(tc.trip, tc.stop)
			if ok != tc.found {
				t.Fatalf("Arrival(%q, %q) found = %v, want %v", tc.trip, tc.stop, ok, tc.found)
			}
			if got != tc.want {
				t.Errorf("Arrival(%q, %q) = %d, want %d", tc.trip, tc.stop, got, tc.want)
			}
		})
	}
}

// A stop time update with no time is not a prediction. The endpoint sends them,
// and taking the zero would put every such trip at the epoch.
func TestAnArrivalWithNoTimeIsNotAPrediction(t *testing.T) {
	t.Parallel()
	f := feed(t, `{"Header":{"Timestamp":1000},"Entity":[
		{"TripUpdate":{"Trip":{"TripId":"t1"},
		 "StopTimeUpdate":[{"StopId":"s1","Arrival":{"HasTime":false,"Time":1500}},
		                   {"StopId":"s2"}]}}]}`)

	for _, stop := range []string{"s1", "s2"} {
		if at, ok := f.Arrival("t1", stop); ok {
			t.Errorf("Arrival(t1, %q) = %d, want nothing", stop, at)
		}
	}
}

// Three of the four relationships mean the trip is running. Only the fourth
// takes it off the board, so a port that tested "not zero" would cancel every
// trip the feed added.
func TestOnlyTheCancelledRelationshipCancelsATrip(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		relationship int
		want         bool
	}{
		{0, false},
		{1, false},
		{2, false},
		{3, true},
	} {
		t.Run(strconv.Itoa(tc.relationship), func(t *testing.T) {
			t.Parallel()
			f := feed(t, `{"Header":{"Timestamp":1000},"Entity":[
				{"TripUpdate":{"Trip":{"TripId":"t1","ScheduleRelationship":`+
				strconv.Itoa(tc.relationship)+`},"StopTimeUpdate":[]}}]}`)
			if got := f.Cancelled("t1"); got != tc.want {
				t.Errorf("Cancelled(t1) = %v, want %v", got, tc.want)
			}
			if f.Cancelled("t2") {
				t.Error("Cancelled(t2) = true, want false: the feed never mentioned it")
			}
		})
	}
}

// The note counts seconds for the first two minutes and whole minutes after
// that. Measured against the reference at each step.
func TestTheNoteCountsSecondsUntilTwoMinutesAndMinutesAfterwards(t *testing.T) {
	t.Parallel()
	const built = 1789128000

	for _, tc := range []struct {
		age  int64
		want string
	}{
		{0, "live 0s"},
		{1, "live 1s"},
		{119, "live 119s"},
		{120, "live 120s"},
		{121, "live 2m old"},
		{179, "live 2m old"},
		{180, "live 3m old"},
		{3599, "live 59m old"},
		{3600, "live 60m old"},
		{7200, "live 120m old"},
		// A feed stamped later than the clock is not old. It happens when the
		// endpoint's clock runs ahead of ours.
		{-60, "live 0s"},
	} {
		t.Run(strconv.FormatInt(tc.age, 10), func(t *testing.T) {
			t.Parallel()
			f := feed(t, `{"Header":{"Timestamp":`+strconv.FormatInt(built, 10)+`},"Entity":[]}`)
			if got := f.Note(built + tc.age); got != tc.want {
				t.Errorf("Note at %ds old = %q, want %q", tc.age, got, tc.want)
			}
		})
	}
}

// A feed that does not say when it was built is taken as current. A stamp of
// zero does say, and it says 1970.
func TestAFeedWithNoStampIsTakenAsCurrent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		header string
		want   string
	}{
		{"no stamp", `{"Header":{},"Entity":[]}`, "live 0s"},
		{"no header at all", `{"Entity":[]}`, "live 0s"},
		{"a stamp of zero", `{"Header":{"Timestamp":0},"Entity":[]}`, "live 29818800m old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := feed(t, tc.header).Note(1789128000); got != tc.want {
				t.Errorf("Note = %q, want %q", got, tc.want)
			}
		})
	}
}

// A feed this program cannot read is an error and not an empty feed. The two
// look the same on screen: no predictions and no buses running.
func TestAFeedThatWillNotParseIsAnError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"not json", `{not json`},
		{"cut off", `{"Header":{"Timestamp":1`},
		{"a stamp that is not a number", `{"Header":{"Timestamp":"soon"},"Entity":[]}`},
		{"an entity list that is not a list", `{"Entity":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(strings.NewReader(tc.body)); err == nil {
				t.Error("Parse returned no error, want one")
			}
		})
	}
}

// Two responses run together are not one feed. A decoder stops at the end of the
// first value and says nothing about the rest, so this has to be refused here:
// what it would draw is a feed with no predictions, which is what a quiet Sunday
// draws as.
func TestMoreThanOneFeedInTheBodyIsAnError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		body string
	}{
		{"two feeds", `{"Header":{"Timestamp":1},"Entity":[]} {"Header":{"Timestamp":2},"Entity":[]}`},
		{"a feed and a fragment", `{"Header":{"Timestamp":1},"Entity":[]} {"junk"`},
		{"a feed and a number", `{"Entity":[]} 7`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(strings.NewReader(tc.body)); err == nil {
				t.Error("Parse returned no error, want one")
			}
		})
	}

	// One feed, with the whitespace a server is free to add, is one feed.
	if _, err := Parse(strings.NewReader(`{"Entity":[]}` + "\n\n")); err != nil {
		t.Errorf("Parse on one feed and a newline = %v, want no error", err)
	}
}

// An update that names no trip is about nothing, and it must not be stored under
// the empty id. A departure whose trip is missing would match it.
func TestAnUpdateThatNamesNoTripIsNotStored(t *testing.T) {
	t.Parallel()
	f := feed(t, `{"Header":{"Timestamp":1000},"Entity":[
		{"TripUpdate":{"Trip":{"ScheduleRelationship":3},"StopTimeUpdate":[
		{"StopId":"s1","Arrival":{"HasTime":true,"Time":1500}}]}}]}`)

	if f.Cancelled("") {
		t.Error(`Cancelled("") = true, want false`)
	}
	if at, ok := f.Arrival("", "s1"); ok {
		t.Errorf(`Arrival("", "s1") = %d, want nothing`, at)
	}
}
