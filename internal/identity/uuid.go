package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// NewUUID returns a cryptographically random RFC 4122 version 4 UUID.
func NewUUID() (string, error) {
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

// ValidUUID reports whether value is a canonical UUID string.
func ValidUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}

	compact := value[0:8] + value[9:13] + value[14:18] + value[19:23] + value[24:36]
	decoded := make([]byte, 16)
	_, err := hex.Decode(decoded, []byte(compact))
	return err == nil
}
