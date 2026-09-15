package codexbootstrap

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPlanAndApplyCreatesGlobalRouterWithoutMutatingDryRun(t *testing.T) {
	codexHome := filepath.Join(t.TempDir(), "codex")
	t.Setenv("CODEX_HOME", codexHome)
	plan, err := PlanBootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != "create" || !plan.CreateDirectory {
		t.Fatalf("unexpected create plan: %+v", plan)
	}
	if _, err := os.Stat(codexHome); !os.IsNotExist(err) {
		t.Fatalf("planning created Codex home: %v", err)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(codexHome, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != ManagedBlock() {
		t.Fatalf("unexpected managed file: %q", data)
	}
	idempotent, err := PlanBootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if idempotent.Action != "none" {
		t.Fatalf("second plan was not idempotent: %+v", idempotent)
	}
}

func TestBootstrapPreservesExistingContentAndOnlyReplacesManagedBlock(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	original := "# Personal instructions\n\nKeep this exactly.\n"
	path := filepath.Join(codexHome, "AGENTS.md")
	if err := os.WriteFile(path, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanBootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != "update" || !strings.HasPrefix(plan.Content, original) {
		t.Fatalf("unexpected additive plan: %+v", plan)
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), original) || !strings.Contains(string(data), BeginMarker) {
		t.Fatalf("existing content was not preserved: %q", data)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("existing file mode changed: %o", info.Mode().Perm())
	}
	second, err := PlanBootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if second.Action != "none" {
		t.Fatalf("managed block was not stable: %+v", second)
	}
}

func TestBootstrapBlocksOverrideAndMalformedManagedContent(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	override := filepath.Join(codexHome, "AGENTS.override.md")
	if err := os.WriteFile(override, []byte("# Temporary override\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanBootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != "blocked" || !strings.Contains(strings.Join(plan.Warnings, " "), "OVERRIDE") {
		t.Fatalf("override was not blocked: %+v", plan)
	}
	if err := os.WriteFile(override, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "AGENTS.md"), []byte(BeginMarker+"\npartial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err = PlanBootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if plan.Action != "blocked" {
		t.Fatalf("malformed block was not blocked: %+v", plan)
	}
}

func TestApplyRefusesStaleBootstrapPlan(t *testing.T) {
	codexHome := t.TempDir()
	t.Setenv("CODEX_HOME", codexHome)
	plan, err := PlanBootstrap()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(codexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "AGENTS.md"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Apply(plan); err == nil {
		t.Fatal("stale plan was applied")
	}
}
