package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/apply"
	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func TestIdentityChangeBetweenPlanAndApplyPreventsMutation(t *testing.T) {
	repositoryPath := filepath.Join(t.TempDir(), "registered")
	if err := os.Mkdir(repositoryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repositoryPath, "init", "-b", "main")
	runTestGit(t, repositoryPath, "remote", "add", "origin", "https://github.com/ukyovfx/registered.git")
	if err := os.WriteFile(filepath.Join(repositoryPath, "content.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repositoryPath, "add", "content.txt")
	runTestGit(t, repositoryPath, "commit", "-m", "registered")
	probe, err := (workspace.Prober{}).Probe(repositoryPath)
	if err != nil {
		t.Fatal(err)
	}
	project := core.ProjectRecord{ID: "prj_11111111-1111-4111-8111-111111111111", DisplayName: "registered"}
	repository := core.RepositoryRecord{
		ID: "repo_22222222-2222-4222-8222-222222222222", ProjectID: project.ID,
		Identity: *probe.PrimaryRemote, PrimaryRemoteName: "origin", CanonicalBranch: "main",
	}
	registered := workspaceRecord("ws_33333333-3333-4333-8333-333333333333", repository.ID, core.RoleCanonical, probe)
	expected := guardExpected(project, repository, registered, probe.Fingerprint)
	target := filepath.Join(t.TempDir(), "must-not-exist")
	plan := core.NewGuardedPlan(target, []core.Operation{{Kind: core.CreateDirectory, Path: "."}}, expected)
	runTestGit(t, repositoryPath, "remote", "set-url", "origin", "https://evil.example/ukyovfx/registered.git")
	err = (apply.Applier{
		Runner:        apply.CommandRunner{},
		GuardVerifier: liveGuardVerifier{Project: project, Repository: repository, Workspace: registered, Path: repositoryPath},
	}).Apply(plan)
	if !core.IsWrongWorkspace(err) {
		t.Fatalf("expected WRONG_WORKSPACE after identity change, got %v", err)
	}
	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("apply mutated target after identity change: %v", statErr)
	}
}
