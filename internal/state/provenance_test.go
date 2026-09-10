package state

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func runGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Context Bridge Tests", "GIT_AUTHOR_EMAIL=contextbridge@example.invalid",
		"GIT_COMMITTER_NAME=Context Bridge Tests", "GIT_COMMITTER_EMAIL=contextbridge@example.invalid",
	)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, data)
	}
	return strings.TrimSpace(string(data))
}

func writeState(t *testing.T, repository, basis string) {
	t.Helper()
	path := filepath.Join(repository, "docs", "agent", "CURRENT-STATE.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	contents := fmt.Sprintf("---\ncontextbridge_state_schema: 1\nbasis_branch: main\nbasis_commit: %s\nbasis_date: %s\n---\n# Current State\n", basis, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateAllowsMetadataOnlyStateCommitAndDetectsProjectChanges(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "product.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "product.txt")
	runGit(t, repo, "commit", "-m", "product")
	basis := runGit(t, repo, "rev-parse", "HEAD")
	writeState(t, repo, basis)
	if err := os.MkdirAll(filepath.Join(repo, ".contextbridge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".contextbridge", "manifest.json"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "docs/agent/CURRENT-STATE.md", ".contextbridge/manifest.json")
	runGit(t, repo, "commit", "-m", "state metadata")
	report := Evaluate(repo, workspace.CommandRunner{})
	if report.ContentIntegrity != "ok" || report.BasisValidity != "valid" || report.BasisFreshness != "fresh" {
		t.Fatalf("metadata-only state was not fresh: %#v", report)
	}
	if err := os.WriteFile(filepath.Join(repo, "product.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "product.txt")
	runGit(t, repo, "commit", "-m", "product change")
	report = Evaluate(repo, workspace.CommandRunner{})
	if report.BasisFreshness != "stale" {
		t.Fatalf("project change was not stale: %#v", report)
	}
}

func TestEvaluateDetectsUncommittedProjectChange(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "product.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "product.txt")
	runGit(t, repo, "commit", "-m", "product")
	basis := runGit(t, repo, "rev-parse", "HEAD")
	writeState(t, repo, basis)
	runGit(t, repo, "add", "docs/agent/CURRENT-STATE.md")
	runGit(t, repo, "commit", "-m", "state metadata")
	if err := os.WriteFile(filepath.Join(repo, "product.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report := Evaluate(repo, workspace.CommandRunner{})
	if report.BasisFreshness != "stale" {
		t.Fatalf("uncommitted project change was not stale: %#v", report)
	}
}
