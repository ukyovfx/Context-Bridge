package registry

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicReplaceFile is shared by Context Bridge-owned file migrations. It
// writes a same-directory temporary file, flushes it, and replaces the target
// using the platform-specific safe replacement primitive.
func AtomicReplaceFile(destination string, data []byte, mode os.FileMode) error {
	directory := filepath.Dir(destination)
	temp, err := os.CreateTemp(directory, ".contextbridge-migration-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := replaceFile(tempPath, destination); err != nil {
		return fmt.Errorf("replace Context Bridge-owned file: %w", err)
	}
	return nil
}
