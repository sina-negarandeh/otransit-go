// Package feeds asks the network what it knows.
//
// Three feeds, and nothing here parses one of them: each parser lives beside the
// payload it was written against and takes a reader. What belongs here is the
// request, the timeout, the subscription key, and what a refusal is called.
//
// None of it is tested, and that is deliberate: TESTING.md says the fetch layer
// is a thin wrapper, that the logic worth testing is extracted from it, and that
// nothing in the suite touches a socket.
package feeds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/sina-negarandeh/otransit-go/internal/detour"
	"github.com/sina-negarandeh/otransit-go/internal/realtime"
	"github.com/sina-negarandeh/otransit-go/internal/weather"
)

// Where each feed lives.
//
// on-118 is Ottawa in ECCC's citypage collection, which ECCC labels
// experimental. The updates feed is RSS out of a CMS. The realtime endpoint has
// beta in its path, so its shape will change, and that is why both of those
// parsers count what they read.
const (
	weatherURL = "https://api.weather.gc.ca/collections/citypageweather-realtime/items/on-118?f=json"
	detourURL  = "https://www.octranspo.com/en/feeds/updates-en/"
	tripsURL   = "https://nextrip-public-api.azure-api.net/octranspo/gtfs-rt-tp/beta/v1/TripUpdates"
)

// How long each feed is given.
//
// The two launch feeds are quick or they are nothing: a browser session lasts a
// minute and nothing waits on them. The realtime endpoint is slow under load and
// its answer is the one a person is actually looking at.
const (
	launchTimeout = 10 * time.Second
	tripsTimeout  = 45 * time.Second
)

// Weather is the current conditions in Ottawa.
func Weather(ctx context.Context) (weather.Conditions, error) {
	body, err := get(ctx, weatherURL, launchTimeout, nil)
	if err != nil {
		return weather.Conditions{}, fmt.Errorf("fetching the weather: %w", err)
	}
	defer body.Close() //nolint:errcheck // read-only, and the parse reports what it read

	return weather.Parse(body)
}

// Detours is what the updates feed has published.
func Detours(ctx context.Context) (*detour.Feed, error) {
	body, err := get(ctx, detourURL, launchTimeout, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching the updates feed: %w", err)
	}
	defer body.Close() //nolint:errcheck // as above

	return detour.Parse(body)
}

// Trips is what the realtime endpoint predicts, or the reason it would not say.
//
// The reason goes on the status bar, so every reason this package gives is kept
// to a few words. See brief and refused.
func Trips(ctx context.Context, key string) (*realtime.Feed, error) {
	body, err := get(ctx, tripsURL+"?format=json", tripsTimeout,
		map[string]string{"Ocp-Apim-Subscription-Key": key})
	if err != nil {
		return nil, err
	}
	defer body.Close() //nolint:errcheck // as above

	return realtime.Parse(body)
}

// get asks one feed and hands back its body for a parser to read.
//
// A body is never read into memory here. Each parser streams, and the realtime
// payload is megabytes of trip updates.
func get(ctx context.Context, url string, timeout time.Duration, headers map[string]string) (io.ReadCloser, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		return nil, err
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		// Classified here rather than by each caller, because one of the three
		// puts it on the status bar and none of them can afford to forget.
		return nil, brief(err)
	}
	if err := refused(resp.StatusCode); err != nil {
		resp.Body.Close() //nolint:errcheck // the status is the error, and nothing was read
		cancel()
		return nil, err
	}
	// The cancel outlives this function, because the body is read after it
	// returns. Closing the body is what releases it.
	return &closer{ReadCloser: resp.Body, cancel: cancel}, nil
}

// refused is what a status means. net/http returns no error for any of them, so
// a feed that said no arrives looking exactly like a feed that answered.
//
// Both of these reach the status bar, so both are short. What a person should do
// about a rejected key does not fit on one row beside a board, and .env.example
// says it where there is room.
func refused(status int) error {
	switch {
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return errors.New("the feed rejected the key")
	case status >= 400:
		return fmt.Errorf("the feed answered %d", status)
	}
	return nil
}

// bar is the room a refusal has. The status bar holds the note, the pin key and
// the two keys that leave, and the note is the part that gives way. A note past
// this takes the keys off the screen, and a person with no network then has no
// way to see how to quit.
const bar = 30

// brief is a transport failure in a few words.
//
// net/http puts the method and the whole URL into every one of them, and the
// realtime URL alone is 96 characters. Which way it failed is what a person can
// act on, and the URL is compiled in, so the URL is what goes.
func brief(err error) error {
	var call *url.Error
	if !errors.As(err, &call) {
		return err
	}
	switch {
	case call.Timeout():
		return errors.New("the feed timed out")
	case errors.Is(err, context.Canceled):
		return errors.New("the feed was not waited for")
	}
	return errors.New("the feed did not answer")
}

// closer releases the request's context when the body is closed.
type closer struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (c *closer) Close() error {
	defer c.cancel()
	return c.ReadCloser.Close()
}
