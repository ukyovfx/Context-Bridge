package localprofile

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAbsentProfile(t *testing.T) {
	_, err := Load(t.TempDir())
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("Load error = %v, want ErrNotConfigured", err)
	}
}

func TestLoadValidProfileDerivesOnlyStandardChildren(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, "chosen-workspace")
	writeProfile(t, home, map[string]any{"schema_version": 1, "workspace_root": root})
	profile, err := Load(home)
	if err != nil {
		t.Fatal(err)
	}
	if profile.SchemaVersion != SchemaVersion || profile.WorkspaceRoot != root {
		t.Fatalf("unexpected profile: %#v", profile)
	}
	layout := profile.Layout()
	if layout.ActiveRoot != filepath.Join(root, "active") || layout.WorktreeRoot != filepath.Join(root, "worktrees") || layout.ArchiveRoot != filepath.Join(root, "archive") || layout.PrivateRoot != filepath.Join(root, "private") {
		t.Fatalf("unexpected derived layout: %#v", layout)
	}
}

func TestLoadRejectsInvalidProfiles(t *testing.T) {
	tests := []struct {
		name string
		data map[string]any
	}{
		{name: "unsupported schema", data: map[string]any{"schema_version": 2, "workspace_root": "C:\\workspace"}},
		{name: "unknown field", data: map[string]any{"schema_version": 1, "workspace_root": "C:\\workspace", "token": "never"}},
		{name: "relative path", data: map[string]any{"schema_version": 1, "workspace_root": "relative"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			writeProfile(t, home, test.data)
			if _, err := Load(home); err == nil {
				t.Fatal("invalid profile was accepted")
			}
		})
	}
}

func TestPlanExistingAndMissingDirectories(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace")
	plan, err := Plan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Create) != 5 {
		t.Fatalf("missing layout create list = %#v", plan.Create)
	}
	created, err := Create(plan)
	if err != nil || len(created) != 5 {
		t.Fatalf("Create = %v, %v", created, err)
	}
	plan, err = Plan(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Create) != 0 {
		t.Fatalf("existing layout still needs creation: %#v", plan.Create)
	}
}

func TestPlanRejectsFileAndGitBoundaryCollisions(t *testing.T) {
	root := t.TempDir()
	workspaceRoot := filepath.Join(root, "workspace")
	if err := os.Mkdir(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspaceRoot, "active"), []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Plan(workspaceRoot); err == nil {
		t.Fatal("file collision was accepted")
	}

	gitRoot := filepath.Join(root, "repo")
	if err := os.Mkdir(gitRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, gitRoot, "init")
	if _, err := Plan(filepath.Join(gitRoot, "workspace")); err == nil || !strings.Contains(err.Error(), "inside an existing Git worktree") {
		t.Fatalf("Git boundary was not rejected: %v", err)
	}
}

func TestPlanRejectsSymlinkCollisionWhenSupported(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "workspace")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink creation is unavailable on this host")
	}
	if _, err := Plan(link); err == nil || !strings.Contains(err.Error(), "symlink or reparse") {
		t.Fatalf("symlink collision was not rejected: %v", err)
	}
}

func TestPlanRejectsSymlinkInExistingParentWhenSupported(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	link := filepath.Join(root, "link")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlink creation is unavailable on this host")
	}
	if _, err := Plan(filepath.Join(link, "workspace")); err == nil || !strings.Contains(err.Error(), "symlink or reparse") {
		t.Fatalf("symlink parent was not rejected: %v", err)
	}
}

func TestPlanRejectsLayoutPathThatExceedsBound(t *testing.T) {
	root := filepath.Join(t.TempDir(), strings.Repeat("x", maxPathLength))
	if _, err := Plan(root); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Fatalf("overlong layout was not rejected: %v", err)
	}
}

func TestLayoutFindingsAndPlacementChecks(t *testing.T) {
	root := t.TempDir()
	profile := Profile{SchemaVersion: SchemaVersion, WorkspaceRoot: filepath.Join(root, "workspace")}
	if !hasCode(profile.LayoutFindings(), "WORKSPACE_ROOT_MISSING") {
		t.Fatal("missing workspace root was not diagnosed")
	}
	if err := os.Mkdir(profile.WorkspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if !hasCode(profile.LayoutFindings(), "ACTIVE_ROOT_MISSING") || !hasCode(profile.LayoutFindings(), "PRIVATE_ROOT_MISSING") {
		t.Fatal("missing derived roots were not diagnosed")
	}
	if _, outside := profile.CanonicalWorkspaceFinding(filepath.Join(profile.Layout().ActiveRoot, "repo")); outside {
		t.Fatal("canonical workspace under active root was diagnosed")
	}
	if finding, outside := profile.CanonicalWorkspaceFinding(filepath.Join(root, "other")); !outside || finding.Code != "CANONICAL_WORKSPACE_OUTSIDE_ACTIVE_ROOT" {
		t.Fatalf("unexpected canonical placement result: %#v, %v", finding, outside)
	}
	if finding, outside := profile.ManagedWorkspaceFinding(filepath.Join(root, "other")); !outside || finding.Code != "MANAGED_WORKSPACE_OUTSIDE_WORKTREE_ROOT" {
		t.Fatalf("unexpected managed placement result: %#v, %v", finding, outside)
	}
}

func TestSaveOnlyAfterSuccessfulLayout(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(t.TempDir(), "workspace")
	if _, err := Load(home); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("profile unexpectedly exists: %v", err)
	}
	plan, err := Plan(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Load(home); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("planning wrote profile: %v", err)
	}
	if _, err := Create(plan); err != nil {
		t.Fatal(err)
	}
	if err := Save(home, plan.Profile); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(home, Filename))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || len(raw) != 2 {
		t.Fatalf("unexpected persisted profile: %s", data)
	}
}

func writeProfile(t *testing.T, home string, value map[string]any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, Filename), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = directory
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, data)
	}
}

func hasCode(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
