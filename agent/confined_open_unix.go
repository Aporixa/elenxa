//go:build aix || darwin || dragonfly || freebsd || netbsd || openbsd || solaris

package agent

import (
	"os"

	"golang.org/x/sys/unix"
)

func openConfinedFile(root, name string) (*os.File, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.OpenFile(name, os.O_RDONLY|unix.O_NONBLOCK, 0)
}
