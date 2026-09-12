# Testing approach

How this project is tested, and why. Read this before you write a test. Read it
again before you change code that has one.

## Contents

- [Three rules](#three-rules)
- [The layers](#the-layers)
- [Known traps the query layer must be tested against](#known-traps-the-query-layer-must-be-tested-against)
- [Anomalies the parser must be tested against](#anomalies-the-parser-must-be-tested-against)
- [The fixtures](#the-fixtures)
- [Making code testable](#making-code-testable)
- [Prove the test, not just the code](#prove-the-test-not-just-the-code)
- [Conventions](#conventions)
- [What we deliberately do not test](#what-we-deliberately-do-not-test)
- [Adding a test: checklist](#adding-a-test-checklist)

## Three rules

### 1. Write the test first

Red, then green, then refactor. A bug fix is no exception. Reproduce the bug as
a failing test before you change the code. A fix with no failing test in front
of it is a guess that happened to work.

This matters more here than usual, because the bugs in this program fail
quietly. A stale trip id shows as "no buses tracked". A broken parse shows as
`sched` on every row. A test for a silent failure is worth nothing until
somebody has seen it fail.

### 2. Isolation means a controlled world, not no dependencies

Test a function that queries SQLite against a database the test built. It holds
exactly the three stops and two routes the test is about. That is isolated. It
is deterministic and fast, with no hidden state and no fixture shared between
tests.

A test that runs against the real cache is not isolated. That file holds
hundreds of megabytes of live data and it changes daily.

The realtime parser works the same way. Isolated means a captured payload in
`testdata/`, not a network call.

The question that matters is whether the test controls every input. It is not
whether the function uses a dependency.

### 3. No test may use live data, the network, or the clock

Not the real cache, not the published feed, not the realtime API, and not
`time.Now`. A test that asserts "route 7 runs today" passes until OC Transpo
changes the service. Then it fails at 3am for a reason that has nothing to do
with the code.

The test builds every input, or a checked-in fixture supplies it.

## The layers

| Layer | Isolated by | Target |
|---|---|---|
| **Pure functions** | nothing to isolate | every branch |
| **Query layer** | an in-memory SQLite database the test builds | every function, plus every trap below |
| **Ingest** | a directory of real GTFS files in `t.TempDir()` | every default, every malformed file, every table |
| **Parsers** | a checked-in payload in `testdata/` | every field, plus every anomaly below |
| **Policy** | separated from the code that performs the effect | every branch |
| **Render** | a fixed-size buffer the test inspects | does each screen draw, do the columns align |
| **Terminal** | a pty | no alternate screen, clean exit, clear error with no tty |
| **The loop** | channels a test fills, and a viewport it reads | what a key, a tick and each feed do to one turn |
| **Shell** | none | not tested |

Maximal coverage means one thing here. Everything reachable with no I/O has a
test. The shell has none on purpose. Testing it means mocking the world, and
those mocks are more likely to be wrong than the twenty lines they cover.

## Known traps the query layer must be tested against

These are real properties of the OC Transpo feed.

- **Booking-period duplicates.** `route_id` `7` and `7-1` are the same route in
  different service periods. A query must deduplicate by `short_name` and filter
  by the active service date.
- **Times past 24:00.** `arrival_time` reaches `28:xx`. A trip scheduled for
  `25:10` on Friday is the one you catch at `01:10` on Saturday. It must appear
  on Saturday's board.
- **Service-day calendars.** `calendar_dates` adds and removes services on given
  dates. Honour both exception types.
- **A `stop_code` is not unique.** One code can cover seven platforms.
- **A `platform_code` is rare.** A small minority of stops carry one. Never read
  a platform out of a stop name.
- **Ordering.** A board is ordered by actual arrival. Assert the order after the
  code applies the realtime, not before.

## Anomalies the parser must be tested against

The realtime endpoint has `beta` in its URL, so its shape will change.

- PascalCase field names, with a `HasX` boolean beside every optional `X`.
- No `Delay` field. There are only absolute `Arrival.Time` epochs.
- `ScheduleRelationship = 3`, which is CANCELED, with an empty
  `StopTimeUpdate` list.
- `ScheduleRelationship = 8`, which is not in the GTFS-RT specification. These
  are added or unscheduled trips. Their trip ids are negative and are absent
  from the static feed.
- Entries that carry only `Departure` and no `Arrival`.
- `HasTime: false` beside a `Time` value that means nothing.
- Predictions in the past.

A parse that silently returns zero arrivals must look different from a feed with
no active trips. Assert on counts, not only on the absence of an error.

## The fixtures

A fixture builder lives beside the code it serves, in a `_test.go` file, so it
never reaches the release binary.

Build the cache fixture through the same schema the program creates at ingest.
Never copy the schema by hand. A fixture that holds its own copy drifts. The
drift stays invisible until a query passes against the fixture and fails against
the real file.

The realtime fixture needs one method per anomaly above. No test should write
PascalCase JSON by hand.

The rules these fixtures exist to keep:

- **In memory, or in `t.TempDir()`.** The unit suite must finish in under a
  second. A suite fast enough to keep up with editing is a suite people run.
- **The same schema as production.** Never copy it by hand.
- **Declarative and minimal.** A test about times after midnight declares one
  trip, not a synthetic city.
- **No fixture shared between tests.** Build a fresh one in each test. Shared
  state creates order dependencies, which are their own class of bug.
- **`t.Cleanup` removes what the test made.** `t.TempDir` already does this.

## Making code testable

Some bugs cannot be tested as the code was first written. That is a code
problem, not a testing problem. The pattern is a functional core inside an
imperative shell. The decision is a pure function. The effect is a thin wrapper
around it.

**Time is an input, never a global.** Pass the service date and the instant.
Never call `time.Now` below the shell. Cases after midnight and at the last bus
must be reproducible.

**Policy is separate from effect.** The rule that decides the next poll and the
rule that decides whether a failure replaces the board are both pure functions.
The loop that performs the fetch is thin.

**Ordering belongs outside the UI.** Sorting a board by actual arrival is a
function a test can call.

**A background result can be supplied.** The code that draws a detour is
unreachable from a test until a test can hand the detour in.

Do these changes while you write the tests that need them. Do not save them for
a separate project.

## Prove the test, not just the code

A test that nobody has seen fail is unproven. After you write one, break the
code and confirm the test fails. It must fail for the right reason, and it
should fail alone.

A vacuous test is one that holds whether or not the code is right. Ask what
would have to be true for the assertion to hold while the code is wrong. The
common shape is an assertion whose effect has two possible causes. A test that
looks for an absent string passes when a truncation removed the string for an
unrelated reason. A test that counts rows across several screens passes when one
screen gains a row and another loses one.

The race detector needs the same treatment. `go test -race` reports only the
races it observes, so a concurrent path with no test proves nothing.

## Conventions

**Name a test as a behaviour claim, not as a function name.**

```go
func TestDeparturesAreOrderedByActualArrivalNotSchedule(t *testing.T) {}
func TestATripScheduledPastMidnightAppearsOnTheNextDay(t *testing.T) {}
```

Do not write `TestDepartures` or `TestSearch2`. The name is the specification.
When it fails in CI, the name alone must say what broke.

**A table test uses subtests, and each case is named.** A table is the Go idiom
and it is the right shape for a pure function with many inputs. Give every case
a name and run it through `t.Run`. Without that, one failure reports the line
number of the loop and the other cases never run.

```go
for _, tc := range []struct {
    name          string
    secs, now     int
    want          string
}{
    {"due now", 600, 600, "due"},
    {"past midnight", 240, 86_280, "4 min"},
} {
    t.Run(tc.name, func(t *testing.T) { ... })
}
```

**One behaviour per test function.** A table of inputs to one function is one
behaviour. Five unrelated assertions are not. A test that asserts five things
fails on the first and hides the other four.

**Assert the property, not the snapshot**, wherever a property exists. A check
that a slice is sorted survives a schedule change. A check that the first value
is `09:43` does not.

**Use `t.Helper` in every assertion helper.** Without it the failure reports the
line inside the helper, and every failure in the package points at the same
line.

**A test that reads the machine finds the machine.** Every test of the
subscription key isolates `HOME`, the working directory and all four variable
names before it asks for one. One that did not found the real key in
`~/.config/otransit/.env` and printed it in a failure message.

**Use `t.Parallel` where the test owns all its state.** The fixtures above are
built per test, so most tests qualify. A test that reads a shared file does not.

**Every bug gets a regression test that carries its story.** A comment above the
test says what the original defect was. The name says what must hold.

## What we deliberately do not test

Written down so nobody argues it again.

- **The standard library and the dependencies.** A test of a dependency tests
  the wrong thing.
- **The network.** The fetch layer is a thin I/O wrapper. Extract its logic,
  which is the not-modified handling and the empty-body rejection, and test
  that. Do not test the socket. Unpacking the archive is not the network: a test
  builds a zip in a temporary directory and asserts what lands where.
- **The download, and the ingest of a full export.** Both need the published
  feed. Run `otransit update` by hand instead, then replay the fixtures against
  the cache it built. `make check` must never need it.
- **The three live fetches.** `internal/feeds` is the request, the timeout and
  the header, and it has no tests of its own. What is worth testing was taken
  out of it: which place the key comes from, which spellings of its name are
  accepted, what a status means, and what a failed fetch amounts to. Each one is
  a function a test calls with no socket in reach.
- **How a terminal draws what we send.** What we emit is ours, and a pty test
  covers it. What a terminal emulator paints is not ours to assert.
- **The exact rendered layout, as a snapshot in a unit test.** A snapshot breaks
  on every cosmetic change. Somebody then updates all of them at once, which
  makes every snapshot worthless. Assert structural properties instead. The
  columns align, the row limit bounds the board, and nothing exceeds the width.
  The one whole-output comparison in this project is the conformance artifact,
  which is a contract with a second implementation rather than a convenience.
- **Performance, as a unit test.** A timing assertion is flaky. Keep the
  benchmark as `go test -bench`, which reports a number and never fails a build.

## Adding a test: checklist

- [ ] Does it fail before the fix, and for the right reason? Run it and watch.
- [ ] **Did you break the code and watch it fail?** An untried test is unproven.
- [ ] Does the name state the behaviour instead of the function?
- [ ] Does it control every input? No clock, no network, no real cache.
- [ ] Does it build its own fixture instead of sharing one?
- [ ] Does it assert a property, where a property exists?
- [ ] If it is a table, does every case have a name and a `t.Run`?
- [ ] Does it test our code and not a dependency?
- [ ] If it is a regression test, does a comment record the original bug?
- [ ] Does it run in milliseconds?

## Breaking the code on purpose

After you write a test, break the code and watch the test fail. A campaign of
those is how this program was checked, and the number it produces is a
discrimination benchmark and not a coverage percentage. It is comparable only
with its own history, and a regression can hide inside a gain, so compare the
per-mutation outcomes and never only the total.

Three outcomes are worth telling apart. A mutation the tests **catch** is the
point. One that is **proven equivalent** changes no output anywhere, and the
proof belongs beside it. One that is **unpinnable** differs only where the
language leaves the behaviour undefined, and no portable test can catch it: say
so out loud rather than writing a test that cannot fail.

The defects this program produces cluster in a few shapes. Write mutations of
those:

- `nil` where a value is missing, and a zero that means something else.
- a map iterated in random order, or a sort that is not total.
- an integer division that truncates the wrong way.
- a slice aliased after `append`.
- a goroutine that races.
- a test whose expectation is computed by the code it is testing.
