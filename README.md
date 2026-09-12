# otransit

A terminal browser for OC Transpo schedules and live arrivals.

```
      ●●●●●●        otransit
    ●●●    ●●●
   ●●        ●●     OC Transpo schedules in your terminal
    ●●●    ●●●
      ●●●●●●
        ┃┃
────────────────────────────────────────────── ☁ mostly cloudy · 21°

 ❯ BILLINGS BRIDGE / BANK      #3034   6   Greenboro        2 min  sched
   ALTA VISTA / ROLLAND        #8626   44  Hurdman         28 min  2 late
   Bus                                 69 routes running today
   O-Train                             3 lines · scheduled times only
────────────────────────────────────────────────────────────────────────
 What are you taking?              type to find a stop · ↑↓ · ↵ · esc
```

It draws into a band at the bottom of the terminal and never takes the whole
screen, so the session you were in is still there when you leave.

## Using it

```bash
otransit update      # download today's feed and build the cache
otransit             # browse
```

Then: arrows and `↵` to drill down from bus or rail to a route, a direction, a
stop and its departures. Any letter searches stops by name or by code. `p` keeps
a board, and a kept board is on the first screen next time with its next
departure on the row. `delete` goes back, `esc` goes back, and on the first
screen either one leaves.

| | |
|---|---|
| `otransit [cache]` | browse |
| `otransit update [cache]` | download the published feed and rebuild the cache |
| `otransit ingest <dir> [cache]` | build the cache from an unzipped feed |
| `otransit logo [width]` | the startup mark |
| `otransit replay <dir> [semantic] [styles]` | replay a fixture, printing every frame |
| `otransit --version` | the version, and the data's attribution |

The cache lives in your cache directory and the pins beside your config, so
deleting the cache to recover from a bad ingest cannot take your pins with it.
Live predictions need a subscription key, and without one every time on screen
is a scheduled one. `.env.example` says where to put it.

## Building it

```bash
mise install         # the toolchain and tools, at the versions mise.toml pins
make check           # the gate: what CI runs, and all it runs
make                 # what else there is
```

## Reading it

The program is a model with two shells over it. `internal/app` decides what each
screen holds and what each key does. `internal/live` drives that model from a
terminal and a clock. `internal/replay` drives the same model from a script and
a fixed clock, and prints every frame. That second shell is why the tests can
make a claim about the whole program without a terminal.

| | |
|---|---|
| `internal/app` | the screens, the keys, the pins, the poller |
| `internal/cache` | every query, against a SQLite cache of the feed |
| `internal/gtfs` | the download and the ingest that build that cache |
| `internal/feeds` | the three live fetches, and the subscription key |
| `internal/live` · `internal/replay` | the two shells |
| `internal/render` | a screen as cells: rows, styles, the status bar, ANSI |
| `internal/term` | raw mode, the band, and decoding keys |
| `internal/realtime` · `internal/weather` · `internal/detour` | one parser each |

Four documents carry the rest:

- **[TRAPS.md](TRAPS.md)** is every property of the feed and of the terminal
  that has caused a defect. Read it before touching a query or anything that
  draws.
- **[GO.md](GO.md)** is the style the code follows, and what enforces it.
- **[TESTING.md](TESTING.md)** is how it is tested, and what is not.
- **[PORTING.md](PORTING.md)** is the record of how it was built against a
  second implementation of the same specification.

## Attribution

Contains information licensed under the Open Government Licence - City of
Ottawa. https://open.ottawa.ca/pages/open-data-licence

Not affiliated with, endorsed by, or sponsored by OC Transpo or the City of
Ottawa.
