package clock

import (
	"testing"
	"time"

	// Embedded so the changeover tests do not depend on the machine's zone
	// database. No test here reads the machine, and none reads the wall clock.
	_ "time/tzdata"
)

func toronto(t *testing.T) *time.Location {
	t.Helper()
	zone, err := time.LoadLocation("America/Toronto")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	return zone
}

func TestNowIsSecondsSinceTheServiceDayBegan(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, date, at string
		want           int
	}{
		{"the service day opens at zero", "2026-09-12", "00:00", 0},
		{"eight in the morning", "2026-09-12", "08:00", 8 * 3600},
		{"a half hour", "2026-09-12", "08:30", 8*3600 + 30*60},
		{"one minute to midnight", "2026-09-12", "23:59", 23*3600 + 59*60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := At(tc.date, tc.at, time.FixedZone("", -4*3600))
			if err != nil {
				t.Fatalf("At: %v", err)
			}
			if c.Now() != tc.want {
				t.Errorf("Now() = %d, want %d", c.Now(), tc.want)
			}
		})
	}
}

// The origin is noon minus twelve hours rather than midnight, and the two are
// not the same on a day that changes over. Midnight to noon is eleven real
// hours on 2026-03-08, so the origin lands at 23:00 the day before. A GTFS
// export covers about five weeks and the changeovers fall in March and
// November, so no fixture can hold either of these.
func TestOnADayThatLosesAnHourTheOriginIsAnHourBeforeMidnight(t *testing.T) {
	t.Parallel()
	// 2026-03-08 is the second Sunday in March. 02:00 EST becomes 03:00 EDT.
	c, err := At("2026-03-08", "01:00", toronto(t))
	if err != nil {
		t.Fatalf("At: %v", err)
	}
	if got, want := c.Now(), 7200; got != want {
		t.Errorf("Now() = %d, want %d. Wall-clock arithmetic gives 3600", got, want)
	}
}

func TestOnADayThatGainsAnHourTheOriginIsAnHourAfterMidnight(t *testing.T) {
	t.Parallel()
	// 2026-11-01 is the first Sunday in November. 02:00 EDT becomes 01:00 EST.
	// The origin is 01:00, so 00:30 belongs to the service day that ended.
	c, err := At("2026-11-01", "00:30", toronto(t))
	if err != nil {
		t.Fatalf("At: %v", err)
	}
	if got, want := c.Now(), -1800; got != want {
		t.Errorf("Now() = %d, want %d. Wall-clock arithmetic gives 1800", got, want)
	}
}

// This is what the origin buys. A trip written 08:00:00 leaves at 08:00 by the
// clock on the wall, on all three days, and the arithmetic needs no special
// case for the two that are not twenty-four hours long.
func TestATripAtEightIsEightHoursInOnEveryDay(t *testing.T) {
	t.Parallel()
	for _, date := range []string{"2026-09-12", "2026-03-08", "2026-11-01"} {
		t.Run(date, func(t *testing.T) {
			t.Parallel()
			c, err := At(date, "08:00", toronto(t))
			if err != nil {
				t.Fatalf("At: %v", err)
			}
			if c.Now() != 8*3600 {
				t.Errorf("Now() = %d, want %d", c.Now(), 8*3600)
			}
		})
	}
}

// The origin of 2026-03-08 falls on 2026-03-07. A service date read off the
// origin reports the day before, and every calendar lookup then asks about the
// wrong day.
func TestTheServiceDateIsTheNamedDayEvenWhenTheOriginFallsTheDayBefore(t *testing.T) {
	t.Parallel()
	c, err := At("2026-03-08", "01:00", toronto(t))
	if err != nil {
		t.Fatalf("At: %v", err)
	}
	if c.Date() != "20260308" {
		t.Errorf("Date() = %q, want %q", c.Date(), "20260308")
	}
	if c.Weekday() != time.Sunday {
		t.Errorf("Weekday() = %v, want Sunday", c.Weekday())
	}
}

// Midnight and the origin are the same instant only on a day that does not
// change over. The old test here advanced a clock by 86400 and checked it had
// moved 86400, which is true on every day, so it asserted nothing about the
// length of one.
func TestMidnightIsTheOriginOnlyOnADayThatDoesNotChangeOver(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, date string
		want       int
	}{
		{"an ordinary day", "2026-09-12", 0},
		{"the day that loses an hour", "2026-03-08", 3600},
		{"the day that gains one", "2026-11-01", -3600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := At(tc.date, "00:00", toronto(t))
			if err != nil {
				t.Fatalf("At: %v", err)
			}
			if c.Now() != tc.want {
				t.Errorf("midnight is %d seconds into the service day, want %d", c.Now(), tc.want)
			}
		})
	}
}

func TestTheServiceDateAndWeekdayComeFromTheNamedDay(t *testing.T) {
	t.Parallel()
	c, err := At("2026-09-12", "08:00", time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("At: %v", err)
	}
	if c.Date() != "20260912" {
		t.Errorf("Date() = %q, want %q", c.Date(), "20260912")
	}
	if c.Weekday() != time.Saturday {
		t.Errorf("Weekday() = %v, want Saturday", c.Weekday())
	}
}

func TestAdvanceMovesRealSecondsAndLeavesTheServiceDateAlone(t *testing.T) {
	t.Parallel()
	c, err := At("2026-09-12", "08:00", time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("At: %v", err)
	}
	got := c.Advance(90)
	if got.Now() != 8*3600+90 {
		t.Errorf("Now() = %d, want %d", got.Now(), 8*3600+90)
	}
	if got.Date() != "20260912" {
		t.Errorf("Date() = %q, want it unchanged", got.Date())
	}
	if c.Now() != 8*3600 {
		t.Errorf("Advance changed the receiver: Now() = %d", c.Now())
	}
}

