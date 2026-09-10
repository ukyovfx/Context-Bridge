package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ukyovfx/Context-Bridge/internal/core"
)

const filename = "registry-v1.json"

type Store struct {
	Home string
}

func DefaultHome() (string, error) {
	if override := os.Getenv("CONTEXTBRIDGE_HOME"); override != "" {
		return filepath.Abs(override)
	}
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		return filepath.Join(local, "ContextBridge"), nil
	}
	config, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve Context Bridge home: %w", err)
	}
	return filepath.Join(config, "ContextBridge"), nil
}

func (s Store) Path() string { return filepath.Join(s.Home, filename) }

func (s Store) Load() (core.Registry, error) {
	data, err := os.ReadFile(s.Path())
	if errors.Is(err, os.ErrNotExist) {
		backup, backupErr := readRegistry(s.Path() + ".bak")
		if backupErr == nil {
			return backup, nil
		}
		return core.NewRegistry(), nil
	}
	if err == nil {
		if registry, parseErr := parseRegistry(data); parseErr == nil {
			return registry, nil
		}
	}
	backup, backupErr := readRegistry(s.Path() + ".bak")
	if backupErr == nil {
		return backup, nil
	}
	if err != nil {
		return core.Registry{}, fmt.Errorf("read registry: %w", err)
	}
	return core.Registry{}, errors.New("registry and backup are invalid")
}

func readRegistry(path string) (core.Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return core.Registry{}, err
	}
	return parseRegistry(data)
}

func parseRegistry(data []byte) (core.Registry, error) {
	var registry core.Registry
	if err := json.Unmarshal(data, &registry); err != nil {
		return core.Registry{}, fmt.Errorf("parse registry: %w", err)
	}
	if err := registry.Validate(); err != nil {
		return core.Registry{}, fmt.Errorf("validate registry: %w", err)
	}
	return registry, nil
}

func (s Store) Save(registry core.Registry) error {
	registry = registry.Sorted()
	if err := registry.Validate(); err != nil {
		return fmt.Errorf("refuse invalid registry write: %w", err)
	}
	data, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return fmt.Errorf("encode registry: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(s.Home, 0o700); err != nil {
		return fmt.Errorf("create registry home: %w", err)
	}
	lockPath := filepath.Join(s.Home, ".registry.lock")
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("registry is locked by another writer")
	}
	if _, err := fmt.Fprintf(lock, "%d\n", os.Getpid()); err != nil {
		lock.Close()
		os.Remove(lockPath)
		return fmt.Errorf("write registry lock: %w", err)
	}
	if err := lock.Close(); err != nil {
		os.Remove(lockPath)
		return err
	}
	defer os.Remove(lockPath)

	path := s.Path()
	if current, err := os.ReadFile(path); err == nil {
		if _, err := parseRegistry(current); err != nil {
			return errors.New("refuse replacement because current registry is invalid")
		}
		if err := writeReplacement(path+".bak", current); err != nil {
			return fmt.Errorf("write registry backup: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read current registry: %w", err)
	}
	if err := writeReplacement(path, data); err != nil {
		return fmt.Errorf("replace registry: %w", err)
	}
	return nil
}

func writeReplacement(destination string, data []byte) error {
	directory := filepath.Dir(destination)
	temp, err := os.CreateTemp(directory, ".contextbridge-registry-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return replaceFile(tempPath, destination)
}
