package auth

import (
	"strings"
	"testing"
)

func TestPasswordHashRoundTrip(t *testing.T) {
	encodedHash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashPassword() returned an error: %v", err)
	}

	if !strings.HasPrefix(encodedHash, "$argon2id$") {
		t.Fatalf("hash prefix = %q, want argon2id", encodedHash)
	}

	matches, err := verifyPassword("correct horse battery staple", encodedHash)
	if err != nil {
		t.Fatalf("verifyPassword() returned an error: %v", err)
	}
	if !matches {
		t.Fatal("verifyPassword() = false for the correct password")
	}

	matches, err = verifyPassword("wrong password value", encodedHash)
	if err != nil {
		t.Fatalf("verifyPassword() returned an error: %v", err)
	}
	if matches {
		t.Fatal("verifyPassword() = true for an incorrect password")
	}
}

func TestVerifyPasswordRejectsMalformedHash(t *testing.T) {
	malformedHashes := []string{
		"not-an-argon-hash",
		"$argon2id$v=19extra$m=65536,t=3,p=2$c2FsdHZhbHVlMTIzNA$a2V5dmFsdWUxMjM0NTY3OA",
		"$argon2id$v=19$m=65536,t=3,p=2extra$c2FsdHZhbHVlMTIzNA$a2V5dmFsdWUxMjM0NTY3OA",
	}

	for _, malformedHash := range malformedHashes {
		if _, err := verifyPassword("password value", malformedHash); err == nil {
			t.Fatalf("verifyPassword() returned no error for malformed hash %q", malformedHash)
		}
	}
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "valid", password: "long password value"},
		{name: "too short", password: "short", wantErr: true},
		{name: "surrounding space", password: " password value ", wantErr: true},
		{name: "too long", password: strings.Repeat("a", maximumPasswordBytes+1), wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validatePassword(test.password)
			if (err != nil) != test.wantErr {
				t.Fatalf("validatePassword() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
