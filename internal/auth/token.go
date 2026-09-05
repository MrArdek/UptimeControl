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

func newUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}

	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80

	return fmt.Sprintf(
		"%08x-%04x-%04x-%04x-%012x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}
