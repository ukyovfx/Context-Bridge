package core

import (
	"encoding/json"
	"testing"
)

func TestParseManifestV1ReadCompatibility(t *testing.T) {
	value := Manifest{SchemaVersion: 1, Generator: Generator{Name: "contextbridge"}, Project: ManifestProject{Name: "old", Repository: "owner/old", Profile: "core"}, Lifecycle: Lifecycle{CreatedByContextBridge: true}}
	data, _ := json.Marshal(value)
	manifest, err := ParseManifest(data)
	if err != nil || manifest.SchemaVersion != 1 {
		t.Fatalf("Manifest V1 was not readable: %v", err)
	}
}

func TestParseManifestV2Identity(t *testing.T) {
	value := Manifest{SchemaVersion: 2, Generator: Generator{Name: "contextbridge"}, Project: ManifestProject{ID: "prj_11111111-1111-4111-8111-111111111111", Name: "new", Profile: "core"}, Repository: &ManifestRepository{ID: "repo_22222222-2222-4222-8222-222222222222", Identity: RemoteIdentity{Host: "github.com", Path: "owner/new"}, PrimaryRemoteName: "origin", CanonicalBranch: "main"}, Lifecycle: Lifecycle{CreatedByContextBridge: true}}
	data, _ := json.Marshal(value)
	if _, err := ParseManifest(data); err != nil {
		t.Fatalf("Manifest V2 rejected: %v", err)
	}
}
