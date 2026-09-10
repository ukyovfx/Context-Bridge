package workspace

import (
	"errors"
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
)

type DiscoveryOptions struct {
	Root            string
	MaxDepth        int
	MaxRepositories int
	Registry        core.Registry
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
	Root               string              `json:"root"`
	Items              []InventoryItem     `json:"items"`
	AccessFailures     []DiscoveryFailure  `json:"access_failures,omitempty"`
	ArtifactIndicators []ArtifactIndicator `json:"artifact_indicators,omitempty"`
	LimitReached       bool                `json:"limit_reached"`
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
	root, err := CanonicalPath(options.Root)
	if err != nil {
		return DiscoveryResult{}, err
	}
	linked, err := safety.IsLinkOrReparse(options.Root)
	if err != nil {
		return DiscoveryResult{}, err
	}
	if linked {
		return DiscoveryResult{}, errors.New("discovery root cannot be a symlink or reparse point")
	}
	result := DiscoveryResult{Root: root, Items: []InventoryItem{}}
	seenRoots := map[string]bool{}
	var scan func(string, int)
	scan = func(directory string, depth int) {
		if result.LimitReached || depth > options.MaxDepth {
			return
		}
		entries, readErr := os.ReadDir(directory)
		if readErr != nil {
			result.AccessFailures = append(result.AccessFailures, DiscoveryFailure{Path: directory, Reason: "access_denied_or_unreadable"})
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
			probe, probeErr := d.Prober.Probe(directory)
			if probeErr != nil {
				result.AccessFailures = append(result.AccessFailures, DiscoveryFailure{Path: directory, Reason: "git_probe_failed"})
			} else if probe.Topology != core.TopologyNonGit && !seenRoots[probe.PathKey] {
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
				if len(result.Items) >= options.MaxRepositories {
					result.LimitReached = true
					return
				}
			}
		}
		for _, entry := range entries {
			if result.LimitReached || entry.Name() == ".git" {
				continue
			}
			path := filepath.Join(directory, entry.Name())
			if entry.IsDir() {
				linked, linkErr := safety.IsLinkOrReparse(path)
				if linkErr != nil {
					result.AccessFailures = append(result.AccessFailures, DiscoveryFailure{Path: path, Reason: "metadata_unreadable"})
					continue
				}
				if !linked {
					scan(path, depth+1)
				}
				continue
			}
			if indicator := artifactIndicator(entry.Name()); indicator != "" {
				result.ArtifactIndicators = append(result.ArtifactIndicators, ArtifactIndicator{Path: path, Indicator: indicator})
			}
		}
	}
	scan(root, 0)
	classifyIndependentClones(result.Items)
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Probe.PathKey < result.Items[j].Probe.PathKey })
	sort.Slice(result.AccessFailures, func(i, j int) bool { return result.AccessFailures[i].Path < result.AccessFailures[j].Path })
	sort.Slice(result.ArtifactIndicators, func(i, j int) bool { return result.ArtifactIndicators[i].Path < result.ArtifactIndicators[j].Path })
	return result, nil
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
