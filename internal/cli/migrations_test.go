package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/core"
	"github.com/ukyovfx/Context-Bridge/internal/registry"
	"github.com/ukyovfx/Context-Bridge/internal/workspace"
)

func TestAdoptDryRunApplyAndIdempotency(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	if err := os.WriteFile(filepath.Join(fixture.repository, "dirty.txt"), []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(fixture.repository, ".contextbridge", "manifest.json")
	before := directorySnapshot(t, fixture.repository)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"adopt", fixture.repository, "--dry-run", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("adopt dry-run failed: %s", stderr.String())
	}
	var plan migrationPlan
	if err := json.Unmarshal(stdout.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Status != "planned" || !plan.RequiresConfirmation || !strings.Contains(stdout.String(), ".contextbridge/manifest.json") {
		t.Fatalf("unexpected adopt plan: %#v", plan)
	}
	if _, err := os.Stat(manifestPath); !os.IsNotExist(err) || before != directorySnapshot(t, fixture.repository) {
		t.Fatal("adopt dry-run mutated the repository")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"adopt", fixture.repository, "--confirm", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("adopt apply failed: %s", stderr.String())
	}
	if data, err := os.ReadFile(manifestPath); err != nil {
		t.Fatal(err)
	} else if manifest, parseErr := core.ParseManifest(data); parseErr != nil || manifest.SchemaVersion != 2 {
		t.Fatalf("adopt did not create Manifest V2: %v", parseErr)
	}
	if data, err := os.ReadFile(filepath.Join(fixture.repository, "dirty.txt")); err != nil || string(data) != "keep me\n" {
		t.Fatal("adopt changed existing user content")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"adopt", fixture.repository, "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("repeated adopt failed: %s", stderr.String())
	}
	if !strings.Contains(stdout.String(), reasonAlreadyAdopted) {
		t.Fatalf("repeated adopt did not report ALREADY_ADOPTED: %s", stdout.String())
	}
}

func TestAdoptOwnedManifestConflict(t *testing.T) {
	fixture := newHandoffFixture(t)
	manifestPath := filepath.Join(fixture.repository, ".contextbridge", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte("not a manifest\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"adopt", fixture.repository, "--dry-run", "--json"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stdout.String(), reasonManifestConflict) {
		t.Fatalf("manifest conflict was not fail-closed: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestUpgradeV1DryRunApplyAndRepeatedNoOp(t *testing.T) {
	fixture := newHandoffFixture(t)
	manifestPath := filepath.Join(fixture.repository, ".contextbridge", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	v1 := `{"schema_version":1,"generator":{"name":"contextbridge","version":"0.1.0"},"project":{"name":"Pilot","repository":"ukyovfx/pilot","profile":"core"},"lifecycle":{"created_by_contextbridge":true,"local_only":false},"files":[]}`
	if err := os.WriteFile(manifestPath, []byte(v1), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	before, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"upgrade", fixture.repository, "--dry-run", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("upgrade dry-run failed: %s", stderr.String())
	}
	if after, readErr := os.ReadFile(manifestPath); readErr != nil || string(after) != string(before) || fileExists(filepath.Join(fixture.repository, ".contextbridge", "manifest.json.v1.bak")) {
		t.Fatal("upgrade dry-run mutated migration files")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"upgrade", fixture.repository, "--confirm", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("upgrade apply failed: %s", stderr.String())
	}
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := core.ParseManifest(data)
	if err != nil || manifest.SchemaVersion != 2 || !fileExists(filepath.Join(fixture.repository, ".contextbridge", "manifest.json.v1.bak")) {
		t.Fatalf("upgrade did not create valid V2 with backup: %v", err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"upgrade", fixture.repository, "--json"}, &stdout, &stderr, "test"); code != 0 || !strings.Contains(stdout.String(), reasonAlreadyUpgraded) {
		t.Fatalf("repeated upgrade did not report ALREADY_UPGRADED: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestUpgradeRefusesUnknownNewerManifest(t *testing.T) {
	fixture := newHandoffFixture(t)
	manifestPath := filepath.Join(fixture.repository, ".contextbridge", "manifest.json")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"schema_version":99}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"upgrade", fixture.repository, "--dry-run", "--json"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stdout.String(), reasonUnknownNewerManifest) {
		t.Fatalf("unknown newer manifest was not refused: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestRebindRequiresConfirmationAndChangesOnlyRegistry(t *testing.T) {
	fixture := newHandoffFixture(t)
	oldPath := fixture.repository
	newPath := filepath.Join(filepath.Dir(oldPath), "pilot-moved")
	if err := os.Rename(oldPath, newPath); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	before := directorySnapshot(t, newPath)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rebind", fixture.project.ID, "--workspace", newPath, "--dry-run", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("rebind dry-run failed: %s", stderr.String())
	}
	if fileExists(filepath.Join(fixture.registryHome, "registry-v1.json.bak")) || before != directorySnapshot(t, newPath) {
		t.Fatal("rebind dry-run mutated state")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"rebind", fixture.project.ID, "--workspace", newPath, "--json"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stdout.String(), reasonConfirmationRequired) {
		t.Fatalf("rebind without confirmation was not stopped: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"rebind", fixture.project.ID, "--workspace", newPath, "--confirm", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("rebind apply failed: %s", stderr.String())
	}
	value, err := (registry.Store{Home: fixture.registryHome}).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Workspaces) != 1 || !equivalentTestPath(t, value.Workspaces[0].Path, newPath) || before != directorySnapshot(t, newPath) {
		t.Fatalf("rebind changed unexpected state: %#v", value.Workspaces)
	}
}

func TestRebindRejectsUnrelatedRepository(t *testing.T) {
	fixture := newHandoffFixture(t)
	other := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, other, "init", "-b", "main")
	runTestGit(t, other, "remote", "add", "origin", "https://github.com/other/unrelated.git")
	if err := os.WriteFile(filepath.Join(other, "file.txt"), []byte("other\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runTestGit(t, other, "add", "file.txt")
	runTestGit(t, other, "commit", "-m", "other")
	if err := os.RemoveAll(fixture.repository); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"rebind", fixture.project.ID, "--workspace", other, "--dry-run", "--json"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stdout.String(), reasonRebindIdentityMismatch) {
		t.Fatalf("unrelated repository was not rejected: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestRevalidateRepositoryRejectsIdentityChange(t *testing.T) {
	fixture := newHandoffFixture(t)
	probe, err := (workspace.Prober{}).Probe(fixture.repository)
	if err != nil {
		t.Fatal(err)
	}
	runTestGit(t, fixture.repository, "remote", "set-url", "origin", "https://github.com/other/changed.git")
	if err := revalidateRepository(fixture.repository, probe.Fingerprint); err == nil || !strings.Contains(err.Error(), reasonIdentityChangedAfterPlan) {
		t.Fatalf("identity change was not rejected: %v", err)
	}
}

func TestMigrationProbeDistinguishesInaccessibleGitFromNonGit(t *testing.T) {
	target := filepath.Join(t.TempDir(), "untrusted")
	if err := os.MkdirAll(filepath.Join(target, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := probeMigrationTarget(target)
	if err == nil || err.Error() != reasonGitRepositoryInaccessible {
		t.Fatalf("expected inaccessible Git reason, got %v", err)
	}
	nonGit := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(nonGit, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err = probeMigrationTarget(nonGit)
	if err == nil || err.Error() != reasonTargetNotGit {
		t.Fatalf("expected non-Git reason, got %v", err)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
