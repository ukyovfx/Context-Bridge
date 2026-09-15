//go:build !windows

package safety

type FileIdentity struct {
	VolumeSerialNumber uint64
	FileID             string
}

func FilesystemIdentitySupported() bool { return false }

func FilesystemIdentity(string) (FileIdentity, error) {
	return FileIdentity{}, ErrFilesystemIdentityUnavailable
}
