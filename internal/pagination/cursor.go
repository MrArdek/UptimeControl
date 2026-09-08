package pagination

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"
)

var ErrInvalidCursor = errors.New("invalid cursor")

type payload struct {
	Time  time.Time `json:"t"`
	ID    string    `json:"i"`
	Scope string    `json:"s"`
}

func Encode(timestamp time.Time, id, scope string) (string, error) {
	encoded, err := json.Marshal(payload{
		Time:  timestamp.UTC(),
		ID:    id,
		Scope: scopeHash(scope),
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func Decode(cursor, scope string) (time.Time, string, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return time.Time{}, "", ErrInvalidCursor
	}
	var value payload
	if err := json.Unmarshal(decoded, &value); err != nil || value.Time.IsZero() || value.ID == "" || value.Scope != scopeHash(scope) {
		return time.Time{}, "", ErrInvalidCursor
	}
	return value.Time.UTC(), value.ID, nil
}

func scopeHash(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return hex.EncodeToString(sum[:16])
}
