package cache

// Schema is the cache an ingest builds, and the cache every query here reads.
// It lives in one place so a test fixture and the real file cannot drift apart.
// A fixture that carried its own copy would keep passing while a query that
// depends on a column stopped working against the file on disk.
//
// Dates are written YYYYMMDD, which sorts and compares as text.
const Schema = `
CREATE TABLE routes (
    route_id TEXT PRIMARY KEY, short_name TEXT, long_name TEXT,
    route_type INTEGER, color TEXT, text_color TEXT, sort_order INTEGER
);
CREATE TABLE trips (
    trip_id TEXT PRIMARY KEY, route_id TEXT, service_id TEXT,
    headsign TEXT, direction_id INTEGER
);
CREATE TABLE stops (
    stop_id TEXT PRIMARY KEY, stop_code TEXT, name TEXT,
    lat REAL, lon REAL, location_type INTEGER, parent TEXT,
    -- Sparse but clean where present. A platform is never read out of a stop
    -- name, because 'CANTERBURY / AD. 860' carries an address and not a
    -- platform.
    platform TEXT
);
CREATE TABLE stop_times (
    trip_id TEXT, stop_id TEXT, seq INTEGER, arr INTEGER, dep INTEGER
);
CREATE TABLE calendar (
    service_id TEXT PRIMARY KEY,
    mon INTEGER, tue INTEGER, wed INTEGER, thu INTEGER,
    fri INTEGER, sat INTEGER, sun INTEGER,
    start_date TEXT, end_date TEXT
);
CREATE TABLE calendar_dates (service_id TEXT, date TEXT, exception INTEGER);
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
`

// Indexes are applied after the rows, because maintaining five of them across
// six million inserts costs more than building them once at the end. They are
// declared here beside the tables for the same reason the tables are here: a
// fixture that queries without them tests a plan the real file does not have.
const Indexes = `
CREATE INDEX idx_st_stop ON stop_times(stop_id, arr);
CREATE INDEX idx_st_trip ON stop_times(trip_id, seq);
CREATE INDEX idx_trips_route ON trips(route_id, direction_id);
CREATE INDEX idx_trips_service ON trips(service_id);
CREATE INDEX idx_cd_date ON calendar_dates(date);
`
