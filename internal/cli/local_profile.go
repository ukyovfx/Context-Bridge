package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/localprofile"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
	"github.com/ukyovfx/Context-Bridge/internal/safety"
)

// emitLocalProfileDiagnostics is deliberately advisory. A profile never
// participates in repository identity, Guard decisions, or mutation plans.
func emitLocalProfileDiagnostics(stdout io.Writer, manifest core.Manifest) {
	home, err := registry.DefaultHome()
	if err != nil {
		return
	}
	profile, err := localprofile.Load(home)
	if errors.Is(err, localprofile.ErrNotConfigured) {
		return
	}
	if err != nil {
		fmt.Fprintln(stdout, "profile finding: profile LOCAL_PROFILE_INVALID")
		return
	}

	findings := profile.LayoutFindings()
	if manifest.SchemaVersion == 2 && manifest.Repository != nil {
		if placements := registeredWorkspaceFindings(profile, manifest.Repository.ID); len(placements) > 0 {
			findings = append(findings, placements...)
		}
	}
	if inside, checkErr := safety.InsideGitRepository(profile.Layout().PrivateRoot); checkErr == nil && inside {
		findings = append(findings, localprofile.Finding{Fields: []string{"private_root"}, Code: "PRIVATE_ROOT_INSIDE_GIT_WORKTREE"})
	}
	if len(findings) == 0 {
		fmt.Fprintln(stdout, "local workspace profile: valid")
		return
	}
	for _, finding := range findings {
		fmt.Fprintf(stdout, "profile finding: %s %s\n", finding.FieldsText(), finding.Code)
	}
}

func localProfileStatus() (string, []string) {
	home, err := registry.DefaultHome()
	if err != nil {
		return "UNREADABLE", []string{"LOCAL_PROFILE_UNREADABLE"}
	}
	profile, err := localprofile.Load(home)
	if errors.Is(err, localprofile.ErrNotConfigured) {
		return "NOT_CONFIGURED", nil
	}
	if err != nil {
		return "INVALID", []string{"LOCAL_PROFILE_INVALID"}
	}
	findings := profile.LayoutFindings()
	if len(findings) > 0 {
		codes := make([]string, 0, len(findings))
		for _, finding := range findings {
			codes = append(codes, finding.Code)
		}
		return "WARNING", codes
	}
	return "READY", nil
}

func registeredWorkspaceFindings(profile localprofile.Profile, repositoryID string) []localprofile.Finding {
	if repositoryID == "" {
		return nil
	}
	home, err := registry.DefaultHome()
	if err != nil {
		return nil
	}
	value, err := (registry.Store{Home: home}).Load()
	if err != nil {
		return nil
	}
	findings := []localprofile.Finding{}
	for _, workspaceRecord := range value.Workspaces {
		if workspaceRecord.RepositoryID != repositoryID {
			continue
		}
		if workspaceRecord.Role == core.RoleCanonical {
			if finding, outside := profile.CanonicalWorkspaceFinding(workspaceRecord.Path); outside {
				findings = append(findings, finding)
			}
		}
		if workspaceRecord.Role == core.RoleManagedWorkspace {
			if finding, outside := profile.ManagedWorkspaceFinding(workspaceRecord.Path); outside {
				findings = append(findings, finding)
			}
		}
	}
	return findings
}
