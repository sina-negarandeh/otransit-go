package detour

import (
	"strings"
	"testing"
)

// feed wraps items in the shape the CMS serves.
func feed(t *testing.T, items string) *Feed {
	t.Helper()
	f, err := Parse(strings.NewReader(
		`<?xml version="1.0" encoding="UTF-8"?>` + "\n" +
			`<rss version="2.0"><channel>` + items + `</channel></rss>`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

func item(title string, categories ...string) string {
	out := "<item><title><![CDATA[" + title + "]]></title>"
	for _, c := range categories {
		out += "<category><![CDATA[" + c + "]]></category>"
	}
	return out + "</item>"
}

// A detour is an item tagged both ways. Either tag on its own is not one, and
// neither spelling is a standard: both are the CMS's convention.
func TestADetourIsAnItemTaggedBothWays(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		item  string
		want  string
		count int
	}{
		{"both tags", item("Bridge closure", "Detours", "affectedRoutes-44"), "Bridge closure", 1},
		{"no route tag", item("Bridge closure", "Detours"), "", 0},
		{"no detour tag", item("Bridge closure", "affectedRoutes-44"), "", 0},
		{"another kind of message", item("Stop moved", "General Message", "affectedRoutes-44"), "", 0},
		// The CMS writes both in camel case. A port that lowered either one
		// would report no detours every week, which reads as a quiet week.
		{"the kind in lower case", item("Bridge closure", "detours", "affectedRoutes-44"), "", 0},
		{"the prefix in lower case", item("Bridge closure", "Detours", "affectedroutes-44"), "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := feed(t, tc.item)
			if got := f.Len(); got != tc.count {
				t.Errorf("Len() = %d, want %d", got, tc.count)
			}
			if got := f.For("44"); got != tc.want {
				t.Errorf("For(44) = %q, want %q", got, tc.want)
			}
		})
	}
}

// The routes are a list, and a route is on it or it is not. A list holding 4
// says nothing about 44, and one holding 444 says nothing either.
func TestARouteIsOnTheListOrItIsNot(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		list string
		want bool
	}{
		{"44", true},
		{"19, 42, 44, 48", true},
		{"19,42,44,48", true},
		{" 44", true},
		{"19,\t44", true},
		{"4", false},
		{"444", false},
		{"19, 42, 48", false},
		{"", false},
	} {
		t.Run(tc.list, func(t *testing.T) {
			t.Parallel()
			f := feed(t, item("Closure", "Detours", "affectedRoutes-"+tc.list))
			if got := f.For("44") != ""; got != tc.want {
				t.Errorf("with the list %q, For(44) found = %v, want %v", tc.list, got, tc.want)
			}
		})
	}
}

// Rail is on the same feed. Nothing here knows one mode from another.
func TestALineIsMatchedLikeARoute(t *testing.T) {
	t.Parallel()
	f := feed(t, item("Rail replacement", "Detours", "affectedRoutes-1"))
	if got := f.For("1"); got != "Rail replacement" {
		t.Errorf("For(1) = %q, want the rail notice", got)
	}
	if got := f.For("44"); got != "" {
		t.Errorf("For(44) = %q, want nothing", got)
	}
}

// The first one that names the route is the one shown. Two detours can name one
// route, and a screen has room for one line.
func TestTheFirstDetourNamingTheRouteIsTheOneReported(t *testing.T) {
	t.Parallel()
	f := feed(t, item("First", "Detours", "affectedRoutes-44")+
		item("Second", "Detours", "affectedRoutes-44"))
	if got := f.Len(); got != 2 {
		t.Errorf("Len() = %d, want 2", got)
	}
	if got := f.For("44"); got != "First" {
		t.Errorf("For(44) = %q, want %q", got, "First")
	}
}

// An item this program cannot read is skipped and the rest are kept, so one
// malformed entry does not empty the board of detours.
func TestABrokenItemDoesNotTakeTheGoodOnesWithIt(t *testing.T) {
	t.Parallel()
	f := feed(t, item("Ignore me", "General Message")+
		item("Keep me", "Detours", "affectedRoutes-44")+
		item("", "Detours"))
	if got := f.Len(); got != 1 {
		t.Errorf("Len() = %d, want 1", got)
	}
	if got := f.For("44"); got != "Keep me" {
		t.Errorf("For(44) = %q, want %q", got, "Keep me")
	}
}

// What parsed is counted, because a feed whose tags changed draws exactly like a
// week with no detours. Only the count tells the two apart.
func TestNothingParsedIsCounted(t *testing.T) {
	t.Parallel()
	if got := feed(t, "").Len(); got != 0 {
		t.Errorf("an empty channel has %d detours, want 0", got)
	}
	if _, err := Parse(strings.NewReader(`<rss><channel><item><title>cut off`)); err == nil {
		t.Error("Parse on a feed that is cut off = nil error, want one")
	}
}
