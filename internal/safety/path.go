package safety

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func InspectRoot(root string) (exists, isDir, isFilesystemRoot, hasReparse bool, err error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return false, false, false, false, fmt.Errorf("absolute root: %w", err)
	}
	clean := filepath.Clean(abs)
	volume := filepath.VolumeName(clean)
	remainder := strings.TrimPrefix(clean, volume)
	isFilesystemRoot = remainder == string(filepath.Separator) || remainder == ""

	info, statErr := os.Lstat(clean)
	if errors.Is(statErr, os.ErrNotExist) {
		return false, false, isFilesystemRoot, false, nil
	}
	if statErr != nil {
		return false, false, isFilesystemRoot, false, fmt.Errorf("inspect projects root: %w", statErr)
	}

	current := clean
	for {
		item, itemErr := os.Lstat(current)
		if itemErr != nil {
			return true, info.IsDir(), isFilesystemRoot, false, fmt.Errorf("inspect path component: %w", itemErr)
		}
		if item.Mode()&os.ModeSymlink != 0 || isReparsePoint(item) {
			return true, info.IsDir(), isFilesystemRoot, true, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return true, info.IsDir(), isFilesystemRoot, false, nil
}

func TargetExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("inspect target: %w", err)
}

func IsLinkOrReparse(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	return info.Mode()&os.ModeSymlink != 0 || isReparsePoint(info), nil
}

func InsideGitRepository(root string) (bool, error) {
	current, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	for {
		_, statErr := os.Lstat(filepath.Join(current, ".git"))
		if statErr == nil {
			return true, nil
		}
		if !errors.Is(statErr, os.ErrNotExist) {
			return false, fmt.Errorf("inspect Git boundary: %w", statErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false, nil
		}
		current = parent
	}
}

func JoinWithin(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", errors.New("safety abort: operation path must be relative")
	}
	joined := filepath.Clean(filepath.Join(root, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(root, joined)
	if err != nil {
		return "", fmt.Errorf("resolve operation path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("safety abort: operation escapes target")
	}
	return joined, nil
}
