package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$`)

type Artifact struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	SHA256   string `json:"sha256"`
}

type Manifest struct {
	Version   string     `json:"version"`
	GitCommit string     `json:"git_commit"`
	GoVersion string     `json:"go_version"`
	Artifacts []Artifact `json:"artifacts"`
}

func ValidateVersion(version string) error {
	if !versionPattern.MatchString(version) {
		return fmt.Errorf("invalid release version %q: expected vMAJOR.MINOR.PATCH[-prerelease]", version)
	}
	return nil
}

func ArtifactNames(version string) (string, string, error) {
	if err := ValidateVersion(version); err != nil {
		return "", "", err
	}
	return "contextbridge-" + version + "-windows-amd64.zip", "contextbridge-" + version + "-linux-amd64.tar.gz", nil
}

func SHA256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func BuildManifest(version, commit, goVersion string, files []string) (Manifest, error) {
	if err := ValidateVersion(version); err != nil {
		return Manifest{}, err
	}
	if commit == "" || goVersion == "" || (len(files) != 2 && len(files) != 3) {
		return Manifest{}, errors.New("release manifest requires version, commit, Go version, and two or three artifacts")
	}
	windowsName, linuxName, err := ArtifactNames(version)
	if err != nil {
		return Manifest{}, err
	}
	windowsHash, err := SHA256File(files[0])
	if err != nil {
		return Manifest{}, err
	}
	linuxHash, err := SHA256File(files[1])
	if err != nil {
		return Manifest{}, err
	}
	if filepath.Base(files[0]) != windowsName || filepath.Base(files[1]) != linuxName {
		return Manifest{}, errors.New("release artifact names do not match the requested version")
	}
	artifacts := []Artifact{
		{Filename: windowsName, OS: "windows", Arch: "amd64", SHA256: windowsHash},
		{Filename: linuxName, OS: "linux", Arch: "amd64", SHA256: linuxHash},
	}
	if len(files) == 3 {
		installerName := "contextbridge-" + version + "-windows-amd64-setup.exe"
		if filepath.Base(files[2]) != installerName {
			return Manifest{}, errors.New("installer name does not match the requested version")
		}
		installerHash, err := SHA256File(files[2])
		if err != nil {
			return Manifest{}, err
		}
		artifacts = append(artifacts, Artifact{Filename: installerName, OS: "windows", Arch: "amd64", SHA256: installerHash})
	}
	return Manifest{Version: version, GitCommit: commit, GoVersion: goVersion, Artifacts: artifacts}, nil
}

func WriteManifest(path string, manifest Manifest) error {
	if manifest.Version == "" || manifest.GitCommit == "" || manifest.GoVersion == "" || (len(manifest.Artifacts) != 2 && len(manifest.Artifacts) != 3) {
		return errors.New("refusing to write incomplete release manifest")
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
