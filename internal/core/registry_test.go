package core

import (
	"path/filepath"
	"testing"
)

func validRegistryValue(t *testing.T) Registry {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	gitDir := filepath.Join(root, ".git")
	return Registry{
		SchemaVersion: 1,
		Projects:      []ProjectRecord{{ID: "prj_11111111-1111-4111-8111-111111111111", DisplayName: "one"}},
		Repositories: []RepositoryRecord{{
			ID: "repo_22222222-2222-4222-8222-222222222222", ProjectID: "prj_11111111-1111-4111-8111-111111111111",
			Identity: RemoteIdentity{Host: "github.com", Path: "ukyovfx/one"}, PrimaryRemoteName: "origin", CanonicalBranch: "main",
		}},
		Workspaces: []WorkspaceRecord{{
			ID: "ws_33333333-3333-4333-8333-333333333333", RepositoryID: "repo_22222222-2222-4222-8222-222222222222",
			Path: root, PathKey: registryPathKey(root), GitRoot: root, GitRootKey: registryPathKey(root), GitDir: gitDir, GitDirKey: registryPathKey(gitDir),
			GitCommonDir: gitDir, GitCommonDirKey: registryPathKey(gitDir), Topology: TopologyMainWorktree, Role: RoleCanonical,
		}},
	}
}

func TestRegistryValidationRejectsAmbiguousOrIncompleteIdentity(t *testing.T) {
	base := validRegistryValue(t)
	if err := base.Validate(); err != nil {
		t.Fatalf("valid registry rejected: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Registry)
	}{
		{"invalid topology", func(r *Registry) { r.Workspaces[0].Topology = TopologyUnknown }},
		{"relative git root", func(r *Registry) { r.Workspaces[0].GitRoot = "relative" }},
		{"duplicate canonical", func(r *Registry) {
			copy := r.Workspaces[0]
			copy.ID = "ws_44444444-4444-4444-8444-444444444444"
			copy.Path = filepath.Join(t.TempDir(), "other")
			copy.PathKey = registryPathKey(copy.Path)
			r.Workspaces = append(r.Workspaces, copy)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := base
			value.Projects = append([]ProjectRecord(nil), base.Projects...)
			value.Repositories = append([]RepositoryRecord(nil), base.Repositories...)
			value.Workspaces = append([]WorkspaceRecord(nil), base.Workspaces...)
			test.mutate(&value)
			if err := value.Validate(); err == nil {
				t.Fatal("invalid registry was accepted")
			}
		})
	}
}
