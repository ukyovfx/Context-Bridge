package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

type chatGPTBootstrap struct {
	Status             string   `json:"status"`
	Project            string   `json:"project"`
	ProjectResolved    bool     `json:"project_resolved"`
	CanonicalWorkspace string   `json:"canonical_workspace"`
	Guard              string   `json:"guard"`
	GitHubRemote       string   `json:"github_remote"`
	BootstrapReadiness string   `json:"bootstrap_readiness"`
	Warnings           []string `json:"warnings"`
	Instructions       string   `json:"instructions,omitempty"`
}

func runBootstrap(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "chatgpt" {
		return errors.New("bootstrap requires chatgpt <project>")
	}
	projectSelector, remaining, err := takePath(args[1:], "bootstrap chatgpt")
	if err != nil {
		return err
	}
	flags := flag.NewFlagSet("bootstrap chatgpt", flag.ContinueOnError)
	flags.SetOutput(stderr)
	jsonOutput := flags.Bool("json", false, "emit JSON")
	if err := flags.Parse(remaining); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	result, err := buildChatGPTBootstrap(projectSelector)
	if err != nil {
		emitChatGPTBootstrap(stdout, result, *jsonOutput)
		return err
	}
	emitChatGPTBootstrap(stdout, result, *jsonOutput)
	return nil
}

func buildChatGPTBootstrap(selector string) (chatGPTBootstrap, error) {
	result := chatGPTBootstrap{Status: "BLOCKED", Project: selector, Warnings: []string{}}
	store, err := registryStore()
	if err != nil {
		return result, err
	}
	value, err := store.Load()
	if err != nil {
		return result, err
	}
	project, err := resolveProject(value, selector)
	if err != nil {
		return result, errors.New("project is not registered or is ambiguous")
	}
	result.Project = project.DisplayName
	result.ProjectResolved = true
	repository, err := repositoryForProject(value, project.ID)
	if err != nil {
		return result, err
	}
	registered, ok := canonicalWorkspace(value, repository.ID)
	if !ok {
		return result, errors.New("canonical workspace is not registered")
	}
	result.CanonicalWorkspace = "registered"
	canonical, err := workspace.CanonicalPath(registered.Path)
	if err != nil {
		return result, err
	}
	manifestLocalOnly := false
	if manifest, manifestErr := readManifest(canonical); manifestErr == nil {
		manifestLocalOnly = manifest.Lifecycle.LocalOnly
	}
	probe, status, probeErr := (workspace.Prober{PrimaryRemoteName: repository.PrimaryRemoteName}).ProbeWithStatus(canonical)
	if probeErr != nil || status != workspace.ProbeOK {
		result.Guard = "BLOCKED"
		result.BootstrapReadiness = "BLOCKED"
		return result, errors.New("canonical workspace could not be re-probed")
	}
	if probe.PrimaryRemote != nil && strings.EqualFold(probe.PrimaryRemote.Host, "github.com") {
		result.GitHubRemote = "AVAILABLE"
	} else if probe.PrimaryRemote == nil {
		result.GitHubRemote = "NOT_CONFIGURED"
	} else {
		result.GitHubRemote = "NON_GITHUB"
	}
	decision := core.EvaluateGuard(guardExpected(project, repository, registered, ""), core.GuardActual{ProjectFound: true, ProjectID: project.ID, RepositoryID: repository.ID, WorkspaceID: registered.ID, Probe: probe})
	if decision.Allowed {
		result.Guard = "PASS"
	} else {
		result.Guard = "BLOCKED"
		if manifestLocalOnly {
			result.Warnings = append(result.Warnings, "LOCAL_ONLY: no GitHub remote identity is available for Guard or connected ChatGPT")
		}
		result.Warnings = append(result.Warnings, string(core.WrongWorkspace))
		result.BootstrapReadiness = "BLOCKED"
		return result, core.WrongWorkspaceError{Reasons: decision.Reasons}
	}
	if probe.PrimaryRemote == nil {
		result.Warnings = append(result.Warnings, "LOCAL_ONLY: no GitHub remote is configured; connected ChatGPT cannot see local repository state")
	} else if !strings.EqualFold(probe.PrimaryRemote.Host, "github.com") {
		result.Warnings = append(result.Warnings, "REMOTE_NOT_GITHUB: connected ChatGPT visibility depends on that remote being connected and pushed")
	}
	if probe.Staged || probe.Unstaged || probe.Untracked || probe.LocalUniqueCount > 0 {
		result.Warnings = append(result.Warnings, "UNPUSHED_LOCAL_STATE: dirty or local-only changes are not visible through GitHub; use a handoff or paste the relevant context")
	}
	result.Instructions = chatGPTInstructions(project.DisplayName, repository, probe, contextEntrypoints(probe.GitRoot), result.Warnings)
	result.BootstrapReadiness = "READY_WITH_WARNINGS"
	result.Status = "READY"
	return result, nil
}

func chatGPTInstructions(name string, repository core.RepositoryRecord, probe core.WorkspaceProbe, entrypoints []string, warnings []string) string {
	base := core.ChatGPTProjectInstructions(name)
	identity := repository.Identity.Key()
	if probe.PrimaryRemote != nil {
		identity = probe.PrimaryRemote.Key()
	}
	var b strings.Builder
	b.WriteString(base)
	fmt.Fprintf(&b, "\nPortable repository identity: %s.\n", identity)
	if len(entrypoints) > 0 {
		fmt.Fprintf(&b, "Repository context entrypoints: %s.\n", strings.Join(entrypoints, ", "))
	} else {
		b.WriteString("Repository context entrypoints: inspect the repository's documented entrypoints before relying on chat context.\n")
	}
	b.WriteString("GitHub-connected ChatGPT can see only repository state that has been pushed to the connected remote. Local, dirty, or unpushed changes are not visible there; use a Context Bridge handoff or paste the relevant context.\n")
	b.WriteString("ChatGPT Project Instructions apply inside this project and may override global custom instructions; review them before saving.\n")
	if len(warnings) > 0 {
		b.WriteString("Review these limitations before relying on the bootstrap: ")
		b.WriteString(strings.Join(warnings, "; "))
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func emitChatGPTBootstrap(stdout io.Writer, result chatGPTBootstrap, jsonOutput bool) {
	if jsonOutput {
		_ = emitJSON(stdout, result)
		return
	}
	fmt.Fprintf(stdout, "ChatGPT bootstrap: %s\nProject resolved: %s\nCanonical workspace: %s\nGuard: %s\nGitHub remote: %s\nChatGPT bootstrap readiness: %s\n", result.Status, result.Project, result.CanonicalWorkspace, result.Guard, result.GitHubRemote, result.BootstrapReadiness)
	for _, warning := range result.Warnings {
		fmt.Fprintf(stdout, "Warning: %s\n", warning)
	}
	if result.Instructions != "" {
		fmt.Fprint(stdout, "\nCopy the following into ChatGPT Project Instructions:\n\n")
		fmt.Fprint(stdout, result.Instructions)
	}
}
