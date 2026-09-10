package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type TelegramNotifier struct {
	endpoint string
	chatID   string
	client   *http.Client
}

func NewTelegramNotifier(botToken, chatID string) *TelegramNotifier {
	return &TelegramNotifier{
		endpoint: "https://api.telegram.org/bot" + botToken + "/sendMessage",
		chatID:   chatID,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (notifier *TelegramNotifier) Notify(ctx context.Context, transition Transition) error {
	message := transitionMessage(transition)
	form := url.Values{}
	form.Set("chat_id", notifier.chatID)
	form.Set("text", message)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, notifier.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("create Telegram request")
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := notifier.client.Do(request)
	if err != nil {
		// net/http errors may include the request URL, which contains the bot token.
		return fmt.Errorf("Telegram request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Telegram returned status %d", response.StatusCode)
	}
	return nil
}

func transitionMessage(transition Transition) string {
	icon := "🔴"
	title := "Недоступен"
	if transition.Kind == "recovered" {
		icon = "🟢"
		title = "Восстановлен"
	} else if transition.Kind == "test" {
		icon = "🔵"
		title = "Тест уведомлений"
	}
	lines := []string{
		icon + " Uptime Control: " + title,
		"Проект: " + transition.ProjectName,
		"Проверка: " + transition.MonitorName,
	}
	if transition.URL != "" {
		lines = append(lines, "Адрес: "+transition.URL)
	} else if transition.Target != "" {
		lines = append(lines, "Адрес: "+transition.Target)
	}
	if transition.Cause != "" && transition.Kind == "down" {
		lines = append(lines, "Причина: "+transition.Cause)
	}
	lines = append(lines, "Время UTC: "+transition.OccurredAt.UTC().Format(time.RFC3339))
	if transition.EventID != "" {
		lines = append(lines, "Event ID: "+transition.EventID)
	}
	return strings.Join(lines, "\n")
}
