package knowledge

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNoneProposalIsValidAndDeterministic(t *testing.T) {
	proposal := Proposal{
		SchemaVersion: SchemaVersion, ID: DeterministicID("prj_test", ClassNone, RecordDecision, "", "", "", ""),
		ProjectID: "prj_test", ProjectName: "Demo", RepositoryID: "repo_test", WorkspaceID: "ws_test",
		WorkspacePath: `C:\work\demo`, RequestedClass: ClassNone, EffectiveClass: ClassNone, RecordKind: RecordDecision,
		VerificationStatus: "NOT_REQUIRED", PreconditionFingerprint: "fingerprint", Evidence: []string{"fresh_git_probe"},
	}
	data, err := MarshalProposal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Proposal
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ID != proposal.ID || string(data) != string(mustMarshal(t, proposal)) {
		t.Fatal("proposal JSON was not deterministic")
	}
}

func TestValidateTextRejectsCredentialBearingContent(t *testing.T) {
	for _, value := range []string{"token=abc123", "https://user:password@example.invalid/repo", "-----BEGIN PRIVATE KEY-----"} {
		if err := ValidateText(value); err == nil {
			t.Fatalf("accepted secret-like value %q", value)
		}
	}
}

func TestValidateProposalRefusesNoneUpgrade(t *testing.T) {
	proposal := Proposal{SchemaVersion: SchemaVersion, ID: DeterministicID("prj_test", ClassNone, RecordDecision, "x", "", "", ""), ProjectID: "prj_test", RepositoryID: "repo_test", WorkspaceID: "ws_test", WorkspacePath: `C:\work`, RequestedClass: ClassNone, EffectiveClass: ClassActive, RecordKind: RecordDecision, Summary: "x"}
	if err := ValidateProposal(proposal); err == nil || !strings.Contains(err.Error(), "NONE") {
		t.Fatalf("expected NONE upgrade refusal, got %v", err)
	}
}

func mustMarshal(t *testing.T, proposal Proposal) []byte {
	t.Helper()
	data, err := MarshalProposal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
