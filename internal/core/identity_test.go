package core

import "testing"

func TestNormalizeRemoteTransportEquivalence(t *testing.T) {
	values := []string{
		"https://User:secret@GitHub.com/ukyovfx/Context-Bridge.git",
		"git@github.com:ukyovfx/Context-Bridge.git",
		"ssh://git@GITHUB.COM:22/ukyovfx/Context-Bridge.git",
	}
	var expected RemoteIdentity
	for i, value := range values {
		identity, err := NormalizeRemote(value)
		if err != nil {
			t.Fatalf("normalize %q: %v", value, err)
		}
		if i == 0 {
			expected = identity
		} else if !expected.Equal(identity) {
			t.Fatalf("transport identities differ: %#v %#v", expected, identity)
		}
	}
	if expected.Key() != "github.com/ukyovfx/Context-Bridge" {
		t.Fatalf("unexpected key %q", expected.Key())
	}
}

func TestNormalizeRemotePreservesHostPortAndPathIdentity(t *testing.T) {
	base, _ := NormalizeRemote("https://github.com/ukyovfx/Context-Bridge.git")
	spoof, _ := NormalizeRemote("https://evil.example/ukyovfx/Context-Bridge.git")
	caseVariant, _ := NormalizeRemote("https://github.com/ukyovfx/context-bridge.git")
	port, _ := NormalizeRemote("ssh://git@github.com:2222/ukyovfx/Context-Bridge.git")
	if base.Equal(spoof) {
		t.Fatal("host-spoof remote collapsed into GitHub identity")
	}
	if base.Equal(caseVariant) {
		t.Fatal("generic normalization did not preserve path case")
	}
	if port.Port != "2222" || base.Equal(port) {
		t.Fatal("meaningful non-default port was not preserved")
	}
}

func TestNormalizeRemoteRejectsUnsupportedOrUnsafeValues(t *testing.T) {
	for _, value := range []string{"http://github.com/a/b", "file:///tmp/repo", "C:\\repo", "git@github.com:a/../b"} {
		if _, err := NormalizeRemote(value); err == nil {
			t.Fatalf("expected rejection for %q", value)
		}
	}
}

func TestRemoteIdentityValidationRejectsNonNormalizedStoredValues(t *testing.T) {
	for _, value := range []RemoteIdentity{
		{Host: "GitHub.com", Path: "a/b"},
		{Host: "github.com", Port: "70000", Path: "a/b"},
		{Host: "github.com", Path: "a/b.git"},
		{Host: "github.com", Path: "a/../b"},
	} {
		if err := value.Validate(); err == nil {
			t.Fatalf("invalid stored identity accepted: %#v", value)
		}
	}
}

func TestValidateEntityIDs(t *testing.T) {
	valid := map[string]string{
		ProjectIDPrefix:    "prj_11111111-1111-4111-8111-111111111111",
		RepositoryIDPrefix: "repo_22222222-2222-4222-8222-222222222222",
		WorkspaceIDPrefix:  "ws_33333333-3333-4333-8333-333333333333",
	}
	for prefix, value := range valid {
		if err := ValidateID(value, prefix); err != nil {
			t.Fatal(err)
		}
	}
	if err := ValidateID("prj_not-a-uuid", ProjectIDPrefix); err == nil {
		t.Fatal("invalid UUID was accepted")
	}
}
