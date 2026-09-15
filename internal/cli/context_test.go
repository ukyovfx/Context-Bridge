package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorReportsChangedInstructionPath(t *testing.T) {
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("Run tests.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run([]string{"doctor", root}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("doctor exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "finding: AGENTS.md UNEXPECTED_INSTRUCTION_CHANGE") {
		t.Fatalf("doctor output did not include changed instruction path: %s", stdout.String())
	}
}

func TestV1ContextCommandsPreviewBeforeApplyAndDoctorReadOnly(t *testing.T) {
	root := t.TempDir()
	codexHome := filepath.Join(t.TempDir(), "codex-home")
	t.Setenv("CODEX_HOME", codexHome)
	before := cliSnapshot(t, root)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"inspect", root}, &stdout, &stderr, "test"); code != 0 || !strings.Contains(stdout.String(), "mode: greenfield") {
		t.Fatalf("inspect failed: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"preview", root}, &stdout, &stderr, "test"); code != 0 || !strings.Contains(stdout.String(), "+++ AGENTS.md") {
		t.Fatalf("preview failed: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if after := cliSnapshot(t, root); after != before {
		t.Fatal("preview changed the target")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"apply", root}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("apply did not require confirmation")
	}
	if after := cliSnapshot(t, root); after != before {
		t.Fatal("unconfirmed apply changed the target")
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"apply", root, "--confirm"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("confirmed apply failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"inspect", root}, &stdout, &stderr, "test"); code != 0 || !strings.Contains(stdout.String(), "codex integration: READY") {
		t.Fatalf("Codex readiness was not reported: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if _, err := os.Stat(codexHome); !os.IsNotExist(err) {
		t.Fatalf("inspect created or touched CODEX_HOME: %v", err)
	}
	proposalOutput := bytes.Buffer{}
	if code := Run([]string{"preview", root}, &proposalOutput, &stderr, "test"); code != 0 || !strings.Contains(proposalOutput.String(), "no changes needed") {
		t.Fatalf("repeated preview was not idempotent: code=%d stderr=%s stdout=%s", code, stderr.String(), proposalOutput.String())
	}
	before = cliSnapshot(t, root)
	stdout.Reset()
	stderr.Reset()
	if code := Run([]string{"doctor", root}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("doctor failed: %s", stderr.String())
	}
	if after := cliSnapshot(t, root); after != before {
		t.Fatal("doctor changed the target")
	}
}

func TestSelfUpgradeCheckIsZeroMutation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"upgrade", "--check"}, &stdout, &stderr, "test-version"); code != 0 || !strings.Contains(stdout.String(), "zero mutations") {
		t.Fatalf("self-upgrade check failed: code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
}

func cliSnapshot(t *testing.T, root string) string {
	t.Helper()
	entries := []string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		entries = append(entries, filepath.ToSlash(path)+":"+string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(entries, "|")
}
