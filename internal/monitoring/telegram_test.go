package monitoring

import (
	"strings"
	"testing"
	"time"
)

func TestTransitionMessage(t *testing.T) {
	message := transitionMessage(Transition{
		Kind:        "down",
		ProjectName: "Telegram bot",
		MonitorName: "Worker",
		Cause:       "heartbeat overdue",
		OccurredAt:  time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	})
	for _, expected := range []string{"Недоступен", "Telegram bot", "Worker", "heartbeat overdue"} {
		if !strings.Contains(message, expected) {
			t.Fatalf("message %q does not contain %q", message, expected)
		}
	}
}
