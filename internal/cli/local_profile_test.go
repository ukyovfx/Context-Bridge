package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/codexbootstrap"
	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/localprofile"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func TestDoctorWithoutProfilePreservesOutput(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "contextbridge-home")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	project := initLocalProfileProject(t, root, "no-profile")

	first := runProjectDoctor(t, project)
	second := runProjectDoctor(t, project)
	if first != second {
		t.Fatalf("doctor output changed without a profile\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	if strings.Contains(first, "profile") {
		t.Fatalf("profile diagnostics appeared without a profile: %s", first)
	}
}

func TestDoctorReportsValidProfileWithoutAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "contextbridge-home")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	workspaceRoot := makeProfileDirectory(t, root, "workspace")
	makeProfileDirectory(t, workspaceRoot, "active")
	project := initLocalProfileProject(t, filepath.Join(workspaceRoot, "active"), "valid-profile")
	for _, name := range []string{"worktrees", "archive", "private"} {
		makeProfileDirectory(t, workspaceRoot, name)
	}
	writeLocalProfile(t, home, map[string]any{
		"schema_version": 1,
		"workspace_root": workspaceRoot,
	})

	output := runProjectDoctor(t, project)
	if !strings.Contains(output, "local workspace profile: valid") {
		t.Fatalf("valid profile was not reported: %s", output)
	}
	for _, path := range []string{workspaceRoot, filepath.Join(workspaceRoot, "active"), filepath.Join(workspaceRoot, "worktrees"), filepath.Join(workspaceRoot, "archive"), filepath.Join(workspaceRoot, "private")} {
		if strings.Contains(output, path) {
			t.Fatalf("doctor exposed configured path %q: %s", path, output)
		}
	}
}

func TestDoctorMalformedProfileIsAdvisory(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "contextbridge-home")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	project := initLocalProfileProject(t, root, "invalid-profile")
	writeLocalProfile(t, home, map[string]any{"schema_version": 2, "workspace_root": root})

	output := runProjectDoctor(t, project)
	if !strings.Contains(output, "profile finding: profile LOCAL_PROFILE_INVALID") {
		t.Fatalf("malformed profile was not reported: %s", output)
	}
}

func TestDoctorReportsPrivateRootInsideGitWorktree(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "contextbridge-home")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	project := initLocalProfileProject(t, root, "private-profile")
	workspaceRoot := makeProfileDirectory(t, root, "workspace")
	for _, name := range []string{"active", "worktrees", "archive"} {
		makeProfileDirectory(t, workspaceRoot, name)
	}
	private := makeProfileDirectory(t, workspaceRoot, "private")
	runTestGit(t, private, "init")
	writeLocalProfile(t, home, map[string]any{
		"schema_version": 1,
		"workspace_root": workspaceRoot,
	})

	output := runProjectDoctor(t, project)
	if !strings.Contains(output, "profile finding: private_root PRIVATE_ROOT_INSIDE_GIT_WORKTREE") {
		t.Fatalf("private-root Git boundary was not reported: %s", output)
	}
}

func TestDoctorReportsCanonicalWorkspaceOutsideActiveRoot(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "contextbridge-home")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	project := initLocalProfileProject(t, root, "canonical-profile")
	remote, err := core.NormalizeRemote("https://github.com/example/canonical-profile.git")
	if err != nil {
		t.Fatal(err)
	}
	runTestGit(t, project, "remote", "add", "origin", "https://github.com/example/canonical-profile.git")
	manifest := readProfileManifest(t, project)
	manifest.Lifecycle.LocalOnly = false
	manifest.Repository.Identity = remote
	manifest.Repository.PrimaryRemoteName = "origin"
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".contextbridge", "manifest.json"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	probe, err := (workspace.Prober{}).Probe(project)
	if err != nil {
		t.Fatal(err)
	}
	value := core.NewRegistry()
	value.Projects = append(value.Projects, core.ProjectRecord{ID: manifest.Project.ID, DisplayName: manifest.Project.Name})
	value.Repositories = append(value.Repositories, core.RepositoryRecord{ID: manifest.Repository.ID, ProjectID: manifest.Project.ID, Identity: remote, PrimaryRemoteName: "origin", CanonicalBranch: "main"})
	value.Workspaces = append(value.Workspaces, workspaceRecord("ws_123e4567-e89b-42d3-a456-426614174000", manifest.Repository.ID, core.RoleCanonical, probe))
	if err := (registry.Store{Home: home}).Save(value); err != nil {
		t.Fatal(err)
	}
	workspaceRoot := makeProfileDirectory(t, root, "workspace")
	for _, name := range []string{"active", "worktrees", "archive", "private"} {
		makeProfileDirectory(t, workspaceRoot, name)
	}
	managedPath := makeProfileDirectory(t, root, "managed-profile")
	runTestGit(t, managedPath, "init")
	managedProbe, err := (workspace.Prober{}).Probe(managedPath)
	if err != nil {
		t.Fatal(err)
	}
	value.Workspaces = append(value.Workspaces, workspaceRecord("ws_223e4567-e89b-42d3-a456-426614174000", manifest.Repository.ID, core.RoleManagedWorkspace, managedProbe))
	if err := (registry.Store{Home: home}).Save(value); err != nil {
		t.Fatal(err)
	}
	writeLocalProfile(t, home, map[string]any{"schema_version": 1, "workspace_root": workspaceRoot})

	output := runProjectDoctor(t, project)
	if !strings.Contains(output, "profile finding: active_root CANONICAL_WORKSPACE_OUTSIDE_ACTIVE_ROOT") {
		t.Fatalf("canonical placement was not reported: %s", output)
	}
	if !strings.Contains(output, "profile finding: worktree_root MANAGED_WORKSPACE_OUTSIDE_WORKTREE_ROOT") {
		t.Fatalf("managed placement was not reported: %s", output)
	}
}

