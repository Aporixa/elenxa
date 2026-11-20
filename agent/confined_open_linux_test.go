//go:build linux

package agent

import (
	"path/filepath"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
	"golang.org/x/sys/unix"
)

func TestOpenTaskLocalFileRejectsFIFOWithoutBlocking(t *testing.T) {
	root := t.TempDir()
	if err := unix.Mkfifo(filepath.Join(root, "scanner.fifo"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openTaskLocalFile(&actool.ToolContext{WorkingDir: root}, "scanner.fifo")
	if err == nil {
		file.Close()
		t.Fatal("FIFO was accepted as scanner input")
	}
}
