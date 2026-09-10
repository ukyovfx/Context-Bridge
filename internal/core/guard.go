package core

import (
	"errors"
	"strings"
)

const WrongWorkspace = "WRONG_WORKSPACE"

type GuardReason string

const (
	ReasonProjectNotFound          GuardReason = "PROJECT_NOT_FOUND"
	ReasonRepositoryMismatch       GuardReason = "REPOSITORY_MISMATCH"
	ReasonWorkspacePathMismatch    GuardReason = "WORKSPACE_PATH_MISMATCH"
	ReasonGitRootMismatch          GuardReason = "GIT_ROOT_MISMATCH"
	ReasonGitDirMismatch           GuardReason = "GIT_DIR_MISMATCH"
	ReasonGitCommonDirMismatch     GuardReason = "GIT_COMMON_DIR_MISMATCH"
	ReasonPrimaryRemoteMismatch    GuardReason = "PRIMARY_REMOTE_MISMATCH"
	ReasonBranchMismatch           GuardReason = "BRANCH_MISMATCH"
	ReasonIdentityChangedAfterPlan GuardReason = "IDENTITY_CHANGED_AFTER_PLAN"
)

type BranchPolicy struct {
	Branch        string `json:"branch,omitempty"`
	AllowDetached bool   `json:"allow_detached"`
}

type GuardExpected struct {
	ProjectID          string         `json:"project_id"`
	RepositoryID       string         `json:"repository_id"`
	WorkspaceID        string         `json:"workspace_id"`
	PathKey            string         `json:"path_key"`
	GitRootKey         string         `json:"git_root_key"`
	GitDirKey          string         `json:"git_dir_key"`
	GitCommonDirKey    string         `json:"git_common_dir_key"`
	PrimaryRemote      RemoteIdentity `json:"primary_remote"`
	BranchPolicy       BranchPolicy   `json:"branch_policy"`
	PlannedFingerprint string         `json:"planned_fingerprint,omitempty"`
}

type GuardActual struct {
	ProjectFound bool
	ProjectID    string
	RepositoryID string
	WorkspaceID  string
	Probe        WorkspaceProbe
}

type GuardDecision struct {
	Allowed    bool          `json:"allowed"`
	PublicCode string        `json:"public_code,omitempty"`
	Reasons    []GuardReason `json:"reasons,omitempty"`
}

func EvaluateGuard(expected GuardExpected, actual GuardActual) GuardDecision {
	reasons := make([]GuardReason, 0)
	if !actual.ProjectFound || expected.ProjectID == "" || actual.ProjectID != expected.ProjectID {
		reasons = append(reasons, ReasonProjectNotFound)
	}
	if expected.RepositoryID == "" || actual.RepositoryID != expected.RepositoryID {
		reasons = append(reasons, ReasonRepositoryMismatch)
	}
	if expected.WorkspaceID == "" || actual.WorkspaceID != expected.WorkspaceID || expected.PathKey == "" || actual.Probe.PathKey != expected.PathKey {
		reasons = append(reasons, ReasonWorkspacePathMismatch)
	}
	if expected.GitRootKey == "" || actual.Probe.GitRootKey != expected.GitRootKey {
		reasons = append(reasons, ReasonGitRootMismatch)
	}
	if expected.GitDirKey == "" || actual.Probe.GitDirKey != expected.GitDirKey {
		reasons = append(reasons, ReasonGitDirMismatch)
	}
	if expected.GitCommonDirKey == "" || actual.Probe.GitCommonDirKey != expected.GitCommonDirKey {
		reasons = append(reasons, ReasonGitCommonDirMismatch)
	}
	if actual.Probe.PrimaryRemote == nil || !expected.PrimaryRemote.Equal(*actual.Probe.PrimaryRemote) {
		reasons = append(reasons, ReasonPrimaryRemoteMismatch)
	}
	if actual.Probe.Detached {
		if !expected.BranchPolicy.AllowDetached {
			reasons = append(reasons, ReasonBranchMismatch)
		}
	} else if expected.BranchPolicy.Branch == "" || actual.Probe.Branch != expected.BranchPolicy.Branch {
		reasons = append(reasons, ReasonBranchMismatch)
	}
	if expected.PlannedFingerprint != "" && !strings.EqualFold(expected.PlannedFingerprint, actual.Probe.Fingerprint) {
		reasons = append(reasons, ReasonIdentityChangedAfterPlan)
	}
	if len(reasons) > 0 {
		return GuardDecision{Allowed: false, PublicCode: WrongWorkspace, Reasons: deduplicateReasons(reasons)}
	}
	return GuardDecision{Allowed: true}
}

func deduplicateReasons(in []GuardReason) []GuardReason {
	seen := map[GuardReason]bool{}
	out := make([]GuardReason, 0, len(in))
	for _, reason := range in {
		if !seen[reason] {
			seen[reason] = true
			out = append(out, reason)
		}
	}
	return out
}

type WrongWorkspaceError struct {
	Reasons []GuardReason
}

func (e WrongWorkspaceError) Error() string { return WrongWorkspace }

func IsWrongWorkspace(err error) bool {
	var target WrongWorkspaceError
	return errors.As(err, &target)
}
