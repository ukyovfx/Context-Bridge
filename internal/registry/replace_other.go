//go:build !windows

package registry

import "os"

func replaceFile(source, destination string) error {
	return os.Rename(source, destination)
}
