package idgen

import (
	"testing"

	"github.com/ukyovfx/Context-Bridge/internal/core"
)

func TestNewGeneratesDistinctUUIDv4Identifiers(t *testing.T) {
	first, err := New(core.ProjectIDPrefix)
	if err != nil {
		t.Fatal(err)
	}
	second, err := New(core.ProjectIDPrefix)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("random identifiers unexpectedly match")
	}
	if err := core.ValidateID(first, core.ProjectIDPrefix); err != nil {
		t.Fatalf("generated identifier is invalid: %q: %v", first, err)
	}
}
