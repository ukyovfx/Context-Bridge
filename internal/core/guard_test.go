package core

import "testing"

func validGuardValues() (GuardExpected, GuardActual) {
	remote := RemoteIdentity{Host: "github.com", Path: "ukyovfx/Context-Bridge"}
	probe := WorkspaceProbe{PathKey: "c:/repo", GitRootKey: "c:/repo", GitDirKey: "c:/repo/.git", GitCommonDirKey: "c:/repo/.git", Branch: "main", PrimaryRemote: &remote, Fingerprint: "abc"}
	expected := GuardExpected{ProjectID: "p", RepositoryID: "r", WorkspaceID: "w", PathKey: probe.PathKey, GitRootKey: probe.GitRootKey, GitDirKey: probe.GitDirKey, GitCommonDirKey: probe.GitCommonDirKey, PrimaryRemote: remote, BranchPolicy: BranchPolicy{Branch: "main"}, PlannedFingerprint: "abc"}
	actual := GuardActual{ProjectFound: true, ProjectID: "p", RepositoryID: "r", WorkspaceID: "w", Probe: probe}
	return expected, actual
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
		{"branch", func(_ *GuardExpected, a *GuardActual) { a.Probe.Branch = "feature" }, ReasonBranchMismatch},
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

func containsReason(values []GuardReason, expected GuardReason) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
