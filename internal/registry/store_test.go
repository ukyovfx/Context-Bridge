package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func validRegistry(t *testing.T) core.Registry {
	t.Helper()
	root := filepath.Join(t.TempDir(), "repo")
	gitDir := filepath.Join(root, ".git")
	identity, err := core.NormalizeRemote("https://github.com/ukyovfx/Context-Bridge.git")
	if err != nil {
		t.Fatal(err)
	}
	return core.Registry{
		SchemaVersion: 1,
		Projects:      []core.ProjectRecord{{ID: "prj_123e4567-e89b-42d3-a456-426614174000", DisplayName: "Context Bridge"}},
		Repositories: []core.RepositoryRecord{{
			ID: "repo_123e4567-e89b-42d3-a456-426614174001", ProjectID: "prj_123e4567-e89b-42d3-a456-426614174000",
			Identity: identity, PrimaryRemoteName: "origin", CanonicalBranch: "main",
		}},
		Workspaces: []core.WorkspaceRecord{{
			ID: "ws_123e4567-e89b-42d3-a456-426614174002", RepositoryID: "repo_123e4567-e89b-42d3-a456-426614174001",
			Path: root, PathKey: workspace.PathKey(root), GitRoot: root, GitRootKey: workspace.PathKey(root),
			GitDir: gitDir, GitDirKey: workspace.PathKey(gitDir), GitCommonDir: gitDir, GitCommonDirKey: workspace.PathKey(gitDir),
			Topology: core.TopologyMainWorktree, Role: core.RoleCanonical,
		}},
	}
}

func TestStoreSaveLoadBackupAndLock(t *testing.T) {
	home := t.TempDir()
	store := Store{Home: home}
	first := validRegistry(t)
	if err := store.Save(first); err != nil {
		t.Fatal(err)
	}
	second := first
	second.Projects = append([]core.ProjectRecord(nil), first.Projects...)
	second.Projects[0].DisplayName = "Context Bridge Updated"
	if err := store.Save(second); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Projects[0].DisplayName != "Context Bridge Updated" {
		t.Fatalf("unexpected current registry: %#v", loaded.Projects[0])
	}
	backup, err := readRegistry(store.Path() + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if backup.Projects[0].DisplayName != "Context Bridge" {
		t.Fatalf("unexpected backup: %#v", backup.Projects[0])
	}
	if err := os.WriteFile(filepath.Join(home, ".registry.lock"), []byte("busy"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(second); err == nil {
		t.Fatal("save succeeded while registry lock existed")
	}
}

func TestStoreFallsBackToValidBackup(t *testing.T) {
	store := Store{Home: t.TempDir()}
	value := validRegistry(t)
	if err := store.Save(value); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path()+".bak", data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path(), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Projects[0].ID != value.Projects[0].ID {
		t.Fatal("backup registry was not loaded")
	}
}

func TestDefaultHomeHonorsOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "identity-home")
	t.Setenv("CONTEXTBRIDGE_HOME", override)
	home, err := DefaultHome()
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := filepath.Abs(override)
	if home != expected {
		t.Fatalf("home = %q, want %q", home, expected)
	}
}
