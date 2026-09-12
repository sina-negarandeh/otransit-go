# Porting

**This document has expired.** It was instruction while this implementation was
incomplete. On 2026-09-12 the comparison went empty: every fixture in
`conformance/`, at text, semantic and styles, byte for byte, and the suite
digest `dba846117b04` that the other implementation's own test asserts.

What is still instruction moved into this document and
[TRAPS.md](TRAPS.md): the four rules the
replay command obeys, and how to read a mutation campaign. The rest is below,
as a record of how it went.

## Two implementations, one contract

There are two programs. This one, and
[otransit-rust](https://github.com/sina-negarandeh/otransit-rust). They read the
same GTFS cache, the same realtime feed, the same detours and the same weather,
and they show a person the same thing.

The Rust implementation came first, so it was the reference. That was a fact
about the order they were written in and not about which is correct. Twice the
reference turned out to be wrong and was fixed: a selection highlight that was
set and never applied, and a direction order left to the database. Three times a
documented rule turned out to be an observation, and the artifact overruled the
document.

The contract is a directory of fixed worlds called `conformance/`, and its
`README.md` is the specification. It lives in the Rust repository beside the
cache slice it reads.

## Running the comparison

Both repositories are cloned side by side, and the Rust binary produces the
reference on demand.

```bash
make reference                      # rebuild the other implementation
make diff FX=narrow                 # one fixture, text
make diff FX=narrow OBS=semantic    # and the decisions behind it
make diff FX=narrow OBS=styles      # and the colours
make diff                           # every fixture at once
```

A digest is only a digest with its algorithm beside it. `shasum` here means
SHA-1, and comparing a SHA-256 against it once cost a round: two hashes of one
artifact under two algorithms disagree exactly like a moving binary does. Hash
twice, with the same command, and check the result against the expected value.

## The order it was built in

Fourteen fixtures, in eight passes, smallest world first.

| pass | fixtures | what it forced |
|---|---|---|
| 0 | `empty` | the walking skeleton, and a day with no service |
| 1 | `platforms`, `quiet-feeds`, `filter` | the screens, the keys, the pins |
| 2 | `after-midnight`, `pin-onward` | a service day that reaches past 24:00 |
| 3 | `weather-night`, `weather-unknown` | a feed that is experimental on purpose |
| 4 | `too-narrow` | the width ladder, and a rule with nothing on it |
| 5 | `pinned-stop` | realtime, and a cancelled trip |
| 6 | `narrow`, `drilldown` | the columns that give way, and detours |
| 7 | `feed-down`, `cadence` | a feed that refuses, and a clock that moves |

Two things were true of every pass. The diff found what the tests did not, and
the tests found what the diff could not. A defect that no fixture reaches is
still a defect: the stop list of one direction was ordered by the union of every
trip's stops, which draws a path no bus takes, and route 44's variants agree so
no fixture ever noticed.

## What remains

Two of the three things this document prescribed for the day the diff went empty
are still open, and neither is done here.

1. The artifact digest belongs in this repository's own test, so a change to
   either side has to be accepted rather than noticed later. `conformance/` is a
   gitignored symlink into the other checkout, so such a test has to skip when it
   is absent, and that shape needs deciding.
2. The comparison belongs in CI, where the Rust toolchain and the fixture slice
   are not present today. The workflow in `.github/` has never run.

## What keeps a replay honest

The replay command stays, because the comparison stays. Four rules decide
whether it means anything, and breaking any of them produces an artifact that
diffs cleanly and proves nothing.

1. **Nothing reads the machine.** The script names the date, the time and the
   UTC offset. Never call `time.Now`. Never read the local zone. The same
   fixture hashes the same in every timezone.
2. **Every key goes through the handler the event loop uses.** A replay with its
   own key dispatch is an oracle for a program nobody runs.
3. **A frame is one whole turn of that loop.** Move the clock, poll the feed,
   refresh the screen, then draw. A replay that performs three of those four
   holds its own opinion of what a frame is.
4. **A replay never writes to the fixture directory.** The pins are read into
   memory. A script that presses the pin key must not edit the world it replays.
