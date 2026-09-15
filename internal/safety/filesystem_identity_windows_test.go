//go:build windows

package safety

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemIdentityEquivalentPathAndRename(t *testing.T) {
	root := t.TempDir()
	first, err := FilesystemIdentity(filepath.Join(root, "."))
	if err != nil {
		t.Fatal(err)
	}
	equivalent, err := FilesystemIdentity(filepath.Join(root, "."))
	if err != nil {
		t.Fatal(err)
	}
	if first != equivalent {
		t.Fatalf("equivalent paths differ: %#v %#v", first, equivalent)
	}
	moved := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-moved")
	if err := os.Rename(root, moved); err != nil {
		t.Fatal(err)
	}
	renamed, err := FilesystemIdentity(moved)
	if err != nil {
		t.Fatal(err)
	}
	if first != renamed {
		t.Fatalf("rename changed physical identity: %#v %#v", first, renamed)
	}
}

func TestFilesystemIdentityReplacementAndCopyDiffer(t *testing.T) {
	parent := t.TempDir()
	original := filepath.Join(parent, "workspace")
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := FilesystemIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(original); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(original, 0o755); err != nil {
		t.Fatal(err)
	}
	replaced, err := FilesystemIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	if first == replaced {
		t.Fatal("replacement unexpectedly retained physical identity")
	}
	copyPath := filepath.Join(parent, "copy")
	if err := os.Mkdir(copyPath, 0o755); err != nil {
		t.Fatal(err)
	}
	copyID, err := FilesystemIdentity(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	if replaced == copyID {
		t.Fatal("copied directory unexpectedly shared physical identity")
	}
}
