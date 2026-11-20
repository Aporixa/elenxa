//go:build linux

package agent

import (
	"os"

	"golang.org/x/sys/unix"
)

func openConfinedFile(root, name string) (*os.File, error) {
	rootFD, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootFD)
	fd, err := unix.Openat2(rootFD, name, &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	})
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), name)
	if file == nil {
		unix.Close(fd)
		return nil, os.ErrInvalid
	}
	return file, nil
}
