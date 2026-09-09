package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"
)

func validRequest(root string) InitRequest {
	return InitRequest{Name: "demo", Root: root, Owner: "ukyovfx", Profile: "openai", GeneratorVersion: "test"}
}

func validSnapshot() InitSnapshot {
	return InitSnapshot{RootExists: true, RootIsDirectory: true}
}

func TestBuildInitPlanIsDeterministicAndImmutable(t *testing.T) {
	req := validRequest(t.TempDir())
	first, err := BuildInitPlan(req, validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildInitPlan(req, validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := first.JSON()
	secondJSON, _ := second.JSON()
	if !reflect.DeepEqual(firstJSON, secondJSON) {
		t.Fatal("identical inputs produced different plans")
	}
	ops := first.Operations()
	ops[0].Path = "changed"
	ops[0].Args = []string{"changed"}
	if first.Operations()[0].Path == "changed" {
		t.Fatal("operation accessor exposed mutable plan state")
	}
	if first.Target() != filepath.Join(req.Root, req.Name) {
		t.Fatalf("unexpected target %q", first.Target())
	}
}

func TestManifestHashesEveryGeneratedFile(t *testing.T) {
	plan, err := BuildInitPlan(validRequest(t.TempDir()), validSnapshot())
	if err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{}
	var manifestText string
	for _, op := range plan.Operations() {
		if op.Kind != WriteFile {
			continue
		}
		if op.Path == ".contextbridge/manifest.json" {
			manifestText = op.Content
		} else {
			contents[op.Path] = op.Content
		}
	}
	var manifest Manifest
	if err := json.Unmarshal([]byte(manifestText), &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != len(contents) {
		t.Fatalf("manifest has %d hashes for %d generated files", len(manifest.Files), len(contents))
	}
	for _, item := range manifest.Files {
		sum := sha256.Sum256([]byte(contents[item.Path]))
		if item.SHA256 != hex.EncodeToString(sum[:]) {
			t.Fatalf("incorrect hash for %s", item.Path)
		}
	}
}

func TestNonOverridableSafetyAborts(t *testing.T) {
	cases := []struct {
		name     string
		req      InitRequest
		snapshot InitSnapshot
	}{
		{"existing target", validRequest(t.TempDir()), InitSnapshot{RootExists: true, RootIsDirectory: true, TargetExists: true}},
		{"nested repository", validRequest(t.TempDir()), InitSnapshot{RootExists: true, RootIsDirectory: true, InsideGitRepository: true}},
		{"filesystem root", validRequest(t.TempDir()), InitSnapshot{RootExists: true, RootIsDirectory: true, RootIsFilesystemRoot: true}},
		{"reparse point", validRequest(t.TempDir()), InitSnapshot{RootExists: true, RootIsDirectory: true, RootHasReparsePoint: true}},
		{"Windows reserved name", InitRequest{Name: "CON", Root: t.TempDir(), Owner: "ukyovfx", Profile: "core"}, validSnapshot()},
		{"missing owner", InitRequest{Name: "demo", Root: t.TempDir(), Profile: "core"}, validSnapshot()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := BuildInitPlan(tc.req, tc.snapshot); err == nil {
				t.Fatal("expected safety abort")
			}
		})
	}
}
