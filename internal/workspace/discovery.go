package workspace

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
)

const (
	defaultMaxDepth        = 8
	defaultMaxRepositories = 256
	defaultMaxEntries      = 100000
	defaultMaxDuration     = 15 * time.Second
)

type DiscoveryOptions struct {
	Root            string
	MaxDepth        int
	MaxRepositories int
	MaxEntries      int
	MaxDuration     time.Duration
	Registry        core.Registry
}

type DiscoveryStatus string

const (
	DiscoverySuccess             DiscoveryStatus = "success"
	DiscoverySuccessWithWarnings DiscoveryStatus = "success_with_warnings"
	DiscoveryPartial             DiscoveryStatus = "partial"
	DiscoveryNoCandidates        DiscoveryStatus = "no_candidates"
	DiscoveryFailed              DiscoveryStatus = "failed"
)

const (
	ReasonAccessDenied              = "ACCESS_DENIED"
	ReasonReparsePointSkipped       = "REPARSE_POINT_SKIPPED"
	ReasonMaxDepthReached           = "MAX_DEPTH_REACHED"
	ReasonEntryLimitReached         = "ENTRY_LIMIT_REACHED"
	ReasonProbeFailed               = "PROBE_FAILED"
	ReasonRootNotFound              = "ROOT_NOT_FOUND"
	ReasonTimeLimitReached          = "TIME_LIMIT_REACHED"
	ReasonRepositoryLimit           = "MAX_REPOSITORIES_REACHED"
	ReasonRegistryReadFailed        = "REGISTRY_READ_FAILED"
	ReasonProbeEvidence             = "PROBE_EVIDENCE_INCOMPLETE"
	ReasonGitRepositoryInaccessible = "GIT_REPOSITORY_INACCESSIBLE"
	ReasonGitProbeFailed            = "GIT_PROBE_FAILED"
)

type DiscoveryIssue struct {
	Path   string `json:"path,omitempty"`
	Reason string `json:"reason"`
}

type InventoryItem struct {
	Probe                 core.WorkspaceProbe   `json:"probe"`
	Role                  core.RegistryRole     `json:"role"`
	States                []core.WorkspaceState `json:"states"`
	ProjectID             string                `json:"project_id,omitempty"`
	RepositoryID          string                `json:"repository_id,omitempty"`
	WorkspaceID           string                `json:"workspace_id,omitempty"`
	RepositoryIdentityKey string                `json:"repository_identity_key,omitempty"`
	RelatedCloneCount     int                   `json:"related_clone_count"`
	SharedRefOIDCount     int                   `json:"shared_ref_oid_count"`
	RefOIDsOnlyHereCount  int                   `json:"ref_oids_only_here_count"`
	LastModified          time.Time             `json:"last_modified"`
}

type DiscoveryFailure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type ArtifactIndicator struct {
	Path      string `json:"path"`
	Indicator string `json:"indicator"`
}

type DiscoveryResult struct {
	RequestedRoot         string              `json:"requested_root"`
	Root                  string              `json:"root"`
	Status                DiscoveryStatus     `json:"status"`
	Items                 []InventoryItem     `json:"items"`
	CandidateCount        int                 `json:"candidate_count"`
	SkippedCount          int                 `json:"skipped_count"`
	TraversalLimitReached bool                `json:"traversal_limit_reached"`
	Warnings              []DiscoveryIssue    `json:"warnings"`
	Errors                []DiscoveryIssue    `json:"errors"`
	AccessFailures        []DiscoveryFailure  `json:"access_failures,omitempty"`
	ArtifactIndicators    []ArtifactIndicator `json:"artifact_indicators,omitempty"`
	LimitReached          bool                `json:"limit_reached"`
}

func (r DiscoveryResult) RootOrRequestedRoot() string {
	if r.Root != "" {
		return r.Root
	}
	return r.RequestedRoot
}

type Discoverer struct {
	Prober Prober
}

