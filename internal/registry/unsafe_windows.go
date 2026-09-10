//go:build windows

package registry

import "unsafe"

func unsafePointer[T any](value *T) unsafe.Pointer { return unsafe.Pointer(value) }
