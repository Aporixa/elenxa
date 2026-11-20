package agent

import (
	"os"
	"path/filepath"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

func TestOpenTaskLocalFileConfinesResolution(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "inside.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openTaskLocalFile(&actool.ToolContext{WorkingDir: root}, "inside.json")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()

	outside := filepath.Join(t.TempDir(), "outside.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.json")); err != nil {
		t.Fatal(err)
	}
	if file, err = openTaskLocalFile(&actool.ToolContext{WorkingDir: root}, "escape.json"); err == nil {
		file.Close()
		t.Fatal("symlink outside the task working directory was opened")
	}
	if file, err = openTaskLocalFile(&actool.ToolContext{WorkingDir: root}, "../outside.json"); err == nil {
		file.Close()
		t.Fatal("parent traversal was opened")
	}
}
