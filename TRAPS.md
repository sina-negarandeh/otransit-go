# Traps

Each of these is a property of the OC Transpo data or of the terminal. Each one
has caused a defect in an implementation of this program, or has come close to
it. None of them is inferable from the code, which is why they are written down.

Read this before you touch the query layer, the ingest, or anything that draws.

- `route_id` repeats across booking periods. Route 7 appears as both `7` and
  `7-1`. Deduplicate by `short_name` and filter by the active service date.
- `arrival_time` goes past 24:00 and reaches `28:xx`. Those trips belong to the
  previous service day. A board queries today and yesterday, and shifts
  yesterday's rows back by 86,400 so both sit on one axis. Everything after that
  point assumes that axis, including a realtime prediction.
- Daylight saving cannot be tested from a feed. A GTFS export covers about five
  weeks, and the changeovers are in March and November, so no export holds one.
  The service-day origin is noon minus twelve hours for this reason. Cover it
  with unit tests that name the dates, and with no fixture.
- `stop_code` is not unique. One code can cover seven platforms.
- `platform_code` is correct but rare. A small minority of stops carry one.
  Never read a platform out of a stop name. `VANTAGE / AD. 303` is not platform
  303.
- The realtime endpoint has `beta` in its URL, so its shape will change. A
  broken parse looks the same as "no buses are running". Count what parsed.
- The O-Train has no realtime data at all. Every rail row reads `sched`.
- The weather feed is ECCC's citypage collection, which ECCC labels
  *experimental*. Its `windChill` and `humidex` are published even when they
  make no sense. Ottawa served `windChill: -2` at 20.7 degrees in light rain,
  flagged `qaValue: 100`. Read neither field. The glyph comes from `iconCode`
  and not from the condition text. Codes 30 to 39 are the night forms of 0 to 9.
  A code the program does not know draws no glyph, because a stand-in would
  collide with the separator in the label.
- Every glyph the program draws must occupy one cell. Measure in runes, and
  check the width. A double-width glyph shifts the alignment silently. `⛅` and
  `⚡` are Wide, which is why the weather line uses `☁` and `⛆`.
- The updates feed is RSS from a CMS, not a specified format. The
  `affectedRoutes-` tag is a convention. If it changes, every screen reports no
  detours, which looks the same as a week with none. Count what parsed.
- A pin stores the board it was made from. That is a stop id, plus a route short
  name and a headsign when the pin was made by drilling. An update replaces the
  whole database, so an id from an old cache can point at nothing, and a route
  can stop running. Resolve both through the current cache. Hide the row when
  either fails to resolve. Never delete the pin, because the stop and the route
  can come back.
- Two routes to one terminus are not two routes to one place. 44 and 48 both end
  at Billings Bridge, and the 48 runs a corridor the 44 never touches. A pin
  must not offer one when you pinned the other. The route belongs in the pin's
  identity and not only in its display.
- Pinned stops are the only data that outlives the process. They live beside the
  config file and not in the cache. Deleting the cache is how a user recovers
  from a bad ingest, and that must not delete the pins. Write one the moment it
  is made: the way out is not always the program's decision, because a terminal
  that closes takes the process with it.
- A pin is a stop and a direction, never a name. The file carries the stop's
  code and name so a person can read it, and nothing resolves through them: the
  feed rewrites names, and a renamed stop is the same pin. Compare the three
  fields that identify one.
