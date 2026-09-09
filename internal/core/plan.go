package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type OperationKind string

const (
	CreateDirectory      OperationKind = "create_directory"
	WriteFile            OperationKind = "write_file"
	InitializeGit        OperationKind = "initialize_git"
	StageGitFiles        OperationKind = "stage_git_files"
	CommitGitFiles       OperationKind = "commit_git_files"
	CreatePrivateAndPush OperationKind = "create_private_github_repository_and_push"
)

type Operation struct {
	Kind    OperationKind
	Path    string
	Content string
	Args    []string
}

type Plan struct {
	target     string
	operations []Operation
}

func NewPlan(target string, operations []Operation) Plan {
	return Plan{target: target, operations: cloneOperations(operations)}
}

func (p Plan) Target() string { return p.target }

func (p Plan) Operations() []Operation { return cloneOperations(p.operations) }

func cloneOperations(in []Operation) []Operation {
	out := make([]Operation, len(in))
	for i, op := range in {
		out[i] = op
		out[i].Args = append([]string(nil), op.Args...)
	}
	return out
}

type OperationSummary struct {
	Kind   OperationKind `json:"kind"`
	Path   string        `json:"path,omitempty"`
	Args   []string      `json:"args,omitempty"`
	Size   int           `json:"size,omitempty"`
	SHA256 string        `json:"sha256,omitempty"`
}

type PlanSummary struct {
	Target     string             `json:"target"`
	Operations []OperationSummary `json:"operations"`
}

func (p Plan) JSON() ([]byte, error) {
	summary := PlanSummary{Target: p.target}
	for _, op := range p.operations {
		item := OperationSummary{Kind: op.Kind, Path: op.Path, Args: append([]string(nil), op.Args...)}
		if op.Kind == WriteFile {
			sum := sha256.Sum256([]byte(op.Content))
			item.Size = len(op.Content)
			item.SHA256 = hex.EncodeToString(sum[:])
		}
		summary.Operations = append(summary.Operations, item)
	}
	b, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal plan: %w", err)
	}
	return append(b, '\n'), nil
}
