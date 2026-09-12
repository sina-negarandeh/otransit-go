// Package detour reads the updates feed and answers which routes have one.
//
// The feed is RSS from a content system and not a specified format. A detour is
// an item tagged `Detours` that also carries a category beginning
// `affectedRoutes-`, and both spellings are that system's convention rather
// than anything agreed. If either changes, every screen reports no detours,
// which looks exactly like a week with none. Len is how a caller tells those
// apart: it counts what parsed.
package detour

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// The two tags an item needs, spelled as the feed spells them. Both are matched
// exactly: lowering either one finds nothing, every week, quietly.
const (
	kind   = "Detours"
	prefix = "affectedRoutes-"
)

// Feed is one fetch of the updates endpoint, holding only the detours.
type Feed struct {
	notices []notice
}

// notice is one detour: what it says, and which routes it is about.
type notice struct {
	title  string
	routes []string
}

// wire is the shape the endpoint serves.
type wire struct {
	Items []struct {
		Title      string   `xml:"title"`
		Categories []string `xml:"category"`
	} `xml:"channel>item"`
}

// Parse reads the feed. An item that is not a detour is skipped rather than
// refused: the endpoint carries general messages and stop notices as well, and
// one item this program does not understand must not take the others with it.
func Parse(r io.Reader) (*Feed, error) {
	var in wire
	if err := xml.NewDecoder(r).Decode(&in); err != nil {
		return nil, fmt.Errorf("reading the updates: %w", err)
	}

	f := &Feed{}
	for _, it := range in.Items {
		routes, ok := affected(it.Categories)
		if !ok {
			continue
		}
		f.notices = append(f.notices, notice{title: it.Title, routes: routes})
	}
	return f, nil
}

// affected is the routes an item is about, and whether it is a detour at all.
func affected(categories []string) ([]string, bool) {
	var routes []string
	tagged := false
	for _, c := range categories {
		if c == kind {
			tagged = true
			continue
		}
		list, ok := strings.CutPrefix(c, prefix)
		if !ok {
			continue
		}
		for _, r := range strings.Split(list, ",") {
			// Trimmed, because the feed writes the list for a person to read:
			// `affectedRoutes-19, 42, 44, 48`.
			if r = strings.TrimSpace(r); r != "" {
				routes = append(routes, r)
			}
		}
	}
	return routes, tagged && len(routes) > 0
}

// Len is how many detours parsed.
func (f *Feed) Len() int { return len(f.notices) }

// For is what the first detour naming this route says, or nothing when no
// detour names it. A route is on a list or it is not: a list holding 4 says
// nothing about 44.
func (f *Feed) For(route string) string {
	for _, n := range f.notices {
		for _, r := range n.routes {
			if r == route {
				return n.title
			}
		}
	}
	return ""
}
