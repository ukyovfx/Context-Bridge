//go:build !windows

package safety

import "testing"

func TestFilesystemIdentityIsNotInventedOnUnsupportedPlatforms(t *testing.T) {
	if FilesystemIdentitySupported() {
		t.Fatal("filesystem identity unexpectedly supported")
	}
	if _, err := FilesystemIdentity("."); err == nil {
		t.Fatal("unsupported filesystem identity returned success")
	}
}
