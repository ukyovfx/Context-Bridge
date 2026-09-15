package core

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type Registry struct {
	SchemaVersion int                `json:"schema_version"`
	Projects      []ProjectRecord    `json:"projects"`
	Repositories  []RepositoryRecord `json:"repositories"`
	Workspaces    []WorkspaceRecord  `json:"workspaces"`
}

type ProjectRecord struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	Aliases     []string `json:"aliases,omitempty"`
}

type RepositoryRecord struct {
	ID                string         `json:"id"`
	ProjectID         string         `json:"project_id"`
	Identity          RemoteIdentity `json:"identity"`
	PrimaryRemoteName string         `json:"primary_remote_name"`
	CanonicalBranch   string         `json:"canonical_branch"`
}

type WorkspaceRecord struct {
	ID               string                      `json:"id"`
	RepositoryID     string                      `json:"repository_id"`
	Path             string                      `json:"path"`
	PathKey          string                      `json:"path_key"`
	GitRoot          string                      `json:"git_root"`
	GitRootKey       string                      `json:"git_root_key"`
	GitDir           string                      `json:"git_dir"`
	GitDirKey        string                      `json:"git_dir_key"`
	GitCommonDir     string                      `json:"git_common_dir"`
	GitCommonDirKey  string                      `json:"git_common_dir_key"`
	Topology         WorkspaceTopology           `json:"topology"`
	Role             RegistryRole                `json:"role"`
	PhysicalIdentity WorkspaceFilesystemIdentity `json:"physical_identity,omitempty"`
}

func NewRegistry() Registry {
	return Registry{SchemaVersion: 1, Projects: []ProjectRecord{}, Repositories: []RepositoryRecord{}, Workspaces: []WorkspaceRecord{}}
}

func (r Registry) Validate() error {
	if r.SchemaVersion != 1 {
		return errors.New("unsupported registry schema_version")
	}
	projects := map[string]bool{}
	aliases := map[string]bool{}
	for _, project := range r.Projects {
		if err := ValidateID(project.ID, ProjectIDPrefix); err != nil {
			return err
		}
		if project.DisplayName == "" || projects[project.ID] {
			return errors.New("registry has an invalid or duplicate project")
		}
		projects[project.ID] = true
		for _, alias := range append([]string{project.DisplayName}, project.Aliases...) {
			if alias == "" || aliases[alias] {
				return fmt.Errorf("registry has an empty or ambiguous project alias %q", alias)
			}
			aliases[alias] = true
		}
	}
	repositories := map[string]bool{}
	repositoryProjects := map[string]bool{}
	repositoryIdentities := map[string]bool{}
	for _, repository := range r.Repositories {
		if err := ValidateID(repository.ID, RepositoryIDPrefix); err != nil {
			return err
		}
		if !projects[repository.ProjectID] || repositories[repository.ID] || repositoryProjects[repository.ProjectID] {
			return errors.New("registry has an invalid repository relationship")
		}
		if err := repository.Identity.Validate(); err != nil {
			return fmt.Errorf("repository %s: %w", repository.ID, err)
		}
		if repository.PrimaryRemoteName == "" || repository.CanonicalBranch == "" {
			return errors.New("repository requires primary remote and canonical branch")
		}
		if repositoryIdentities[repository.Identity.Key()] {
			return errors.New("registry has duplicate repository identities")
		}
		repositories[repository.ID] = true
		repositoryProjects[repository.ProjectID] = true
		repositoryIdentities[repository.Identity.Key()] = true
	}
	workspaces := map[string]bool{}
	paths := map[string]bool{}
	canonicalRepositories := map[string]bool{}
	for _, workspace := range r.Workspaces {
		if err := ValidateID(workspace.ID, WorkspaceIDPrefix); err != nil {
			return err
		}
		if !repositories[workspace.RepositoryID] || workspaces[workspace.ID] {
			return errors.New("registry has an invalid workspace relationship")
		}
		if !filepath.IsAbs(workspace.Path) || !filepath.IsAbs(workspace.GitRoot) || !filepath.IsAbs(workspace.GitDir) || !filepath.IsAbs(workspace.GitCommonDir) || workspace.PathKey == "" || workspace.GitRootKey == "" || workspace.GitDirKey == "" || workspace.GitCommonDirKey == "" {
			return errors.New("workspace requires absolute canonical Git paths")
		}
		if workspace.PathKey != registryPathKey(workspace.Path) || workspace.GitRootKey != registryPathKey(workspace.GitRoot) || workspace.GitDirKey != registryPathKey(workspace.GitDir) || workspace.GitCommonDirKey != registryPathKey(workspace.GitCommonDir) {
			return errors.New("workspace path keys do not match their canonical paths")
		}
		if paths[workspace.PathKey] {
			return errors.New("registry has duplicate workspace paths")
		}
		if workspace.Role != RoleCanonical && workspace.Role != RoleManagedWorkspace && workspace.Role != RoleRegisteredAlternate {
			return errors.New("workspace has an invalid registry role")
		}
		if workspace.Topology != TopologyMainWorktree && workspace.Topology != TopologyLinkedWorktree && workspace.Topology != TopologyIndependentClone {
			return errors.New("workspace has an invalid Git topology")
		}
		if !workspace.PhysicalIdentity.ValidOrEmpty() {
			return errors.New("workspace has invalid physical filesystem identity")
		}
		if workspace.Role == RoleCanonical {
			if canonicalRepositories[workspace.RepositoryID] {
				return errors.New("repository has multiple canonical workspaces")
			}
			canonicalRepositories[workspace.RepositoryID] = true
		}
		workspaces[workspace.ID] = true
		paths[workspace.PathKey] = true
	}
	return nil
}

func registryPathKey(path string) string {
	key := filepath.ToSlash(filepath.Clean(path))
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	return key
}

func (r Registry) Sorted() Registry {
	out := r
	out.Projects = append([]ProjectRecord(nil), r.Projects...)
	out.Repositories = append([]RepositoryRecord(nil), r.Repositories...)
	out.Workspaces = append([]WorkspaceRecord(nil), r.Workspaces...)
	sort.Slice(out.Projects, func(i, j int) bool { return out.Projects[i].ID < out.Projects[j].ID })
	sort.Slice(out.Repositories, func(i, j int) bool { return out.Repositories[i].ID < out.Repositories[j].ID })
	sort.Slice(out.Workspaces, func(i, j int) bool { return out.Workspaces[i].ID < out.Workspaces[j].ID })
	return out
}
