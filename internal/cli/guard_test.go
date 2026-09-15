package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestRegistrationRejectsIncompleteProbeEvidence(t *testing.T) {
	remote := core.RemoteIdentity{Host: "github.com", Path: "example/repo"}
	err := validateRegistrationProbe(core.WorkspaceProbe{
		Topology: core.TopologyMainWorktree, PathKey: "c:/repo", GitRootKey: "c:/repo",
		Branch: "main", PrimaryRemote: &remote, EvidenceErrors: []string{"status_unavailable"},
	})
	if err == nil || !strings.Contains(err.Error(), "complete workspace probe evidence") {
		t.Fatalf("incomplete registration probe was accepted: %v", err)
	}
}

type incompleteProbeRunner struct{}

func (incompleteProbeRunner) Output(directory string, args ...string) ([]byte, error) {
	command := strings.Join(args, " ")
	switch {
	case command == "rev-parse --show-toplevel":
		return []byte(directory + "\n"), nil
	case command == "rev-parse --absolute-git-dir", command == "rev-parse --path-format=absolute --git-common-dir":
		return []byte(filepath.Join(directory, ".git") + "\n"), nil
	case command == "rev-parse --verify HEAD":
		return []byte("0123456789012345678901234567890123456789\n"), nil
	case command == "symbolic-ref --quiet --short HEAD":
		return []byte("main\n"), nil
	case command == "remote":
		return []byte("origin\n"), nil
	case strings.HasPrefix(command, "remote get-url"):
		return []byte("https://github.com/example/repo.git\n"), nil
	case strings.HasPrefix(command, "status"):
		return nil, errors.New("synthetic status failure")
	case strings.HasPrefix(command, "rev-list"), strings.HasPrefix(command, "stash"), strings.HasPrefix(command, "for-each-ref"), strings.HasPrefix(command, "worktree"):
		return []byte("0\n"), nil
	default:
		return nil, errors.New("unexpected probe command")
	}
}

func TestLiveGuardVerifierRejectsIncompleteProbeEvidence(t *testing.T) {
	path := t.TempDir()
	if err := os.Mkdir(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	remote := core.RemoteIdentity{Host: "github.com", Path: "example/repo"}
	prober := workspace.Prober{Runner: incompleteProbeRunner{}}
	probe, err := prober.Probe(path)
	if err != nil {
		t.Fatal(err)
	}
	project := core.ProjectRecord{ID: "prj_11111111-1111-4111-8111-111111111111", DisplayName: "repo"}
	repository := core.RepositoryRecord{ID: "repo_22222222-2222-4222-8222-222222222222", ProjectID: project.ID, Identity: remote, PrimaryRemoteName: "origin", CanonicalBranch: "main"}
	workspaceRecord := core.WorkspaceRecord{ID: "ws_33333333-3333-4333-8333-333333333333", RepositoryID: repository.ID, Path: probe.CanonicalPath, PathKey: probe.PathKey, GitRoot: probe.GitRoot, GitRootKey: probe.GitRootKey, GitDir: probe.GitDir, GitDirKey: probe.GitDirKey, GitCommonDir: probe.GitCommonDir, GitCommonDirKey: probe.GitCommonDirKey, Topology: probe.Topology, Role: core.RoleCanonical}
	verifier := liveGuardVerifier{Project: project, Repository: repository, Workspace: workspaceRecord, Path: path, Prober: prober}
	if err := verifier.Verify(guardExpected(project, repository, workspaceRecord, probe.Fingerprint)); !core.IsWrongWorkspace(err) {
		t.Fatalf("incomplete live probe was accepted: %v", err)
	}
}
