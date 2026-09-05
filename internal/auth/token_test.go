package auth

import (
	"bytes"
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
