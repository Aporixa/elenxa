//go:build windows || plan9 || js || wasip1

package agent

import "os"

func openConfinedFile(root, name string) (*os.File, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.Open(name)
}
