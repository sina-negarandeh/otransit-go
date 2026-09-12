// The boards somebody kept, and what the pin key can do on the one being drawn.
//
// The file they came from is the shell's business. What is here is what a pin is,
// which two pins are the same pin, and how many of them the screen has room for.
package app

import "slices"

// kept is the boards somebody pinned: the lines the file holds, and the ones the
// cache can still draw.
//
// One type rather than two fields on App, because the two move together. Every
// add and every removal touches both, and the agreement between them is what
// makes a pin drawable, unpinnable and countable against the cap. Held here, a
// caller cannot get one half right.
type kept struct {
	// stored is every pin, resolvable or not. A stop that vanishes from one
	// export and returns in the next brings its pin back, so a pin is never
	// dropped for being unresolvable today.
	stored []Pin
	// live is the pins a screen can draw, in the order they were pinned. The
	// first screen works it out as it builds its rows, because that is the screen
	// that finds out.
	live []Pin
}

// A keep is what the pin key can do on the board being drawn.
type keep int

const (
	// unpinned: it can be kept, and there is room.
	unpinned keep = iota
	// pinned: it is kept, and the key takes it off.
	pinned
	// full: it is not kept and there is no room. Said out loud, because a key
	// that silently does nothing reads as a broken key.
	full
)

// hold takes what the pins file said. Nothing is resolved yet: the first screen
// does that, and until it has, nothing is drawable.
func (k *kept) hold(pins []Pin) { k.stored, k.live = pins, nil }

// resolved records the pins a screen could draw.
func (k *kept) resolved(live []Pin) { k.live = live }

// all is every pin, for the shell to write down. The order is the order they were
// pinned in, which is the order they are drawn in.
func (k kept) all() []Pin { return slices.Clone(k.stored) }

// state is what the pin key means for this board.
//
// The cap counts what can be drawn and not every line of the file, because a pin
// nothing can find takes no room on the screen.
func (k kept) state(want Pin) keep {
	switch {
	case k.has(want):
		return pinned
	case len(k.live) >= maxPins:
		return full
	}
	return unpinned
}

// has reports whether this board is kept.
func (k kept) has(want Pin) bool { return k.at(want) >= 0 }

// at is where a board's pin is, or -1. The name a pin was made under is no part
// of the question: a stop the feed renames is the same stop.
func (k kept) at(want Pin) int {
	return slices.IndexFunc(k.stored, func(p Pin) bool { return p.key() == want.key() })
}

// toggle keeps a board or stops keeping it, and does neither when there is no
// room for another.
//
// Both lists move together. A board that can be kept is one being looked at, so
// it resolves, and a board being unkept was drawn.
func (k *kept) toggle(want Pin) {
	switch k.state(want) {
	case pinned:
		k.stored = slices.Delete(k.stored, k.at(want), k.at(want)+1)
		if i := slices.IndexFunc(k.live, func(p Pin) bool { return p.key() == want.key() }); i >= 0 {
			k.live = slices.Delete(k.live, i, i+1)
		}
	case full:
	default:
		k.stored = append(k.stored, want)
		k.live = append(k.live, want)
	}
}
