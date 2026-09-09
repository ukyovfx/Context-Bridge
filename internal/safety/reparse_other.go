//go:build !windows

package safety

import "os"

func isReparsePoint(os.FileInfo) bool { return false }
