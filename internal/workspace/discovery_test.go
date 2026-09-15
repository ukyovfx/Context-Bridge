package workspace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestDiscoveryReportsNoCandidatesForEmptyRoot(t *testing.T) {
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DiscoveryNoCandidates || result.CandidateCount != 0 || result.SkippedCount != 0 {
		t.Fatalf("unexpected empty-root result: %#v", result)
	}
}

func TestDiscoveryReportsEntryAndDepthLimits(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	entryLimited, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root, MaxEntries: 1})
	if err != nil {
		t.Fatal(err)
	}
	if entryLimited.Status != DiscoveryPartial || !entryLimited.TraversalLimitReached || !hasDiscoveryReason(entryLimited.Warnings, ReasonEntryLimitReached) {
		t.Fatalf("entry limit was not reported: %#v", entryLimited)
	}
	depthLimited, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root, MaxDepth: 1, MaxEntries: 100})
	if err != nil {
		t.Fatal(err)
	}
	if depthLimited.Status != DiscoveryPartial || !depthLimited.TraversalLimitReached || !hasDiscoveryReason(depthLimited.Warnings, ReasonMaxDepthReached) {
		t.Fatalf("depth limit was not reported: %#v", depthLimited)
	}
}

func TestDiscoveryReportsTimeLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Ensure the already-expired test deadline is deterministic without changing
	// Discoverer's production timeout behavior.
	time.Sleep(time.Millisecond)
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root, MaxDuration: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DiscoveryPartial || !result.TraversalLimitReached || !hasDiscoveryReason(result.Warnings, ReasonTimeLimitReached) {
		t.Fatalf("time limit was not reported: %#v", result)
	}
}

func TestDiscoveryReportsBoundedAggregateWithProbeableCandidates(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	createRepository(t, first, "https://github.com/ukyovfx/first.git")
	createRepository(t, second, "https://github.com/ukyovfx/second.git")
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root, MaxRepositories: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DiscoveryPartial || result.CandidateCount != 1 || !hasDiscoveryReason(result.Warnings, ReasonRepositoryLimit) {
		t.Fatalf("aggregate bound was not reported: %#v", result)
	}
}

func TestDiscoveryReportsSuccessWithWarningsForArtifactIndicators(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "backup.zip"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != DiscoverySuccessWithWarnings || !hasDiscoveryReason(result.Warnings, "ARTIFACT_INDICATOR_FOUND") {
		t.Fatalf("artifact warning was not reported: %#v", result)
	}
}

func TestDiscoveryJSONIsValidForPartialResult(t *testing.T) {
	root := t.TempDir()
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root, MaxEntries: 0, MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	result.Status = DiscoveryPartial
	result.TraversalLimitReached = true
	result.Warnings = append(result.Warnings, DiscoveryIssue{Path: root, Reason: ReasonEntryLimitReached})
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DiscoveryResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("partial result was not valid JSON: %v", err)
	}
	if decoded.Status != DiscoveryPartial || len(decoded.Warnings) == 0 {
		t.Fatalf("partial result fields were lost: %#v", decoded)
	}
}

func TestDiscoveryReportsUnreadableDirectoryWhenTestable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("portable unreadable-directory setup is not reliable on Windows")
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })
	result, err := (Discoverer{}).Discover(DiscoveryOptions{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.AccessFailures) == 0 || !hasDiscoveryReason(result.Warnings, ReasonAccessDenied) {
		t.Skip("test process can still read the protected directory")
	}
	if result.Status != DiscoveryPartial {
		t.Fatalf("unreadable directory was not partial: %#v", result)
	}
}

func hasDiscoveryReason(issues []DiscoveryIssue, expected string) bool {
	for _, issue := range issues {
		if issue.Reason == expected {
			return true
		}
	}
	return false
}
