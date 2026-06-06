// Package notify provides notification interfaces and implementations for
// alerting about new episodes of tracked anime.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/xvantz/animark/internal/core"
)

// Notification describes a new episode alert.
type Notification struct {
	Anime   *core.AnimeRecord `json:"anime"`
	Episode int               `json:"episode"`
	Title   string            `json:"title"`
	Message string            `json:"message"`
}

// Notifier sends notifications about new episodes.
type Notifier interface {
	// Name returns a human-friendly name for this notifier.
	Name() string
	// Notify sends a notification. May return an error if delivery fails.
	Notify(ctx context.Context, n *Notification) error
}

// ---- Log Notifier ----

// LogNotifier writes notifications to the application log.
type LogNotifier struct{}

func NewLogNotifier() *LogNotifier { return &LogNotifier{} }

func (n *LogNotifier) Name() string { return "log" }

func (n *LogNotifier) Notify(_ context.Context, notif *Notification) error {
	log.Printf("[notify] %s", notif.Message)
	return nil
}

// ---- Discord Webhook Notifier ----

// DiscordWebhookNotifier sends notifications to a Discord channel via webhook.
type DiscordWebhookNotifier struct {
	webhookURL string
	httpClient *http.Client
	username   string
}

func NewDiscordWebhookNotifier(webhookURL string) *DiscordWebhookNotifier {
	return &DiscordWebhookNotifier{
		webhookURL: webhookURL,
		httpClient: &http.Client{},
		username:   "animark",
	}
}

func (n *DiscordWebhookNotifier) Name() string { return "discord" }

func (n *DiscordWebhookNotifier) Notify(ctx context.Context, notif *Notification) error {
	payload := map[string]interface{}{
		"username": n.username,
		"content":  notif.Message,
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(payload); err != nil {
		return fmt.Errorf("discord encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, &buf)
	if err != nil {
		return fmt.Errorf("discord request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("discord post: %w", err)
	}
	resp.Body.Close()
	return nil
}

// ---- Generic Webhook Notifier ----

// WebhookNotifier sends notifications as JSON POST to any URL.
type WebhookNotifier struct {
	url  string
	name string
}

func NewWebhookNotifier(name, url string) *WebhookNotifier {
	return &WebhookNotifier{
		url:  url,
		name: name,
	}
}

func (n *WebhookNotifier) Name() string { return n.name }

func (n *WebhookNotifier) Notify(ctx context.Context, notif *Notification) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(notif); err != nil {
		return fmt.Errorf("webhook encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, &buf)
	if err != nil {
		return fmt.Errorf("webhook request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook post: %w", err)
	}
	resp.Body.Close()
	return nil
}

// ---- Helpers ----

// FormatNotification creates a human-readable message from a notification.
func FormatNotification(n *Notification) string {
	title := n.Title
	if title == "" && n.Anime != nil {
		title = n.Anime.Title("en", "ja")
	}
	parts := []string{fmt.Sprintf("📺 %s", title)}
	if n.Episode > 0 {
		parts = append(parts, fmt.Sprintf("Episode %d is now airing!", n.Episode))
	} else {
		parts = append(parts, "New episode is now airing!")
	}
	if n.Anime != nil && n.Anime.URL != "" {
		parts = append(parts, n.Anime.URL)
	}
	return strings.Join(parts, "\n")
}
