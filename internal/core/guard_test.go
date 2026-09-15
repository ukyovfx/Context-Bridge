package core

import (
	"runtime"
	"testing"
)

func validGuardValues() (GuardExpected, GuardActual) {
	remote := RemoteIdentity{Host: "github.com", Path: "ukyovfx/Context-Bridge"}
	physical := testPhysicalIdentity()
	probe := WorkspaceProbe{PathKey: "c:/repo", GitRootKey: "c:/repo", GitDirKey: "c:/repo/.git", GitCommonDirKey: "c:/repo/.git", Branch: "main", PrimaryRemote: &remote, Fingerprint: "abc", PhysicalIdentity: physical}
	expected := GuardExpected{ProjectID: "p", RepositoryID: "r", WorkspaceID: "w", PathKey: probe.PathKey, GitRootKey: probe.GitRootKey, GitDirKey: probe.GitDirKey, GitCommonDirKey: probe.GitCommonDirKey, PhysicalIdentity: physical, PrimaryRemote: remote, BranchPolicy: BranchPolicy{Branch: "main"}, PlannedFingerprint: "abc"}
	actual := GuardActual{ProjectFound: true, ProjectID: "p", RepositoryID: "r", WorkspaceID: "w", Probe: probe}
	return expected, actual
}

func testPhysicalIdentity() WorkspaceFilesystemIdentity {
	workspace := FilesystemIdentity{VolumeSerialNumber: 1, FileID: "01010101010101010101010101010101"}
	root := FilesystemIdentity{VolumeSerialNumber: 1, FileID: "02020202020202020202020202020202"}
	gitDir := FilesystemIdentity{VolumeSerialNumber: 1, FileID: "03030303030303030303030303030303"}
	common := FilesystemIdentity{VolumeSerialNumber: 1, FileID: "04040404040404040404040404040404"}
	return WorkspaceFilesystemIdentity{WorkspaceRoot: &workspace, GitRoot: &root, GitDir: &gitDir, GitCommonDir: &common}
}

func TestEvaluateGuardAllowsExactIdentity(t *testing.T) {
	expected, actual := validGuardValues()
	if decision := EvaluateGuard(expected, actual); !decision.Allowed || decision.PublicCode != "" {
		t.Fatalf("exact identity was rejected: %#v", decision)
	}
}

func TestEvaluateGuardFailsClosedForEveryIdentityDimension(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*GuardExpected, *GuardActual)
		reason GuardReason
	}{
		{"project", func(_ *GuardExpected, a *GuardActual) { a.ProjectFound = false }, ReasonProjectNotFound},
		{"repository", func(_ *GuardExpected, a *GuardActual) { a.RepositoryID = "other" }, ReasonRepositoryMismatch},
		{"path", func(_ *GuardExpected, a *GuardActual) { a.Probe.PathKey = "c:/other" }, ReasonWorkspacePathMismatch},
		{"root", func(_ *GuardExpected, a *GuardActual) { a.Probe.GitRootKey = "c:/other" }, ReasonGitRootMismatch},
		{"git dir", func(_ *GuardExpected, a *GuardActual) { a.Probe.GitDirKey = "other" }, ReasonGitDirMismatch},
		{"common dir", func(_ *GuardExpected, a *GuardActual) { a.Probe.GitCommonDirKey = "other" }, ReasonGitCommonDirMismatch},
		{"remote", func(_ *GuardExpected, a *GuardActual) {
			a.Probe.PrimaryRemote = &RemoteIdentity{Host: "evil.example", Path: "ukyovfx/Context-Bridge"}
		}, ReasonPrimaryRemoteMismatch},
		{"fingerprint", func(_ *GuardExpected, a *GuardActual) { a.Probe.Fingerprint = "changed" }, ReasonIdentityChangedAfterPlan},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expected, actual := validGuardValues()
			test.mutate(&expected, &actual)
			decision := EvaluateGuard(expected, actual)
			if decision.Allowed || decision.PublicCode != WrongWorkspace || !containsReason(decision.Reasons, test.reason) {
				t.Fatalf("unexpected guard decision: %#v", decision)
			}
		})
	}
}

func TestEvaluateGuardAllowsMutableGitState(t *testing.T) {
	expected, actual := validGuardValues()
	actual.Probe.Branch = "archive/kitsusync-clean/codex/deploy-rollback-transaction"
	if decision := EvaluateGuard(expected, actual); !decision.Allowed {
		t.Fatalf("legitimate branch change was treated as workspace identity drift: %#v", decision)
	}
	actual.Probe.Branch = ""
	actual.Probe.Detached = true
	if decision := EvaluateGuard(expected, actual); !decision.Allowed {
		t.Fatalf("detached Git state was treated as workspace identity drift: %#v", decision)
	}
}

func TestEvaluateGuardFailsClosedForIncompleteProbeEvidence(t *testing.T) {
	expected, actual := validGuardValues()
	actual.Probe.EvidenceErrors = []string{"status_unavailable"}
	decision := EvaluateGuard(expected, actual)
	if decision.Allowed || decision.PublicCode != WrongWorkspace || !containsReason(decision.Reasons, ReasonIncompleteProbeEvidence) {
		t.Fatalf("incomplete probe evidence was accepted: %#v", decision)
	}
}

func TestEvaluateGuardFailsClosedForFilesystemIdentityMismatch(t *testing.T) {
	expected, actual := validGuardValues()
	if actual.Probe.PhysicalIdentity.WorkspaceRoot == nil {
		t.Fatal("test identity missing")
	}
	changed := *actual.Probe.PhysicalIdentity.WorkspaceRoot
	changed.FileID = "ffffffffffffffffffffffffffffffff"
	actual.Probe.PhysicalIdentity.WorkspaceRoot = &changed
	decision := EvaluateGuard(expected, actual)
	if decision.Allowed && runtime.GOOS == "windows" {
		t.Fatal("filesystem identity mismatch was accepted on Windows")
	}
}

func containsReason(values []GuardReason, expected GuardReason) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