func (d Discoverer) Discover(options DiscoveryOptions) (DiscoveryResult, error) {
	if options.MaxDepth <= 0 {
		options.MaxDepth = defaultMaxDepth
	}
	if options.MaxRepositories <= 0 {
		options.MaxRepositories = defaultMaxRepositories
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = defaultMaxEntries
	}
	if options.MaxDuration <= 0 {
		options.MaxDuration = defaultMaxDuration
	}
	result := DiscoveryResult{
		RequestedRoot:      options.Root,
		Items:              []InventoryItem{},
		Warnings:           []DiscoveryIssue{},
		Errors:             []DiscoveryIssue{},
		AccessFailures:     []DiscoveryFailure{},
		ArtifactIndicators: []ArtifactIndicator{},
	}
	if _, statErr := os.Lstat(options.Root); statErr != nil {
		result.Status = DiscoveryFailed
		reason := ReasonAccessDenied
		if os.IsNotExist(statErr) {
			reason = ReasonRootNotFound
		}
		result.Errors = append(result.Errors, DiscoveryIssue{Path: options.Root, Reason: reason})
		return result, nil
	}
	root, err := CanonicalPath(options.Root)
	if err != nil {
		result.Status = DiscoveryFailed
		reason := ReasonAccessDenied
		if os.IsNotExist(err) {
			reason = ReasonRootNotFound
		}
		result.Errors = append(result.Errors, DiscoveryIssue{Path: options.Root, Reason: reason})
		return result, nil
	}
	result.Root = root
	linked, err := safety.IsLinkOrReparse(options.Root)
	if err != nil {
		result.Status = DiscoveryFailed
		result.Errors = append(result.Errors, DiscoveryIssue{Path: options.Root, Reason: ReasonAccessDenied})
		return result, nil
	}
	if linked {
		result.Status = DiscoveryFailed
		result.SkippedCount = 1
		result.Errors = append(result.Errors, DiscoveryIssue{Path: options.Root, Reason: ReasonReparsePointSkipped})
		return result, nil
	}
	seenRoots := map[string]bool{}
	deadline := time.Now().Add(options.MaxDuration)
	entriesScanned := 0
	partial := false
	addWarning := func(path, reason string) {
		result.Warnings = append(result.Warnings, DiscoveryIssue{Path: path, Reason: reason})
	}
	markLimit := func(path, reason string) {
		result.LimitReached = true
		result.TraversalLimitReached = true
		partial = true
		addWarning(path, reason)
	}
	var scan func(string, int)
	scan = func(directory string, depth int) {
		if result.LimitReached {
			return
		}
		if time.Now().After(deadline) {
			result.SkippedCount++
			markLimit(directory, ReasonTimeLimitReached)
			return
		}
		if depth > options.MaxDepth {
			result.SkippedCount++
			markLimit(directory, ReasonMaxDepthReached)
			return
		}
		entries, readErr := os.ReadDir(directory)
		if readErr != nil {
			result.SkippedCount++
			partial = true
			result.AccessFailures = append(result.AccessFailures, DiscoveryFailure{Path: directory, Reason: ReasonAccessDenied})
			addWarning(directory, ReasonAccessDenied)
			return
		}
		hasGit := false
		for _, entry := range entries {
			if entry.Name() == ".git" {
				hasGit = true
				break
			}
		}
		if hasGit {
			probe, probeStatus, probeErr := d.Prober.ProbeWithStatus(directory)
			if probeErr != nil {
				partial = true
				reason := ReasonGitProbeFailed
				result.AccessFailures = append(result.AccessFailures, DiscoveryFailure{Path: directory, Reason: reason})
				addWarning(directory, reason)
			} else if probeStatus == ProbeGitRepositoryInaccessible {
				partial = true
				result.AccessFailures = append(result.AccessFailures, DiscoveryFailure{Path: directory, Reason: ReasonGitRepositoryInaccessible})
				addWarning(directory, ReasonGitRepositoryInaccessible)
			} else if probeStatus == ProbeOK && !seenRoots[probe.PathKey] {
				seenRoots[probe.PathKey] = true
				info, _ := os.Stat(directory)
				item := InventoryItem{Probe: probe, Role: core.RoleUnregistered, States: probe.States()}
				if info != nil {
					item.LastModified = info.ModTime()
				}
				if probe.PrimaryRemote != nil {
					item.RepositoryIdentityKey = probe.PrimaryRemote.Key()
				}
				applyRegistryIdentity(&item, options.Registry)
				result.Items = append(result.Items, item)
				if len(probe.EvidenceErrors) > 0 {
					partial = true
					addWarning(directory, ReasonProbeEvidence)
				}
				if len(result.Items) >= options.MaxRepositories {
					markLimit(directory, ReasonRepositoryLimit)
					return
				}
			}
		}
		if !time.Now().Before(deadline) {
			result.SkippedCount += countDirectories(entries)
			markLimit(directory, ReasonTimeLimitReached)
			return
		}
		for index, entry := range entries {
			if result.LimitReached {
				continue
			}
			if entriesScanned >= options.MaxEntries {
				result.SkippedCount += countDirectories(entries[index:])
				markLimit(directory, ReasonEntryLimitReached)
				return
			}
			entriesScanned++
			if time.Now().After(deadline) {
				result.SkippedCount += countDirectories(entries[index:])
				markLimit(directory, ReasonTimeLimitReached)
				return
			}
			if entry.Name() == ".git" {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			linked, linkErr := safety.IsLinkOrReparse(path)
			if linkErr != nil {
				if entry.IsDir() {
					result.SkippedCount++
					partial = true
					result.AccessFailures = append(result.AccessFailures, DiscoveryFailure{Path: path, Reason: ReasonAccessDenied})
					addWarning(path, ReasonAccessDenied)
				}
				continue
			}
			if linked {
				result.SkippedCount++
				partial = true
				addWarning(path, ReasonReparsePointSkipped)
				continue
			}
			if entry.IsDir() {
				if depth >= options.MaxDepth {
					result.SkippedCount++
					markLimit(path, ReasonMaxDepthReached)
					continue
				}
				scan(path, depth+1)
				continue
			}
			if indicator := artifactIndicator(entry.Name()); indicator != "" {
				result.ArtifactIndicators = append(result.ArtifactIndicators, ArtifactIndicator{Path: path, Indicator: indicator})
			}
		}
	}
	scan(root, 0)
	classifyIndependentClones(result.Items)
	result.CandidateCount = len(result.Items)
	if len(result.ArtifactIndicators) > 0 {
		addWarning(result.ArtifactIndicators[0].Path, "ARTIFACT_INDICATOR_FOUND")
	}
	if len(result.Errors) > 0 {
		result.Status = DiscoveryFailed
	} else if partial || result.TraversalLimitReached || len(result.AccessFailures) > 0 {
		result.Status = DiscoveryPartial
	} else if result.CandidateCount == 0 && len(result.Warnings) == 0 {
		result.Status = DiscoveryNoCandidates
	} else if len(result.Warnings) > 0 {
		result.Status = DiscoverySuccessWithWarnings
	} else {
		result.Status = DiscoverySuccess
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Probe.PathKey < result.Items[j].Probe.PathKey })
	sort.Slice(result.AccessFailures, func(i, j int) bool { return result.AccessFailures[i].Path < result.AccessFailures[j].Path })
	sort.Slice(result.ArtifactIndicators, func(i, j int) bool { return result.ArtifactIndicators[i].Path < result.ArtifactIndicators[j].Path })
	return result, nil
}

