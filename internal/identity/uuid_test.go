package identity

import (
	"regexp"
	"testing"
)

func TestNewUUIDReturnsVersion4UUID(t *testing.T) {
	identifier, err := NewUUID()
	if err != nil {
		t.Fatalf("NewUUID() returned an error: %v", err)
	}

	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(identifier) {
		t.Fatalf("NewUUID() = %q, want a UUID v4", identifier)
	}

	if !ValidUUID(identifier) {
		t.Fatalf("ValidUUID(%q) = false", identifier)
	}
}

func TestValidUUIDRejectsInvalidValues(t *testing.T) {
	invalidValues := []string{
		"",
		"not-a-uuid",
		"00000000-0000-0000-0000-00000000000z",
		"000000000000-0000-0000-000000000000",
	}

	for _, value := range invalidValues {
		if ValidUUID(value) {
			t.Fatalf("ValidUUID(%q) = true, want false", value)
		}
	}
}
