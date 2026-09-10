package core

type WorkspaceTopology string

const (
	TopologyMainWorktree     WorkspaceTopology = "MAIN_WORKTREE"
	TopologyLinkedWorktree   WorkspaceTopology = "LINKED_WORKTREE"
	TopologyIndependentClone WorkspaceTopology = "INDEPENDENT_CLONE"
	TopologyNonGit           WorkspaceTopology = "NON_GIT"
	TopologyUnknown          WorkspaceTopology = "UNKNOWN"
)

type RegistryRole string

const (
	RoleCanonical           RegistryRole = "CANONICAL"
	RoleManagedWorkspace    RegistryRole = "MANAGED_WORKSPACE"
	RoleRegisteredAlternate RegistryRole = "REGISTERED_ALTERNATE"
	RoleUnregistered        RegistryRole = "UNREGISTERED"
)

type WorkspaceState string

const (
	StateClean              WorkspaceState = "CLEAN"
	StateDirty              WorkspaceState = "DIRTY"
	StateHasLocalUniqueWork WorkspaceState = "HAS_LOCAL_UNIQUE_WORK"
	StateStaleEvidence      WorkspaceState = "STALE_EVIDENCE"
	StateReviewRequired     WorkspaceState = "REVIEW_REQUIRED"
	StateCleanupCandidate   WorkspaceState = "CLEANUP_CANDIDATE"
)

type RemoteObservation struct {
	Name       string           `json:"name"`
	Identities []RemoteIdentity `json:"identities,omitempty"`
	Errors     []string         `json:"errors,omitempty"`
}

type RefObservation struct {
	Name string `json:"name"`
	OID  string `json:"oid"`
}

type WorkspaceProbe struct {
	CanonicalPath     string              `json:"canonical_path"`
	PathKey           string              `json:"path_key"`
	GitRoot           string              `json:"git_root,omitempty"`
	GitRootKey        string              `json:"git_root_key,omitempty"`
	GitDir            string              `json:"git_dir,omitempty"`
	GitDirKey         string              `json:"git_dir_key,omitempty"`
	GitCommonDir      string              `json:"git_common_dir,omitempty"`
	GitCommonDirKey   string              `json:"git_common_dir_key,omitempty"`
	Head              string              `json:"head,omitempty"`
	Branch            string              `json:"branch,omitempty"`
	Detached          bool                `json:"detached"`
	PrimaryRemoteName string              `json:"primary_remote_name"`
	PrimaryRemote     *RemoteIdentity     `json:"primary_remote,omitempty"`
	Remotes           []RemoteObservation `json:"remotes,omitempty"`
	Topology          WorkspaceTopology   `json:"topology"`
	RelatedWorktrees  []string            `json:"related_worktrees,omitempty"`
	PorcelainV2       string              `json:"porcelain_v2,omitempty"`
	Staged            bool                `json:"staged"`
	Unstaged          bool                `json:"unstaged"`
	Untracked         bool                `json:"untracked"`
	LocalUniqueCount  int                 `json:"local_unique_count"`
	Refs              []RefObservation    `json:"refs,omitempty"`
	EvidenceErrors    []string            `json:"evidence_errors,omitempty"`
	Fingerprint       string              `json:"fingerprint"`
}

func (p WorkspaceProbe) States() []WorkspaceState {
	states := []WorkspaceState{StateClean}
	if p.Staged || p.Unstaged || p.Untracked {
		states[0] = StateDirty
	}
	if p.LocalUniqueCount > 0 {
		states = append(states, StateHasLocalUniqueWork)
	}
	if len(p.EvidenceErrors) > 0 {
		states = append(states, StateStaleEvidence, StateReviewRequired)
	}
	return states
}
