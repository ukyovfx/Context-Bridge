package apply

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/core"
)

type noOpRunner struct{}

func (noOpRunner) Run(string, string, ...string) error { return nil }

type rejectingVerifier struct{}

func (rejectingVerifier) Verify(core.GuardExpected) error {
	return core.WrongWorkspaceError{Reasons: []core.GuardReason{core.ReasonIdentityChangedAfterPlan}}
}

func TestGuardFailurePreventsFirstMutation(t *testing.T) {
	target := filepath.Join(t.TempDir(), "target")
	plan := core.NewGuardedPlan(target, []core.Operation{{Kind: core.CreateDirectory, Path: "."}}, core.GuardExpected{ProjectID: "prj_123e4567-e89b-42d3-a456-426614174000"})
	err := (Applier{Runner: noOpRunner{}, GuardVerifier: rejectingVerifier{}}).Apply(plan)
	if !core.IsWrongWorkspace(err) {
		t.Fatalf("expected WRONG_WORKSPACE, got %v", err)
	}
	if _, statErr := os.Lstat(target); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("guard failure mutated target: %v", statErr)
	}
}