func TestAMalformedDateOrTimeIsAnError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, date, at string }{
		{"a date that is not a date", "2026-13-40", "08:00"},
		{"a date in the wrong order", "12-09-2026", "08:00"},
		{"a time past the day", "2026-09-12", "25:00"},
		{"a time with no colon", "2026-09-12", "0800"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := At(tc.date, tc.at, time.UTC); err == nil {
				t.Errorf("At(%q, %q) = nil error, want one", tc.date, tc.at)
			}
		})
	}
}

// A board reads yesterday as well, because a trip written 25:10 on Friday is
// the one a person catches on Saturday morning.
func TestYesterdayIsTheServiceDateBeforeThisOne(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, date, want string
	}{
		{"an ordinary day", "2026-09-12", "20260911"},
		{"the first of a month", "2026-09-01", "20260831"},
		{"the first of a year", "2026-01-01", "20251231"},
		{"the day after a leap day", "2028-03-01", "20280229"},
		{"the day after the clocks go forward", "2026-03-09", "20260308"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := At(tc.date, "08:00", toronto(t))
			if err != nil {
				t.Fatalf("At: %v", err)
			}
			if got := c.Yesterday(); got != tc.want {
				t.Errorf("Yesterday() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A realtime feed is stamped in absolute time and a schedule is written in
// service-day seconds. These two turn one into the other, and the pair has to
// agree at the held instant on every day, including the two that are not
// twenty-four hours long.
func TestTheHeldInstantIsTheSameMomentInBothCountings(t *testing.T) {
	t.Parallel()
	zone := toronto(t)

	for _, tc := range []struct {
		name string
		date string
	}{
		{"an ordinary day", "2026-09-11"},
		{"the day that loses an hour", "2026-03-08"},
		{"the day that gains one", "2026-11-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := At(tc.date, "08:00", zone)
			if err != nil {
				t.Fatalf("At: %v", err)
			}
			if got := c.Second(c.Epoch()); got != c.Now() {
				t.Errorf("Second(Epoch()) = %d, want Now(), which is %d", got, c.Now())
			}
		})
	}
}

func TestAnInstantConvertsToSecondsOnTheServiceDay(t *testing.T) {
	t.Parallel()
	// 2026-09-11 08:00 at -04:00. The service day began at midnight, so the
	// held instant is 28,800 seconds in, and that is the stamp a feed built at
	// the same moment carries.
	const dayStart, heldAt = 1789099200, 1789128000

	c, err := At("2026-09-11", "08:00", time.FixedZone("", -4*3600))
	if err != nil {
		t.Fatalf("At: %v", err)
	}
	if got := c.Epoch(); got != heldAt {
		t.Errorf("Epoch() = %d, want %d", got, heldAt)
	}
	for _, tc := range []struct {
		name  string
		epoch int64
		want  int
	}{
		{"the day's own origin", dayStart, 0},
		{"the held instant", heldAt, 28800},
		{"a minute after it", heldAt + 60, 28860},
		{"an hour before the day began", dayStart - 3600, -3600},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := c.Second(tc.epoch); got != tc.want {
				t.Errorf("Second(%d) = %d, want %d", tc.epoch, got, tc.want)
			}
		})
	}
}

// Of is the one place a service day is worked out, so the clock a replay builds
// from a script and the clock a running program reads off the machine are the
// same kind of thing. A test names the instant either way, because no test here
// reads the machine.
func TestAnInstantBelongsToTheServiceDayItFallsIn(t *testing.T) {
	t.Parallel()
	zone := toronto(t)

	for _, tc := range []struct {
		name string
		at   time.Time
		date string
		now  int
	}{
		{"the middle of an afternoon", time.Date(2026, 9, 12, 14, 30, 0, 0, zone), "20260912", 14*3600 + 30*60},
		{"a minute past midnight", time.Date(2026, 9, 12, 0, 1, 0, 0, zone), "20260912", 60},
		{"the last minute of a day", time.Date(2026, 9, 12, 23, 59, 0, 0, zone), "20260912", 23*3600 + 59*60},
		// The day Toronto loses an hour, at the time the loss shows. Its origin
		// is eleven the night before, so one in the morning is two hours in and
		// not one. Eight in the morning is 28,800 on that day as on every other,
		// which is why this line names one o'clock: the rule is invisible at
		// eight, and a test that named it there would pass on a clock that read
		// the wall and called it elapsed.
		{"an hour that lost an hour", time.Date(2026, 3, 8, 1, 0, 0, 0, zone), "20260308", 7200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Of(tc.at)
			if got := c.Date(); got != tc.date {
				t.Errorf("Date() = %q, want %q", got, tc.date)
			}
			if got := c.Now(); got != tc.now {
				t.Errorf("Now() = %d, want %d", got, tc.now)
			}
			if got := c.Second(c.Epoch()); got != tc.now {
				t.Errorf("Second(Epoch()) = %d, want %d", got, tc.now)
			}
		})
	}
}

// The two ways in agree. A script naming a date and a time builds the same clock
// as the instant that date and time stand for.
func TestTheTwoWaysToBuildAClockAgree(t *testing.T) {
	t.Parallel()
	zone := time.FixedZone("", -4*3600)
	named, err := At("2026-09-12", "08:00", zone)
	if err != nil {
		t.Fatalf("At: %v", err)
	}
	held := Of(time.Date(2026, 9, 12, 8, 0, 0, 0, zone))
	if named != held {
		t.Errorf("At built %+v and Of built %+v, want the same clock", named, held)
	}
}