- Five pins is the cap, which is the first screen's arithmetic: eight rows, two
  for the modes, and one for the blank that says the two groups are different
  kinds of thing. The cap counts the pins the cache can still find, so a pin
  whose stop vanished costs no room. At the cap the board's hint reads `pins
  full` and names no key, because there is no gesture to offer.
- Delete goes back. It takes a letter off the filter, and with nothing typed it
  walks up a level, which on the first screen means the program is done. That is
  the same floor esc reaches, so both keys end it there.
- `your_key_here` is not a subscription key. `.env.example` is committed with
  it, and somebody who copied the template and never edited it has no key:
  scheduled times only is the answer, not a request to the endpoint about a
  placeholder.
- A service day starts its clock at zero, so a poller owed an attempt at 30,600
  seconds would never come due again after midnight. A clock that lands on a new
  day is owed one at once.
- On a list screen, every letter goes to the filter. A key with another meaning
  can only work where typing does nothing, which is the departures board. Do not
  guard such a key on "the filter is empty". That is true at the start of every
  search, so the key would take the first letter the user types. `p` and `q` are
  the two keys that have another meaning, and the board is the only screen where
  they do. 58 stops begin with Q, 139 with J and 221 with K.
- The subscription key is read from three places, in this order: the
  environment, `./.env`, then `~/.config/otransit/.env`. The third is where it
  lives on a real machine, so one key serves both implementations and neither
  repository holds a copy. Accept all four historical variable names, because a
  key file written for either program must work for the other.
- A missing key is not an error. It means scheduled times only, and the status
  bar says so. The realtime poller is the only thing that needs the key. The
  updates feed and the weather feed need none, so "no key" does not mean "no
  network".
- Every file in the export carries a UTF-8 byte order mark. `encoding/csv`
  leaves it on, so the first column of each file is named with an invisible
  character in front of it. Nothing asks for that name, so every value in the
  column reads as empty. The ingest strips anything invisible from a column
  name.
- A 304 Not Modified arrives as an ordinary success with an empty body, and so
  does a 404: `net/http` returns an error for neither. Classify the status
  before a byte reaches the disk. A 304 written to a file is a zero-byte
  archive, and the complaint arrives later in the words of the zip reader.
- `route_text_color` is not the colour to draw. The feed publishes white on the
  teal of the 4, which is the harder of the two to read, and 103 of the 175
  routes disagree with a legible answer in the same direction. The badge
  computes black or white from the colour behind it, by WCAG contrast.
- `route_sort_order` is not the order a person reads routes in. It puts rail
  line 1 after line 2. Order by the number a name starts with, and put every
  name that starts with a letter after all of them.
- Where two rows tie, the export's own order decides. Two routes call in the
  same second, and three stops are called TERMINAL / SANDFORD FLEMING. Nothing
  in the data separates them, so the cache keeps the order the file was written
  in and the queries order by `rowid` last. A tidier rule invents an order the
  data does not have.
- A search is bounded twice. It ranks four times the stops it offers, then drops
  the ones with no service today, then stops at the limit. So a hundred dead
  platforms in front of a live one hide it. That is the bound, and it is what
  keeps a search fast enough to run between keystrokes.
- `direction_id` does not name a direction. Rail line 1 runs one trip towards
  Lyon under each id, so grouping by it offers "toward Lyon" twice and asks a
  person to choose between two identical rows. A direction is its headsign.
- Block art is painted, never drawn. A cell of the startup mark is a space
  carrying a background colour. SF Mono's U+2588 does not fill the cell and the
  font has no shade characters at all, so macOS substitutes a font with other
  metrics and the picture shears. Paint is the terminal's job, not the font's.
- The mark above the viewport draws no rule, and the mark on its own draws one.
  The viewport's top edge is what the pole stands on, and that edge repaints and
  carries the weather. `otransit logo` opens no viewport, so it has to draw the
  line itself. Forgetting that left the one command whose only job is to show
  the mark showing a pole standing on nothing.
- A route colour is not always a colour the palette can use. The line down the
  left of a drilled screen takes the route's colour only when the colour reads
  as a rule beside the text: not white, which outshines the stop names, and not
  `#6d6e70`, which is exactly the grey a pole number uses. Neither is rejected
  for being dull. A rail line's red is darker than that grey and is the colour
  most worth having.
- A cache is built, never added to. `stop_times` carries no key to replace on,
  so a second ingest into one file holds every departure twice and every board
  draws each bus two times. The ingest removes the file first, and `update`
  builds beside the live cache and renames.
