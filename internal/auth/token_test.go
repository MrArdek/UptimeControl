package auth

import (
	"bytes"
	"regexp"
	"testing"
)

func TestSessionTokensAreRandomAndHashed(t *testing.T) {
	firstToken, firstHash, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() returned an error: %v", err)
	}

	secondToken, secondHash, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken() returned an error: %v", err)
	}

	if firstToken == secondToken || bytes.Equal(firstHash, secondHash) {
		t.Fatal("two generated sessions are identical")
	}

	if bytes.Equal([]byte(firstToken), firstHash) {
		t.Fatal("stored hash contains the raw session token")
	}

	if !bytes.Equal(firstHash, hashSessionToken(firstToken)) {
		t.Fatal("session token hash is not deterministic")
	}
}

func TestNewUUIDReturnsVersion4UUID(t *testing.T) {
	identifier, err := newUUID()
	if err != nil {
		t.Fatalf("newUUID() returned an error: %v", err)
	}

	pattern := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !pattern.MatchString(identifier) {
		t.Fatalf("newUUID() = %q, want a UUID v4", identifier)
	}
}
