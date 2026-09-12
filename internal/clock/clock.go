// Package clock holds one instant of a replay and answers where that instant
// sits in the service day.
//
// A service day is not a calendar day. Its origin is noon minus twelve hours,
// which is midnight on almost every day and the correct answer on the two days
// a year that are not twenty-four hours long. Wall-clock arithmetic reports
// 08:00 as 28800 seconds on every day, and on a day that lost an hour the true
// elapsed time is 25200.
//
// Nothing here reads the machine. The caller supplies the day, the moment and
// the zone.
package clock

import (
	"fmt"
	"time"
)

// Clock is one held instant, and the service day it belongs to.
//
// The zero value is not useful. Build one with At.
type Clock struct {
	// day is noon on the service date. It fixes the date and the weekday, and
	// it is not read off start: the origin of a day that lost an hour falls on
	// the day before, so a date read from start names the wrong day.
	day time.Time
	// start is the service day origin, and at is the held instant.
	start, at time.Time
}

// Of is the clock at one instant, on the service day that instant falls in.
//
// This is the only place a service day is worked out, and nothing here reads the
// machine: a replay hands in an instant it computed from a script and a running
// program hands in one it read from the clock, and no screen can tell which.
func Of(t time.Time) Clock {
	y, m, d := t.Date()
	noon := time.Date(y, m, d, 12, 0, 0, 0, t.Location())
	return Clock{day: noon, start: noon.Add(-12 * time.Hour), at: t}
}

// At holds the instant that date and hhmm name in zone. The date is written
// YYYY-MM-DD and the time is written HH:MM.
func At(date, hhmm string, zone *time.Location) (Clock, error) {
	day, err := time.ParseInLocation(time.DateOnly, date, zone)
	if err != nil {
		return Clock{}, fmt.Errorf("date %q is not a date written YYYY-MM-DD: %w", date, err)
	}
	clock, err := time.Parse("15:04", hhmm)
	if err != nil {
		return Clock{}, fmt.Errorf("time %q is not a time written HH:MM: %w", hhmm, err)
	}

	y, m, d := day.Date()
	return Of(time.Date(y, m, d, clock.Hour(), clock.Minute(), 0, 0, zone)), nil
}

// Now is the held instant, in seconds since the service day began. It can
// exceed 86400 on a day that gained an hour, and a schedule reaches 28:xx for
// its own reasons, so nothing may assume it is under a day.
func (c Clock) Now() int {
	return int(c.at.Sub(c.start) / time.Second)
}

// Date is the service date, written YYYYMMDD, which is how the cache stores it.
func (c Clock) Date() string {
	return c.day.Format("20060102")
}

// Yesterday is the service date before this one, written YYYYMMDD. The step
// back is taken from noon, which exists on every day, so a day that lost an
// hour cannot land it on the wrong date.
func (c Clock) Yesterday() string {
	return c.day.AddDate(0, 0, -1).Format("20060102")
}

// Weekday is the day of the week the service date falls on.
func (c Clock) Weekday() time.Weekday {
	return c.day.Weekday()
}

// Epoch is the held instant in seconds since 1970.
//
// A realtime feed stamps itself in absolute time, and a schedule is written in
// seconds on a service day. This and Second are where the two meet, and they
// are the only place either counting crosses into the other.
func (c Clock) Epoch() int64 { return c.at.Unix() }

// Second is where an absolute instant falls on this service day, in seconds
// from its origin. It reads the origin and not midnight, so a prediction on a
// day that lost an hour lands on the same axis a schedule does.
func (c Clock) Second(epoch int64) int {
	return int(epoch - c.start.Unix())
}

// Advance returns the clock secs real seconds later. The service day it
// belongs to does not move, because a replay never crosses one.
func (c Clock) Advance(secs int) Clock {
	c.at = c.at.Add(time.Duration(secs) * time.Second)
	return c
}
