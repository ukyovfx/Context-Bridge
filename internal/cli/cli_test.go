package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestDiscoverAlwaysReportsHumanAndJSONTerminalStatus(t *testing.T) {
	root := t.TempDir()
	t.Setenv("CONTEXTBRIDGE_HOME", filepath.Join(root, "registry"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"discover", "--root", root}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("human discovery failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "terminal_status=no_candidates") {
		t.Fatalf("human discovery omitted terminal status: %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"discover", "--root", root, "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("JSON discovery failed: %s", stderr.String())
	}
	var result struct {
		Status         string `json:"status"`
		CandidateCount int    `json:"candidate_count"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("discovery JSON was invalid: %v\n%s", err, stdout.String())
	}
	if result.Status != "no_candidates" || result.CandidateCount != 0 {
		t.Fatalf("unexpected JSON result: %#v", result)
	}
}

func TestDiscoverJSONReportsMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	t.Setenv("CONTEXTBRIDGE_HOME", filepath.Join(t.TempDir(), "registry"))
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"discover", "--root", root, "--json"}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("missing root unexpectedly succeeded")
	}
	var result struct {
		Status string `json:"status"`
		Errors []struct {
			Reason string `json:"reason"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("failed discovery JSON was invalid: %v\n%s", err, stdout.String())
	}
	if result.Status != "failed" || len(result.Errors) != 1 || result.Errors[0].Reason != "ROOT_NOT_FOUND" {
		t.Fatalf("unexpected missing-root result: %#v", result)
	}
}

func TestInitDryRunPerformsZeroMutation(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "dry-project")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", "dry-project", "--root", root, "--local-only", "--dry-run"}, &stdout, &stderr, "test")
	if code != 0 {
		t.Fatalf("dry-run failed: %s", stderr.String())
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("dry-run mutated target: %v", err)
	}
	if !strings.Contains(stdout.String(), "zero mutations performed") {
		t.Fatal("dry-run did not report zero mutation")
	}
}

func TestDoctorDetectsStaleCurrentState(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GIT_AUTHOR_NAME", "Context Bridge Tests")
	t.Setenv("GIT_AUTHOR_EMAIL", "contextbridge-tests@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Context Bridge Tests")
	t.Setenv("GIT_COMMITTER_EMAIL", "contextbridge-tests@example.invalid")
	var stdout, stderr bytes.Buffer
	code := Run([]string{"init", "doctor-project", "--root", root, "--local-only"}, &stdout, &stderr, "test")
	if code != 0 {
		t.Fatalf("init failed: %s", stderr.String())
	}
	project := filepath.Join(root, "doctor-project")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"doctor", "--project", project}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("doctor failed: %s", stderr.String())
	}
	currentState := filepath.Join(project, "docs", "agent", "CURRENT-STATE.md")
	file, err := os.OpenFile(currentState, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("\nstale\n"); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"doctor", "--project", project}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("doctor accepted stale CURRENT-STATE")
	}
	if !strings.Contains(stderr.String(), "CURRENT-STATE has uncommitted content changes") {
		t.Fatalf("unexpected diagnostic: %s", stderr.String())
	}
}

func TestRegistryRegisterDryRunPerformsZeroMutation(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "existing")
	home := filepath.Join(root, "registry-home")
	if err := os.Mkdir(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repository, "init", "-b", "main")
	runTestGit(t, repository, "remote", "add", "origin", "https://github.com/ukyovfx/existing.git")
	if err := os.WriteFile(filepath.Join(repository, "content.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repository, "add", "content.txt")
	runTestGit(t, repository, "commit", "-m", "existing")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	before := directorySnapshot(t, repository)
	var stdout, stderr bytes.Buffer
	code := Run([]string{"registry", "register", repository, "--dry-run"}, &stdout, &stderr, "test")
	if code != 0 {
		t.Fatalf("registration dry-run failed: %s", stderr.String())
	}
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Fatalf("registration dry-run mutated registry home: %v", err)
	}
	after := directorySnapshot(t, repository)
	if before != after {
		t.Fatalf("registration dry-run mutated the existing repository\nbefore:\n%s\nafter:\n%s", before, after)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"registry", "register", repository}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("registration failed: %s", stderr.String())
	}
	if actual := directorySnapshot(t, repository); before != actual {
		t.Fatalf("registration mutated the existing repository\nbefore:\n%s\nafter:\n%s", before, actual)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"guard", "--project", "existing", "--workspace", repository}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("guard rejected registered workspace: %s", stderr.String())
	}
	runTestGit(t, repository, "checkout", "-b", "wrong-branch")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"guard", "--project", "existing", "--workspace", repository}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "BRANCH_MISMATCH") {
		t.Fatalf("guard accepted wrong branch: %s", stderr.String())
	}
	runTestGit(t, repository, "checkout", "main")
	runTestGit(t, repository, "remote", "set-url", "origin", "https://evil.example/ukyovfx/existing.git")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"guard", "--project", "existing", "--workspace", repository}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "PRIMARY_REMOTE_MISMATCH") {
		t.Fatalf("guard accepted wrong remote: %s", stderr.String())
	}
}

func runTestGit(t *testing.T, directory string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = directory
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Context Bridge Tests", "GIT_AUTHOR_EMAIL=contextbridge@example.invalid",
		"GIT_COMMITTER_NAME=Context Bridge Tests", "GIT_COMMITTER_EMAIL=contextbridge@example.invalid",
	)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, data)
	}
}

func directorySnapshot(t *testing.T, root string) string {
	t.Helper()
	items := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			items = append(items, fmt.Sprintf("%s|directory", filepath.ToSlash(relative)))
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(data))
		items = append(items, fmt.Sprintf("%s|%s|%d|%d", filepath.ToSlash(relative), hash, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(items)
	return strings.Join(items, "\n")
}
