// Package realtime reads one fetch of the GTFS-Realtime trip updates and
// answers what it said about a trip.
//
// The endpoint has `beta` in its URL, so its shape will change. A broken parse
// looks exactly like a quiet Sunday: no predictions, and every row reading
// sched. Parse therefore reports an error rather than returning an empty feed,
// and a caller that swallows that error has built the quiet Sunday itself.
//
// Nothing here reads the clock. Age arrives as an argument, because the only
// clock this program has belongs to the replay.
package realtime

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// cancelled is the relationship that takes a trip off the board. The other
// three mean it is running, including the two that mean the schedule never had
// it. The feed spells the word with one l and this program spells it with two.
const cancelled = 3

// secondsShown is how old a feed can be and still be counted in seconds.
const secondsShown = 120

// Feed is one fetch of the endpoint.
//
// The zero value is a feed that said nothing, which is not the same as no feed
// at all. A caller with no feed has no *Feed.
type Feed struct {
	// built is when the feed says it was made, in seconds since 1970, and
	// stamped reports whether it said. A feed that did not say is taken as
	// current, because the alternative is reporting it as 1970.
	built   int64
	stamped bool

	off     map[string]bool
	arrival map[call]int64
}

// call is one trip at one stop, which is what a prediction is about. A trip
// calls at a stop once, so the pair is the key.
type call struct{ trip, stop string }

// wire is the shape the endpoint serves. The pointers are the fields whose
// absence means something other than zero.
type wire struct {
	Header struct {
		Timestamp *int64
	}
	Entity []struct {
		TripUpdate *struct {
			Trip struct {
				TripID               string `json:"TripId"`
				ScheduleRelationship int
			}
			StopTimeUpdate []struct {
				StopID  string `json:"StopId"`
				Arrival *struct {
					HasTime bool
					Time    int64
				}
			}
		}
	}
}

// Parse reads a feed.
func Parse(r io.Reader) (*Feed, error) {
	var in wire
	// DisallowUnknownFields is not set: the endpoint adds fields without warning
	// and they are not this program's business.
	dec := json.NewDecoder(r)
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("reading the trip updates: %w", err)
	}
	// A decoder stops at the end of the first value and says nothing about what
	// follows, so two responses run together read as one good feed. That draws
	// as a feed with no predictions, which is what a quiet Sunday draws as, and
	// refusing it here is the only place the two are told apart.
	if dec.More() {
		return nil, fmt.Errorf("reading the trip updates: more than one feed in the body")
	}

	f := &Feed{off: map[string]bool{}, arrival: map[call]int64{}}
	if in.Header.Timestamp != nil {
		f.built, f.stamped = *in.Header.Timestamp, true
	}
	for _, e := range in.Entity {
		if e.TripUpdate == nil {
			continue
		}
		trip := e.TripUpdate.Trip.TripID
		// An update that names no trip is about nothing. Kept out rather than
		// stored under the empty id, which a departure with no trip would then
		// match.
		if trip == "" {
			continue
		}
		if e.TripUpdate.Trip.ScheduleRelationship == cancelled {
			f.off[trip] = true
		}
		for _, u := range e.TripUpdate.StopTimeUpdate {
			// No time is not a time. Taking the zero would put the trip at the
			// epoch, which draws as a departure that went fifty years ago.
			if u.Arrival == nil || !u.Arrival.HasTime {
				continue
			}
			f.arrival[call{trip: trip, stop: u.StopID}] = u.Arrival.Time
		}
	}
	return f, nil
}

// Arrival is when the feed expects a trip at a stop, in seconds since 1970.
func (f *Feed) Arrival(trip, stop string) (int64, bool) {
	at, ok := f.arrival[call{trip: trip, stop: stop}]
	return at, ok
}

// Cancelled reports whether the feed says a trip is not running.
func (f *Feed) Cancelled(trip string) bool { return f.off[trip] }

// Note is what the status bar says about a feed read at now.
//
// Seconds for the first two minutes, then whole minutes. A feed stamped ahead
// of the clock is not old: the endpoint's clock and ours are two clocks.
func (f *Feed) Note(now int64) string {
	age := int64(0)
	if f.stamped && now > f.built {
		age = now - f.built
	}
	if age <= secondsShown {
		return "live " + strconv.FormatInt(age, 10) + "s"
	}
	return "live " + strconv.FormatInt(age/60, 10) + "m old"
}
