//go:build windows

package safety

import (
	"encoding/hex"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type FileIdentity struct {
	VolumeSerialNumber uint64
	FileID             string
}

type fileIDInfo struct {
	VolumeSerialNumber uint64
	FileID             [16]byte
}

func FilesystemIdentitySupported() bool { return true }

func FilesystemIdentity(path string) (FileIdentity, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return FileIdentity{}, fmt.Errorf("%w: %v", ErrFilesystemIdentityUnavailable, err)
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return FileIdentity{}, fmt.Errorf("%w: %v", ErrFilesystemIdentityUnavailable, err)
	}
	defer windows.CloseHandle(handle)
	var info fileIDInfo
	if err := windows.GetFileInformationByHandleEx(handle, windows.FileIdInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return FileIdentity{}, fmt.Errorf("%w: %v", ErrFilesystemIdentityUnavailable, err)
	}
	if info.VolumeSerialNumber == 0 || info.FileID == [16]byte{} {
		return FileIdentity{}, ErrFilesystemIdentityUnavailable
	}
	return FileIdentity{VolumeSerialNumber: info.VolumeSerialNumber, FileID: hex.EncodeToString(info.FileID[:])}, nil
}
