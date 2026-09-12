//go:build darwin

package term

import "golang.org/x/sys/unix"

// Reading and writing a terminal's settings is one ioctl on every unix and a
// different number on each. These are Darwin's.
const (
	getTermios = unix.TIOCGETA
	setTermios = unix.TIOCSETA
)
