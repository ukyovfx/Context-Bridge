package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if !strings.Contains(stderr.String(), "stale CURRENT-STATE") {
		t.Fatalf("unexpected diagnostic: %s", stderr.String())
	}
}
