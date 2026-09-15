package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var discordHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return fmt.Errorf("redirects are not allowed")
	},
}

// ValidateDiscordWebhookURL rejects non-Discord hosts (SSRF).
func ValidateDiscordWebhookURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("webhook URL is required")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid webhook URL")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("webhook URL must use https")
	}
	if u.User != nil {
		return fmt.Errorf("webhook URL must not include credentials")
	}
	host := strings.ToLower(u.Hostname())
	if host != "discord.com" && host != "discordapp.com" {
		return fmt.Errorf("webhook URL host is not allowed")
	}
	path := strings.ToLower(u.EscapedPath())
	if !strings.HasPrefix(path, "/api/webhooks/") {
		return fmt.Errorf("webhook URL path is not a Discord webhook")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("webhook URL must not include query or fragment")
	}
	return nil
}

func postDiscordWebhook(webhookURL, content string) error {
	if err := ValidateDiscordWebhookURL(webhookURL); err != nil {
		return err
	}
	content = prepareContent(content)
	if content == "" {
		return fmt.Errorf("message is empty")
	}
	body, err := json.Marshal(map[string]string{"content": content})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := discordHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("discord webhook HTTP %d", resp.StatusCode)
	}
	return nil
}
