// The pins file's format: one line a pin, read and written here.
//
// Only this file knows the shape of a line. Where the file lives, when it is read
// and when it is written are the shell's, because a replay reads a fixture's pins
// and must never write them back.
package app

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// ParsePins reads a pins file: one pin per line, tab separated.
//
// A line carries the stop id, the stop code and the stop name, and then a route
// short name and a headsign when the pin was made by drilling. The code and the
// name are kept only so they can be written back: nothing resolves through them.
// Every screen asks the current cache instead, because an update replaces the
// whole database.
//
// A line that will not parse is reported, and the lines that did parse come back
// beside the error. A fixture treats that as fatal. A person's own file does not:
// one bad line they typed must not lose the pins above it.
func ParsePins(r io.Reader) ([]Pin, error) {
	var out []Pin

	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimRight(sc.Text(), "\n")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}

		f := strings.Split(line, "\t")
		if len(f) < 3 || strings.TrimSpace(f[0]) == "" {
			return out, fmt.Errorf("pins line %d: want a stop id, a code and a name, tab separated", n)
		}

		p := Pin{Stop: f[0], Code: f[1], Name: f[2]}
		switch len(f) {
		case 3:
		case 5:
			// A pin made by drilling carries the route it was made from, because
			// two routes to one terminus are not two routes to one place.
			p.Route, p.Headsign = f[3], f[4]
		default:
			return out, fmt.Errorf("pins line %d: a route needs a headsign beside it", n)
		}
		out = append(out, p)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("reading pins: %w", err)
	}
	return out, nil
}

// RenderPins writes the pins back in the form ParsePins reads.
//
// Tabs, because a stop name holds commas and slashes and never a tab, so nothing
// needs escaping and the file stays editable by hand. A pin made from a search
// writes three fields and one made by drilling writes five, which is what every
// pin wrote before a route was part of one: an older file reads back unchanged.
func RenderPins(pins []Pin) string {
	var b strings.Builder
	for _, p := range pins {
		fmt.Fprintf(&b, "%s\t%s\t%s", p.Stop, p.Code, p.Name) //nolint:errcheck // a strings.Builder cannot fail
		if p.Route != "" {
			fmt.Fprintf(&b, "\t%s\t%s", p.Route, p.Headsign) //nolint:errcheck // as above
		}
		b.WriteString("\n")
	}
	return b.String()
}

// Pinned sets what the pins file said. A replay reads the fixture's pins and
// never writes them back.
//
// Nothing is resolved by this. The first screen finds out which of them the cache
// can draw, because it is the screen that has to draw them.
func (a *App) Pinned(pins []Pin) { a.kept.hold(pins) }

// Outside sets the weather the rule carries. A world with no weather feed
// leaves it alone.
func (a *App) Outside(w Weather) { a.weather = w }

// Updates holds what the updates feed said. A screen about a route reads it
// through detourFor, and a replay never writes it back.
func (a *App) Updates(n Notices) { a.notices = n }

// Pins is every pin, for the shell to write down.
func (a *App) Pins() []Pin { return a.kept.all() }
