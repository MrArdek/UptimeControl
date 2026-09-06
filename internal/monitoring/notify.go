package monitoring

import (
	"context"
	"strings"
)

// MultiNotifier fans a transition out to every configured notifier.
// A failure in one notifier never prevents delivery to the others, so the
// pre-existing Telegram delivery keeps working when webhooks are added.
type MultiNotifier struct {
	notifiers []Notifier
}

func NewMultiNotifier(notifiers ...Notifier) *MultiNotifier {
	kept := make([]Notifier, 0, len(notifiers))
	for _, notifier := range notifiers {
		if notifier != nil {
			kept = append(kept, notifier)
		}
	}

	return &MultiNotifier{notifiers: kept}
}

func (notifier *MultiNotifier) Notify(ctx context.Context, transition Transition) error {
	var failures []string
	for _, nested := range notifier.notifiers {
		if err := nested.Notify(ctx, transition); err != nil {
			failures = append(failures, err.Error())
		}
	}
	if len(failures) > 0 {
		return &multiNotifyError{joined: strings.Join(failures, "; ")}
	}

	return nil
}

type multiNotifyError struct {
	joined string
}

func (err *multiNotifyError) Error() string {
	return "one or more notifiers failed: " + err.joined
}
