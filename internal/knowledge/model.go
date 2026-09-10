package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	SchemaVersion      = 1
	ClassNone          = "NONE"
	ClassActive        = "ACTIVE"
	ClassDurableRecord = "DURABLE_RECORD"
	ClassAcceptedState = "ACCEPTED_STATE"
	RecordDecision     = "decision"
	RecordAudit        = "audit"
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\b(ghp|github_pat|sk)-[A-Za-z0-9_-]{12,}`),
	regexp.MustCompile(`(?i)\b(password|passwd|token|secret|api[_-]?key)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)https?://[^/\s:@]+:[^/\s@]+@`),
}

var proposalIDPattern = regexp.MustCompile(`^wb_[0-9a-f]{24}$`)

type Proposal struct {
	SchemaVersion           int      `json:"schema_version"`
	ID                      string   `json:"id"`
	ProjectID               string   `json:"project_id"`
	ProjectName             string   `json:"project_name"`
	RepositoryID            string   `json:"repository_id"`
	WorkspaceID             string   `json:"workspace_id"`
	WorkspacePath           string   `json:"workspace_path"`
	RequestedClass          string   `json:"requested_class"`
	EffectiveClass          string   `json:"effective_class"`
	Reason                  string   `json:"reason,omitempty"`
	Destination             string   `json:"destination,omitempty"`
	RecordKind              string   `json:"record_kind,omitempty"`
	Summary                 string   `json:"summary,omitempty"`
	Status                  string   `json:"status,omitempty"`
	Blocker                 string   `json:"blocker,omitempty"`
	NextAction              string   `json:"next_action,omitempty"`
	BasisBranch             string   `json:"basis_branch,omitempty"`
	BasisCommit             string   `json:"basis_commit,omitempty"`
	VerificationStatus      string   `json:"verification_status"`
	VerificationCommands    []string `json:"verification_commands,omitempty"`
	PreconditionFingerprint string   `json:"precondition_fingerprint"`
	Evidence                []string `json:"evidence"`
}

func NormalizeClass(value string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case ClassNone:
		return ClassNone, nil
	case ClassActive:
		return ClassActive, nil
	case ClassDurableRecord:
		return ClassDurableRecord, nil
	case ClassAcceptedState:
		return ClassAcceptedState, nil
	default:
		return "", fmt.Errorf("writeback class must be none, active, durable_record, or accepted_state")
	}
}

func NormalizeRecordKind(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case RecordDecision, RecordAudit:
		return strings.ToLower(strings.TrimSpace(value)), nil
	default:
		return "", errors.New("record kind must be decision or audit")
	}
}

func ValidateText(values ...string) error {
	for _, value := range values {
		for _, pattern := range sensitivePatterns {
			if pattern.MatchString(value) {
				return errors.New("write-back content resembles a secret or credential-bearing value")
			}
		}
	}
	return nil
}

func ValidateProposal(proposal Proposal) error {
	if proposal.SchemaVersion != SchemaVersion {
		return errors.New("unsupported write-back proposal schema")
	}
	if !proposalIDPattern.MatchString(proposal.ID) || proposal.ProjectID == "" || proposal.RepositoryID == "" || proposal.WorkspaceID == "" || proposal.WorkspacePath == "" {
		return errors.New("write-back proposal identity is incomplete")
	}
	requested, err := NormalizeClass(proposal.RequestedClass)
	if err != nil {
		return err
	}
	effective, err := NormalizeClass(proposal.EffectiveClass)
	if err != nil {
		return err
	}
	if requested == ClassNone && effective != ClassNone {
		return errors.New("NONE cannot be upgraded to another write-back class")
	}
	if proposal.ID != DeterministicID(proposal.ProjectID, requested, proposal.RecordKind, proposal.Summary, proposal.Status, proposal.Blocker, proposal.NextAction) {
		return errors.New("write-back proposal ID is not deterministic")
	}
	if effective == ClassDurableRecord && proposal.RecordKind != RecordDecision && proposal.RecordKind != RecordAudit {
		return errors.New("durable record kind is invalid")
	}
	if effective != ClassNone && strings.TrimSpace(proposal.Summary) == "" {
		return errors.New("write-back summary is required")
	}
	if err := ValidateText(proposal.Reason, proposal.Summary, proposal.Status, proposal.Blocker, proposal.NextAction, strings.Join(proposal.Evidence, "\n")); err != nil {
		return err
	}
	return nil
}

func DeterministicID(projectID, requestedClass, recordKind, summary, status, blocker, nextAction string) string {
	data := strings.Join([]string{projectID, requestedClass, recordKind, summary, status, blocker, nextAction}, "\x00")
	sum := sha256.Sum256([]byte(data))
	return "wb_" + hex.EncodeToString(sum[:12])
}

func MarshalProposal(proposal Proposal) ([]byte, error) {
	if err := ValidateProposal(proposal); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(proposal, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func UnmarshalProposal(data []byte) (Proposal, error) {
	var proposal Proposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		return Proposal{}, errors.New("write-back proposal JSON is invalid")
	}
	if err := ValidateProposal(proposal); err != nil {
		return Proposal{}, err
	}
	return proposal, nil
}

func Slug(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastDash := false
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			lastDash = false
		} else if !lastDash {
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return "knowledge"
	}
	if len(result) > 48 {
		result = strings.Trim(result[:48], "-")
	}
	return result
}
