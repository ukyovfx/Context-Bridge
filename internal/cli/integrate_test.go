package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/registry"
)

func TestIntegrateCleanRepoCreatesManifestRouteAndRegistration(t *testing.T) {
	repo, home := newIntegrationRepo(t, "clean")
	before := directorySnapshot(t, repo)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"integrate", repo, "--dry-run"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("dry-run failed: %s", stderr.String())
	}
	if before != directorySnapshot(t, repo) || fileExists(filepath.Join(repo, ".contextbridge", "manifest.json")) {
		t.Fatal("integration dry-run mutated the repository")
	}
	if code := Run([]string{"integrate", repo}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "confirmation required") {
		t.Fatalf("repository changes did not require confirmation: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"integrate", repo, "--confirm"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("integration apply failed: %s", stderr.String())
	}
	if !fileExists(filepath.Join(repo, ".contextbridge", "manifest.json")) || !fileExists(filepath.Join(repo, "AGENTS.md")) {
		t.Fatal("integration did not create expected context files")
	}
	value, err := (registry.Store{Home: home}).Load()
	if err != nil || len(value.Projects) != 1 || len(value.Repositories) != 1 || len(value.Workspaces) != 1 {
		t.Fatalf("integration did not register exactly one workspace: %v %#v", err, value)
	}
	if !strings.Contains(stdout.String(), "Context Bridge readiness: ready") {
		t.Fatalf("success omitted readiness: %s", stdout.String())
	}
}

func TestIntegratePreservesDirtyExistingAgentsAndIsIdempotent(t *testing.T) {
	repo, _ := newIntegrationRepo(t, "preserve")
	agents := "# Existing Instructions\n\nKeep this exact content.\n"
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(agents), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("uncommitted\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"integrate", repo, "--confirm"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("integration failed: %s", stderr.String())
	}
	if data, err := os.ReadFile(filepath.Join(repo, "AGENTS.md")); err != nil || !strings.HasPrefix(string(data), agents) || !strings.Contains(string(data), "## Context Bridge") {
		t.Fatalf("existing AGENTS content was not preserved: %q", data)
	}
	if data, err := os.ReadFile(filepath.Join(repo, "dirty.txt")); err != nil || string(data) != "uncommitted\n" {
		t.Fatal("dirty user change was lost")
	}
	before := directorySnapshot(t, repo)
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"integrate", repo}, &stdout, &stderr, "test"); code != 0 || !strings.Contains(stdout.String(), "already_integrated") {
		t.Fatalf("repeated integration was not a no-op: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if before != directorySnapshot(t, repo) {
		t.Fatal("repeated integration mutated the repository")
	}
}

func TestIntegrateAndHandoffAllowStaleRegisteredBranchWithoutAutoHealing(t *testing.T) {
	repo, home := newIntegrationRepo(t, "kitsusync")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"integrate", repo, "--confirm"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("initial integration failed: %s", stderr.String())
	}
	store := registry.Store{Home: home}
	before, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if before.Repositories[0].CanonicalBranch != "main" {
		t.Fatalf("unexpected fixture branch: %#v", before.Repositories[0])
	}
	runTestGit(t, repo, "checkout", "-b", "archive/kitsusync-clean/codex/deploy-rollback-transaction")

	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"integrate", repo}, &stdout, &stderr, "test"); code != 0 || !strings.Contains(stdout.String(), "already_integrated") {
		t.Fatalf("integration rejected stale registered branch: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"guard", "--project", "kitsusync", "--workspace", repo}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("Guard rejected stable workspace identity after branch change: %s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"doctor", "--project", repo}, &stdout, &stderr, "test"); code != 0 || !strings.Contains(stdout.String(), "workspace identity remains valid") {
		t.Fatalf("doctor treated mutable branch state as identity drift: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"handoff", "kitsusync", "--task", "inspect current state", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("handoff rejected stable workspace identity after branch change: %s", stderr.String())
	}
	after, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.Repositories[0].CanonicalBranch != before.Repositories[0].CanonicalBranch || after.Workspaces[0].PathKey != before.Workspaces[0].PathKey {
		t.Fatalf("read-only reconciliation silently healed persistent registry state: before=%#v after=%#v", before, after)
	}
}

func TestIntegrateSurfacesContextConflictAndIdentityChange(t *testing.T) {
	repo, _ := newIntegrationRepo(t, "conflict")
	if err := os.MkdirAll(filepath.Join(repo, "docs", "agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "CLAUDE.md"), []byte("# Claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "agent", "START-HERE.md"), []byte("# Start\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "agent", "CURRENT-STATE.md"), []byte("# State\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"integrate", repo, "--dry-run"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stdout.String(), "MULTIPLE_AGENT_ENTRYPOINTS") {
		t.Fatalf("instruction conflict was not surfaced: code=%d stdout=%s", code, stdout.String())
	}

	clean, _ := newIntegrationRepo(t, "identity")
	old := integrateRevalidate
	integrateRevalidate = func(string, string) error { return errors.New(reasonIdentityChangedAfterPlan) }
	defer func() { integrateRevalidate = old }()
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"integrate", clean, "--confirm"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stdout.String(), reasonIdentityChangedAfterPlan) || fileExists(filepath.Join(clean, ".contextbridge", "manifest.json")) {
		t.Fatalf("identity change was not fail-closed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestIntegrateRefusesRegistryCanonicalConflict(t *testing.T) {
	first, home := newIntegrationRepo(t, "first")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"integrate", first, "--confirm"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("first integration failed: %s", stderr.String())
	}
	second, _ := newIntegrationRepo(t, "second")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	runTestGit(t, second, "remote", "set-url", "origin", "https://github.com/example/first.git")
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"integrate", second, "--dry-run"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stdout.String(), "another canonical workspace") {
		t.Fatalf("canonical registry conflict was not refused: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if value, err := (registry.Store{Home: home}).Load(); err != nil || len(value.Workspaces) != 1 {
		t.Fatalf("registry conflict changed existing registration: %v %#v", err, value)
	}
}

func newIntegrationRepo(t *testing.T, name string) (string, string) {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, name)
	home := filepath.Join(root, "registry")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "init", "-b", "main")
	runTestGit(t, repo, "remote", "add", "origin", "https://github.com/example/"+name+".git")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, repo, "add", "README.md")
	runTestGit(t, repo, "commit", "-m", "initial")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	return repo, home
}
