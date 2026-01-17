package core

import "testing"

func TestNewULID(t *testing.T) {
	id1 := NewULID()
	id2 := NewULID()

	if id1.String() == "" {
		t.Error("ULID should not be empty")
	}

	if id1.String() == id2.String() {
		t.Error("Two ULIDs should be different")
	}

	// ULIDs should be lexicographically sortable by time
	if id1.String() > id2.String() {
		t.Error("Later ULID should sort after earlier ULID")
	}
}

func TestParseULID(t *testing.T) {
	original := NewULID()
	parsed, err := ParseULID(original.String())
	if err != nil {
		t.Fatalf("ParseULID failed: %v", err)
	}
	if parsed != original {
		t.Errorf("Parsed ULID %v != original %v", parsed, original)
	}
}

func TestParseULID_Invalid(t *testing.T) {
	_, err := ParseULID("invalid")
	if err == nil {
		t.Error("ParseULID should fail on invalid input")
	}
}
