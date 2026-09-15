package release

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateVersion(t *testing.T) {
	for _, version := range []string{"v1.2.0", "v1.2.0-rc.1"} {
		if err := ValidateVersion(version); err != nil {
			t.Fatalf("%s rejected: %v", version, err)
		}
	}
	for _, version := range []string{"1.2.0", "v1.2", "latest", "v1.2.0+build"} {
		if err := ValidateVersion(version); err == nil {
			t.Fatalf("%s accepted", version)
		}
	}
}

func TestManifestAcceptsInstallerArtifact(t *testing.T) {
	root := t.TempDir()
	windowsName, linuxName, _ := ArtifactNames("v1.2.0-test.1")
	installerName := "contextbridge-v1.2.0-test.1-windows-amd64-setup.exe"
	for name := range map[string]bool{windowsName: true, linuxName: true, installerName: true} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := BuildManifest("v1.2.0-test.1", "commit", "go1.23", []string{filepath.Join(root, windowsName), filepath.Join(root, linuxName), filepath.Join(root, installerName)})
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Artifacts) != 3 || manifest.Artifacts[2].Filename != installerName {
		t.Fatalf("installer missing: %#v", manifest)
	}
}

func TestManifestAndArtifactNames(t *testing.T) {
	root := t.TempDir()
	windowsName, linuxName, err := ArtifactNames("v1.2.0-test.1")
	if err != nil {
		t.Fatal(err)
	}
	windowsPath := filepath.Join(root, windowsName)
	linuxPath := filepath.Join(root, linuxName)
	if err := os.WriteFile(windowsPath, []byte("windows"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(linuxPath, []byte("linux"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildManifest("v1.2.0-test.1", "0123456789abcdef", "go1.23", []string{windowsPath, linuxPath})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Artifacts[0].OS != "windows" || manifest.Artifacts[1].Arch != "amd64" || manifest.Artifacts[0].SHA256 == "" || manifest.Artifacts[1].SHA256 == "" {
		t.Fatalf("incomplete manifest: %#v", manifest)
	}
	manifestPath := filepath.Join(root, "release-manifest.json")
	if err := WriteManifest(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(manifestPath); err != nil || len(data) == 0 {
		t.Fatalf("manifest was not written: %v", err)
	}
}
