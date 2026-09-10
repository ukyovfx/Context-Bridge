package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
)

type githubIdentity struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func readGitHubIdentity() (githubIdentity, error) {
	if _, err := lookPath("gh"); err != nil {
		return githubIdentity{}, err
	}
	data, err := runOutput("", "gh", "api", "user")
	if err != nil {
		return githubIdentity{}, errors.New("GitHub CLI is not authenticated or GitHub is unavailable")
	}
	var identity githubIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		return githubIdentity{}, errors.New("GitHub identity response was invalid")
	}
	if identity.Login == "" || identity.Type != "User" {
		return githubIdentity{}, errors.New("safety abort: authenticated GitHub identity is not a personal user account")
	}
	return identity, nil
}

func requireRepositoryAbsent(owner, project string) error {
	cmd := exec.Command("gh", "api", "repos/"+owner+"/"+project)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return errors.New("safety abort: GitHub repository already exists")
	}
	message := string(output)
	if strings.Contains(message, "HTTP 404") || strings.Contains(message, `"status":"404"`) || strings.Contains(message, `"status": "404"`) {
		return nil
	}
	return errors.New("could not prove that the target GitHub repository is absent")
}

func lookPath(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("required executable %s was not found", name)
	}
	return path, nil
}

func runOutput(directory, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	if directory != "" {
		cmd.Dir = directory
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}

func registryStore() (registry.Store, error) {
	home, err := registry.DefaultHome()
	if err != nil {
		return registry.Store{}, err
	}
	return registry.Store{Home: home}, nil
}

func resolveProject(registryValue core.Registry, selector string) (core.ProjectRecord, error) {
	matches := make([]core.ProjectRecord, 0)
	for _, project := range registryValue.Projects {
		if project.ID == selector || project.DisplayName == selector || contains(project.Aliases, selector) {
			matches = append(matches, project)
		}
	}
	if len(matches) != 1 {
		return core.ProjectRecord{}, errors.New("project selector is missing or ambiguous")
	}
	return matches[0], nil
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func repositoryForProject(registryValue core.Registry, projectID string) (core.RepositoryRecord, error) {
	matches := make([]core.RepositoryRecord, 0)
	for _, repository := range registryValue.Repositories {
		if repository.ProjectID == projectID {
			matches = append(matches, repository)
		}
	}
	if len(matches) != 1 {
		return core.RepositoryRecord{}, errors.New("project repository is missing or ambiguous")
	}
	return matches[0], nil
}

func readManifest(path string) (core.Manifest, error) {
	data, err := osReadFile(filepath.Join(path, ".contextbridge", "manifest.json"))
	if err != nil {
		return core.Manifest{}, err
	}
	return core.ParseManifest(data)
}

var osReadFile = func(path string) ([]byte, error) { return os.ReadFile(path) }
