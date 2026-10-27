//go:build darwin || linux

package pty

import "golang.org/x/sys/unix"

// enableOutputProcessing turns OPOST|ONLCR back on after term.MakeRaw, so a
// bare LF written to the terminal also returns the carriage.
func enableOutputProcessing(fd int) error {
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return err
	}
	t.Oflag |= unix.OPOST | unix.ONLCR
	return unix.IoctlSetTermios(fd, ioctlSetTermios, t)
}
