package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/registry"
)

func TestNewDryRunUsesConfiguredActiveRootWithoutMutation(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workspaceRoot := filepath.Join(root, "workspace")
	for _, name := range []string{"active", "worktrees", "archive", "private"} {
		if err := os.MkdirAll(filepath.Join(workspaceRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	writeLocalProfile(t, home, map[string]any{"schema_version": 1, "workspace_root": workspaceRoot})
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"new", "NODE", "--dry-run"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("new dry-run failed: %s", stderr.String())
	}
	target := filepath.Join(workspaceRoot, "active", "NODE")
	if !strings.Contains(stdout.String(), target) {
		t.Fatalf("dry-run omitted exact target: %s", stdout.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("dry-run created target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "registry-v1.json")); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote registry: %v", err)
	}
}

func TestNewCreatesAndRegistersLocalProject(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workspaceRoot := filepath.Join(root, "workspace")
	for _, name := range []string{"active", "worktrees", "archive", "private"} {
		if err := os.MkdirAll(filepath.Join(workspaceRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	writeLocalProfile(t, home, map[string]any{"schema_version": 1, "workspace_root": workspaceRoot})
	t.Setenv("GIT_AUTHOR_NAME", "Context Bridge Tests")
	t.Setenv("GIT_AUTHOR_EMAIL", "contextbridge-tests@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Context Bridge Tests")
	t.Setenv("GIT_COMMITTER_EMAIL", "contextbridge-tests@example.invalid")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"new", "NODE"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("new failed: %s", stderr.String())
	}
	target := filepath.Join(workspaceRoot, "active", "NODE")
	if _, err := os.Stat(filepath.Join(target, ".contextbridge", "manifest.json")); err != nil {
		t.Fatal(err)
	}
	value, err := (registry.Store{Home: home}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Projects) != 1 || len(value.Repositories) != 1 || len(value.Workspaces) != 1 {
		t.Fatalf("unexpected registration: %#v", value)
	}
	if value.Workspaces[0].Path != target {
		t.Fatalf("registered path %q, want %q", value.Workspaces[0].Path, target)
	}
	if !strings.Contains(stdout.String(), "Codex Local Project") {
		t.Fatalf("success omitted Codex next action: %s", stdout.String())
	}
}

func TestNewRequiresValidProfileAndRefusesExistingTarget(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"new", "NODE"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "setup first") {
		t.Fatalf("missing profile was not refused: code=%d stderr=%s", code, stderr.String())
	}
	workspaceRoot := filepath.Join(root, "workspace")
	for _, name := range []string{"active", "worktrees", "archive", "private"} {
		if err := os.MkdirAll(filepath.Join(workspaceRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeLocalProfile(t, home, map[string]any{"schema_version": 1, "workspace_root": workspaceRoot})
	if err := os.Mkdir(filepath.Join(workspaceRoot, "active", "NODE"), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"new", "NODE"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "target already exists") {
		t.Fatalf("existing target was not refused: code=%d stderr=%s", code, stderr.String())
	}
}

func TestNewGitHubOptionKeepsRemoteCreationExplicit(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	workspaceRoot := filepath.Join(root, "workspace")
	for _, name := range []string{"active", "worktrees", "archive", "private"} {
		if err := os.MkdirAll(filepath.Join(workspaceRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	writeLocalProfile(t, home, map[string]any{"schema_version": 1, "workspace_root": workspaceRoot})
	oldIdentity, oldAbsent := initGitHubIdentity, initRepositoryAbsent
	initGitHubIdentity = func() (githubIdentity, error) { return githubIdentity{Login: "owner", Type: "User"}, nil }
	initRepositoryAbsent = func(string, string) error { return nil }
	defer func() { initGitHubIdentity, initRepositoryAbsent = oldIdentity, oldAbsent }()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"new", "NODE", "--github", "--owner", "owner", "--dry-run"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("GitHub new dry-run failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "create_private_github_repository_and_push") {
		t.Fatalf("GitHub option omitted remote operation: %s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, "active", "NODE")); !os.IsNotExist(err) {
		t.Fatalf("GitHub dry-run mutated target: %v", err)
	}
}