func countDirectories(entries []os.DirEntry) int {
	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			count++
		}
	}
	return count
}

func applyRegistryIdentity(item *InventoryItem, registry core.Registry) {
	for _, registered := range registry.Workspaces {
		if registered.PathKey != item.Probe.PathKey {
			continue
		}
		item.WorkspaceID = registered.ID
		item.RepositoryID = registered.RepositoryID
		item.Role = registered.Role
		for _, repository := range registry.Repositories {
			if repository.ID == registered.RepositoryID {
				item.ProjectID = repository.ProjectID
				identityMatches := item.Probe.PrimaryRemote != nil && repository.Identity.Equal(*item.Probe.PrimaryRemote)
				pathsMatch := registered.GitRootKey == item.Probe.GitRootKey && registered.GitDirKey == item.Probe.GitDirKey && registered.GitCommonDirKey == item.Probe.GitCommonDirKey
				if !identityMatches || !pathsMatch {
					appendState(item, core.StateStaleEvidence)
					appendState(item, core.StateReviewRequired)
				}
				return
			}
		}
	}
	if item.Role == core.RoleUnregistered {
		appendState(item, core.StateReviewRequired)
	}
}

func appendState(item *InventoryItem, state core.WorkspaceState) {
	if !containsState(item.States, state) {
		item.States = append(item.States, state)
	}
}

func classifyIndependentClones(items []InventoryItem) {
	groups := map[string]map[string]int{}
	groupItems := map[string][]int{}
	for index, item := range items {
		if item.RepositoryIdentityKey == "" {
			continue
		}
		if groups[item.RepositoryIdentityKey] == nil {
			groups[item.RepositoryIdentityKey] = map[string]int{}
		}
		groups[item.RepositoryIdentityKey][item.Probe.GitCommonDirKey]++
		groupItems[item.RepositoryIdentityKey] = append(groupItems[item.RepositoryIdentityKey], index)
	}
	for i := range items {
		group := groups[items[i].RepositoryIdentityKey]
		if items[i].RepositoryIdentityKey != "" && len(group) > 1 && items[i].Probe.Topology == core.TopologyMainWorktree {
			items[i].Probe.Topology = core.TopologyIndependentClone
		}
	}
	for identity, indices := range groupItems {
		if len(groups[identity]) <= 1 {
			continue
		}
		for _, index := range indices {
			local := refOIDSet(items[index].Probe)
			others := map[string]bool{}
			for _, otherIndex := range indices {
				if items[otherIndex].Probe.GitCommonDirKey == items[index].Probe.GitCommonDirKey {
					continue
				}
				for oid := range refOIDSet(items[otherIndex].Probe) {
					others[oid] = true
				}
			}
			items[index].RelatedCloneCount = len(groups[identity]) - 1
			for oid := range local {
				if others[oid] {
					items[index].SharedRefOIDCount++
				} else {
					items[index].RefOIDsOnlyHereCount++
				}
			}
			if items[index].RefOIDsOnlyHereCount > 0 && !containsState(items[index].States, core.StateReviewRequired) {
				items[index].States = append(items[index].States, core.StateReviewRequired)
			}
		}
	}
}

func refOIDSet(probe core.WorkspaceProbe) map[string]bool {
	result := map[string]bool{}
	if probe.Head != "" {
		result[probe.Head] = true
	}
	for _, ref := range probe.Refs {
		if ref.OID != "" {
			result[ref.OID] = true
		}
	}
	return result
}

func containsState(states []core.WorkspaceState, expected core.WorkspaceState) bool {
	for _, state := range states {
		if state == expected {
			return true
		}
	}
	return false
}

func artifactIndicator(name string) string {
	lower := strings.ToLower(name)
	for _, extension := range []string{".zip", ".7z", ".tar", ".tgz", ".bundle", ".bak"} {
		if strings.HasSuffix(lower, extension) {
			return "archive_or_backup_extension"
		}
	}
	return ""
}
