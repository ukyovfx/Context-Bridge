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
)

func TestChatGPTBootstrapIsDeterministicPortableAndReadOnly(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	before := directorySnapshot(t, fixture.repository)
	var first, stderr bytes.Buffer
	if code := Run([]string{"bootstrap", "chatgpt", "pilot"}, &first, &stderr, "test"); code != 0 {
		t.Fatalf("bootstrap failed: %s", stderr.String())
	}
	if code := Run([]string{"bootstrap", "chatgpt", fixture.project.ID, "--json"}, &bytes.Buffer{}, &stderr, "test"); code != 0 {
		t.Fatalf("JSON bootstrap failed: %s", stderr.String())
	}
	var second bytes.Buffer
	if code := Run([]string{"bootstrap", "chatgpt", "pilot"}, &second, &stderr, "test"); code != 0 || first.String() != second.String() {
		t.Fatalf("bootstrap output was not deterministic: %d\n%s\n%s", code, first.String(), second.String())
	}
	if before != directorySnapshot(t, fixture.repository) {
		t.Fatal("bootstrap mutated the repository")
	}
	output := first.String()
	if !strings.Contains(output, "Project resolved: Pilot") || !strings.Contains(output, "Guard: PASS") || !strings.Contains(output, "GitHub remote: AVAILABLE") || !strings.Contains(output, "relevant code, tests, CI") {
		t.Fatalf("readiness or portable guidance missing: %s", output)
	}
	if strings.Contains(output, fixture.repository) || strings.Contains(output, fixture.registryHome) || strings.Contains(output, "secret@") {
		t.Fatalf("bootstrap leaked local or credential-bearing path data: %s", output)
	}
	if len(output) > 6000 {
		t.Fatalf("bootstrap is too large to paste directly: %d bytes", len(output))
	}
	if !strings.Contains(output, core.ChatGPTProjectInstructions("Pilot")) {
		t.Fatalf("bootstrap does not reuse the project template")
	}
}

func TestChatGPTBootstrapReportsGuardFailureAndMissingCanonicalWorkspace(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	runTestGit(t, fixture.repository, "checkout", "-b", "wrong-branch")
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"bootstrap", "chatgpt", "pilot", "--json"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stdout.String(), "WRONG_WORKSPACE") {
		t.Fatalf("Guard failure was not reported: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	runTestGit(t, fixture.repository, "checkout", "main")
	value, err := (registry.Store{Home: fixture.registryHome}).Load()
	if err != nil {
		t.Fatal(err)
	}
	value.Workspaces = nil
	if err := (registry.Store{Home: fixture.registryHome}).Save(value); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"bootstrap", "chatgpt", "pilot", "--json"}, &stdout, &stderr, "test"); code == 0 || !strings.Contains(stderr.String(), "canonical workspace") {
		t.Fatalf("missing canonical workspace was not refused: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestChatGPTBootstrapJSONShapeIsStable(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"bootstrap", "chatgpt", "pilot", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("JSON bootstrap failed: %s", stderr.String())
	}
	var result chatGPTBootstrap
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.ProjectResolved || result.Guard != "PASS" || result.BootstrapReadiness != "READY_WITH_WARNINGS" || result.Instructions == "" {
		t.Fatalf("unexpected bootstrap result: %#v", result)
	}
	if strings.Contains(result.Instructions, filepath.Dir(fixture.repository)) {
		t.Fatal("JSON instructions leaked a local path")
	}
}

func TestChatGPTBootstrapLocalOnlyWarnsWithoutRemoteClaims(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "registry")
	t.Setenv("CONTEXTBRIDGE_HOME", home)
	t.Setenv("GIT_AUTHOR_NAME", "Context Bridge Tests")
	t.Setenv("GIT_AUTHOR_EMAIL", "contextbridge-tests@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Context Bridge Tests")
	t.Setenv("GIT_COMMITTER_EMAIL", "contextbridge-tests@example.invalid")
	// The existing new-project flow supplies the registered local-only fixture.
	profileRoot := filepath.Join(root, "profile")
	for _, name := range []string{"active", "worktrees", "archive", "private"} {
		if err := os.MkdirAll(filepath.Join(profileRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeLocalProfile(t, home, map[string]any{"schema_version": 1, "workspace_root": profileRoot})
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"new", "Local"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("local-only project creation failed: %s", stderr.String())
	}
	stdout.Reset()
	if code := Run([]string{"bootstrap", "chatgpt", "Local", "--json"}, &stdout, &stderr, "test"); code == 0 {
		t.Fatalf("local-only bootstrap bypassed Guard: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "LOCAL_ONLY") || !strings.Contains(stdout.String(), "NOT_CONFIGURED") {
		t.Fatalf("local-only limitation warning missing: %s", stdout.String())
	}
}
