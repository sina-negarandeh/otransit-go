//go:build linux

package term

import "golang.org/x/sys/unix"

// Linux's numbers for the same two ioctls.
const (
	getTermios = unix.TCGETS
	setTermios = unix.TCSETS
)
