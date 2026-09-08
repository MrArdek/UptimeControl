package pagination

import (
	"errors"
	"testing"
	"time"
)

func TestCursorRoundTripAndScope(t *testing.T) {
	wantTime := time.Date(2026, 9, 8, 12, 30, 0, 0, time.FixedZone("test", 2*60*60))
	wantID := "11111111-1111-4111-8111-111111111111"
	cursor, err := Encode(wantTime, wantID, "checks:monitor-a")
	if err != nil {
		t.Fatalf("Encode(): %v", err)
	}
	gotTime, gotID, err := Decode(cursor, "checks:monitor-a")
	if err != nil {
		t.Fatalf("Decode(): %v", err)
	}
	if !gotTime.Equal(wantTime) || gotID != wantID {
		t.Fatalf("decoded (%s, %q), want (%s, %q)", gotTime, gotID, wantTime, wantID)
	}
	if _, _, err := Decode(cursor, "checks:monitor-b"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("scope mismatch error = %v", err)
	}
}

func TestCursorRejectsInvalidValue(t *testing.T) {
	if _, _, err := Decode("not-base64", "scope"); !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("Decode() error = %v", err)
	}
}