func TestSetupDryRunAndDeclineDoNotWrite(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "contextbridge-home")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	workspaceRoot := filepath.Join(root, "chosen")
	oldIdentity := setupGitHubIdentity
	setupGitHubIdentity = func() (githubIdentity, error) { return githubIdentity{Login: "test-user", Type: "User"}, nil }
	defer func() { setupGitHubIdentity = oldIdentity }()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"setup", "--workspace-root", workspaceRoot, "--dry-run"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("setup dry-run failed: %s", stderr.String())
	}
	if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
		t.Fatalf("setup dry-run created workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, localprofile.Filename)); !os.IsNotExist(err) {
		t.Fatalf("setup dry-run created profile: %v", err)
	}
	oldInput := setupInput
	defer func() { setupInput = oldInput }()
	setupInput = strings.NewReader("n\n")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"setup", "--workspace-root", workspaceRoot}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("declined setup unexpectedly succeeded")
	}
	if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
		t.Fatalf("declined setup created workspace: %v", err)
	}
}

func TestSetupCodexBootstrapPreviewAndApply(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "contextbridge-home")
	codexHome := filepath.Join(root, "codex-home")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	t.Setenv("CODEX_HOME", codexHome)
	oldIdentity := setupGitHubIdentity
	setupGitHubIdentity = func() (githubIdentity, error) { return githubIdentity{Login: "test-user", Type: "User"}, nil }
	defer func() { setupGitHubIdentity = oldIdentity }()
	workspaceRoot := filepath.Join(root, "workspace")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"setup", "--workspace-root", workspaceRoot, "--codex-bootstrap", "--dry-run"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("setup bootstrap dry-run failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(codexHome, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("Codex dry-run wrote global instructions: %v", err)
	}
	if !strings.Contains(stdout.String(), codexbootstrap.BeginMarker) {
		t.Fatalf("setup preview omitted managed block: %s", stdout.String())
	}
	stdout.Reset()
	if code := Run([]string{"setup", "--workspace-root", workspaceRoot, "--codex-bootstrap", "--confirm"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("setup bootstrap apply failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(codexHome, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	before := directorySnapshot(t, root)
	stdout.Reset()
	if code := Run([]string{"setup", "--codex-bootstrap", "--confirm"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("idempotent setup failed: %s", stderr.String())
	}
	if after := directorySnapshot(t, root); before != after {
		t.Fatal("idempotent setup mutated machine state")
	}
}

func initLocalProfileProject(t *testing.T, root, name string) string {
	t.Helper()
	t.Setenv("GIT_AUTHOR_NAME", "Context Bridge Tests")
	t.Setenv("GIT_AUTHOR_EMAIL", "contextbridge-tests@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Context Bridge Tests")
	t.Setenv("GIT_COMMITTER_EMAIL", "contextbridge-tests@example.invalid")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"init", name, "--root", root, "--local-only"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("init failed: %s", stderr.String())
	}
	return filepath.Join(root, name)
}

func runProjectDoctor(t *testing.T, project string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"doctor", "--project", project}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("doctor failed: %s", stderr.String())
	}
	return stdout.String()
}

func writeLocalProfile(t *testing.T, home string, value map[string]any) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, localprofile.Filename), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func makeProfileDirectory(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readProfileManifest(t *testing.T, project string) core.Manifest {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(project, ".contextbridge", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := core.ParseManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}
