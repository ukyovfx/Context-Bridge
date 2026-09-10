package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/knowledge"
)

func TestWritebackNonePlanAndApplyPerformZeroMutation(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	before := directorySnapshot(t, fixture.repository)
	var planned, stderr bytes.Buffer
	if code := Run([]string{"writeback", "plan", fixture.project.ID, "--class", "none", "--json"}, &planned, &stderr, "test"); code != 0 {
		t.Fatalf("NONE plan failed: %s", stderr.String())
	}
	var envelope writebackResult
	if err := json.Unmarshal(planned.Bytes(), &envelope); err != nil {
		t.Fatalf("invalid plan JSON: %v", err)
	}
	if strings.Contains(planned.String(), "https://") || strings.Contains(planned.String(), "secret") {
		t.Fatalf("plan JSON leaked remote credentials: %s", planned.String())
	}
	if envelope.Status != "planned" || envelope.Proposal == nil || envelope.Proposal.EffectiveClass != knowledge.ClassNone {
		t.Fatalf("unexpected NONE plan: %#v", envelope)
	}
	proposalPath := filepath.Join(t.TempDir(), "proposal.json")
	if err := os.WriteFile(proposalPath, planned.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	var applied bytes.Buffer
	if code := Run([]string{"writeback", "apply", "--proposal", proposalPath, "--json"}, &applied, &stderr, "test"); code != 0 {
		t.Fatalf("NONE apply failed: %s", stderr.String())
	}
	if !strings.Contains(applied.String(), `"status":"no_write"`) {
		t.Fatalf("unexpected NONE apply: %s", applied.String())
	}
	if after := directorySnapshot(t, fixture.repository); after != before {
		t.Fatalf("NONE write-back mutated the repository\nbefore=%s\nafter=%s", before, after)
	}
}

func TestWritebackPlanJSONIsDeterministic(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var first, second, stderr bytes.Buffer
	args := []string{"writeback", "plan", fixture.project.ID, "--class", "active", "--summary", "Deterministic note", "--json"}
	if code := Run(args, &first, &stderr, "test"); code != 0 {
		t.Fatalf("first plan failed: %s", stderr.String())
	}
	if code := Run(args, &second, &stderr, "test"); code != 0 {
		t.Fatalf("second plan failed: %s", stderr.String())
	}
	if first.String() != second.String() {
		t.Fatalf("plan JSON was not deterministic\nfirst=%s\nsecond=%s", first.String(), second.String())
	}
}

func TestWritebackApplyRejectsMalformedProposalWithJSON(t *testing.T) {
	proposalPath := filepath.Join(t.TempDir(), "malformed.json")
	if err := os.WriteFile(proposalPath, []byte(`{"schema_version":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"writeback", "apply", "--proposal", proposalPath, "--json"}, &stdout, &stderr, "test"); code == 0 {
		t.Fatal("malformed proposal unexpectedly applied")
	}
	var result struct {
		Status string `json:"status"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("malformed proposal output was not JSON: %v", err)
	}
	if result.Status != "failed" || result.Reason != "INVALID_PROPOSAL" {
		t.Fatalf("unexpected malformed proposal result: %#v", result)
	}
}

func TestWritebackActiveApplyWritesOnlyActivePlanWithoutGitMutation(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var planned, stderr bytes.Buffer
	if code := Run([]string{"writeback", "plan", "pilot", "--class", "active", "--summary", "Investigate pilot", "--json"}, &planned, &stderr, "test"); code != 0 {
		t.Fatalf("active plan failed: %s", stderr.String())
	}
	var envelope writebackResult
	if err := json.Unmarshal(planned.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	proposalPath := filepath.Join(t.TempDir(), "proposal.json")
	if err := os.WriteFile(proposalPath, planned.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	var applied bytes.Buffer
	if code := Run([]string{"writeback", "apply", "--proposal", proposalPath, "--json"}, &applied, &stderr, "test"); code != 0 {
		t.Fatalf("active apply failed: %s", stderr.String())
	}
	if !strings.Contains(applied.String(), `"status":"applied"`) {
		t.Fatalf("unexpected active apply: %s", applied.String())
	}
	if envelope.Proposal == nil || envelope.Proposal.Destination == "" {
		t.Fatal("active plan omitted destination")
	}
	content, err := os.ReadFile(filepath.Join(fixture.repository, filepath.FromSlash(envelope.Proposal.Destination)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "contextbridge_writeback_schema: 1") || !strings.Contains(string(content), "Investigate pilot") {
		t.Fatalf("active record content is incomplete: %s", content)
	}
	if _, err := os.Stat(filepath.Join(fixture.repository, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatalf("write-back left a Git lock: %v", err)
	}
	if strings.Contains(string(content), "https://") || strings.Contains(string(content), "secret") {
		t.Fatal("active record leaked remote credentials")
	}
}

func TestWritebackDurableRecordUsesOneCanonicalDestination(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var planned, stderr bytes.Buffer
	if code := Run([]string{"writeback", "plan", fixture.project.ID, "--class", "durable_record", "--kind", "audit", "--summary", "Pilot audit", "--json"}, &planned, &stderr, "test"); code != 0 {
		t.Fatalf("durable plan failed: %s", stderr.String())
	}
	var envelope writebackResult
	if err := json.Unmarshal(planned.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Proposal == nil || !strings.HasPrefix(filepath.ToSlash(envelope.Proposal.Destination), "docs/agent/audits/") {
		t.Fatalf("durable destination was not canonical: %#v", envelope.Proposal)
	}
	proposalPath := filepath.Join(t.TempDir(), "proposal.json")
	if err := os.WriteFile(proposalPath, planned.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	var applied bytes.Buffer
	if code := Run([]string{"writeback", "apply", "--proposal", proposalPath, "--json"}, &applied, &stderr, "test"); code != 0 {
		t.Fatalf("durable apply failed: %s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(fixture.repository, filepath.FromSlash(envelope.Proposal.Destination))); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fixture.repository, "docs", "agent", "audit")); !os.IsNotExist(err) {
		t.Fatalf("singular duplicate audit directory was created: %v", err)
	}
}

func TestAcceptedStatePlanDowngradesWhenVerificationDoesNotPass(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"writeback", "plan", fixture.project.ID, "--class", "accepted_state", "--summary", "Pilot state", "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("accepted-state plan failed: %s", stderr.String())
	}
	var result writebackResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Proposal == nil || result.Proposal.EffectiveClass != knowledge.ClassActive || !strings.HasPrefix(result.Proposal.Reason, writebackReasonAcceptedDowngraded) {
		t.Fatalf("accepted state was not downgraded: %#v", result.Proposal)
	}
}

func TestWritebackApplyAbortsWhenIdentityChanges(t *testing.T) {
	fixture := newHandoffFixture(t)
	t.Setenv("CONTEXTBRIDGE_HOME", fixture.registryHome)
	var planned, stderr bytes.Buffer
	if code := Run([]string{"writeback", "plan", fixture.project.ID, "--class", "active", "--summary", "Identity check", "--json"}, &planned, &stderr, "test"); code != 0 {
		t.Fatalf("plan failed: %s", stderr.String())
	}
	proposalPath := filepath.Join(t.TempDir(), "proposal.json")
	if err := os.WriteFile(proposalPath, planned.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.repository, "changed-after-plan.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var applied bytes.Buffer
	if code := Run([]string{"writeback", "apply", "--proposal", proposalPath, "--json"}, &applied, &stderr, "test"); code == 0 {
		t.Fatal("apply accepted changed identity")
	}
	if !strings.Contains(applied.String(), `"reason":"IDENTITY_CHANGED_AFTER_PLAN"`) {
		t.Fatalf("unexpected identity-change result: %s", applied.String())
	}
	if _, err := os.Stat(filepath.Join(fixture.repository, "docs", "agent", "plans", "active")); !os.IsNotExist(err) {
		t.Fatalf("identity abort created an active plan: %v", err)
	}
}

func TestKnowledgeDoctorIsReadOnlyAndMachineReadable(t *testing.T) {
	fixture := newHandoffFixture(t)
	before := directorySnapshot(t, fixture.repository)
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"knowledge", "doctor", "--project", fixture.repository, "--json"}, &stdout, &stderr, "test"); code != 0 {
		t.Fatalf("knowledge doctor failed: %s", stderr.String())
	}
	var result struct {
		Status string   `json:"status"`
		Issues []string `json:"issues"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("invalid doctor JSON: %v", err)
	}
	if result.Status != "WARNING" || len(result.Issues) == 0 {
		t.Fatalf("expected missing knowledge warnings: %#v", result)
	}
	if after := directorySnapshot(t, fixture.repository); after != before {
		t.Fatal("knowledge doctor mutated the repository")
	}
}
