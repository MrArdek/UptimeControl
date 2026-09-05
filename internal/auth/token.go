package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const sessionTokenLength = 32

func newSessionToken() (string, []byte, error) {
	randomBytes := make([]byte, sessionTokenLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", nil, fmt.Errorf("read random bytes: %w", err)
	}

	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	return token, hashSessionToken(token), nil
}

func hashSessionToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}
