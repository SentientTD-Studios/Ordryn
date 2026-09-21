package hooks

import (
	"net"
	"testing"

	"GoTodo/internal/extensions"
)

func TestValidateDiscordWebhookURL(t *testing.T) {
	ok := "https://discord.com/api/webhooks/123/abc"
	if err := ValidateDiscordWebhookURL(ok); err != nil {
		t.Fatal(err)
	}
	legacy := "https://discordapp.com/api/webhooks/123/abc"
	if err := ValidateDiscordWebhookURL(legacy); err != nil {
		t.Fatal(err)
	}
	canary := "https://canary.discord.com/api/webhooks/123/abc"
	if err := ValidateDiscordWebhookURL(canary); err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"",
		"http://discord.com/api/webhooks/123/abc",
		"https://example.com/api/webhooks/123/abc",
		"https://127.0.0.1/api/webhooks/123/abc",
		"https://discord.com/api/v10/users/@me",
		"https://discord.com/api/webhooks/123/abc?wait=true",
		"https://user:pass@discord.com/api/webhooks/123/abc",
		"https://discord.com:8443/api/webhooks/123/abc",
	}
	for _, raw := range cases {
		if err := ValidateDiscordWebhookURL(raw); err == nil {
			t.Fatalf("expected reject %q", raw)
		}
	}
}

func TestValidateSlackAndTeamsWebhookURL(t *testing.T) {
	if err := validateWebhookURL(extensions.DeliverySlackWebhook, "https://hooks.slack.com/services/T00/B00/xxx"); err != nil {
		t.Fatal(err)
	}
	if err := validateWebhookURL(extensions.DeliverySlackWebhook, "https://hooks.slack.com/triggers/T00/123/xxx"); err != nil {
		t.Fatal(err)
	}
	if err := validateWebhookURL(extensions.DeliverySlackWebhook, "https://example.com/services/T00/B00/xxx"); err == nil {
		t.Fatal("expected slack host reject")
	}
	if err := validateWebhookURL(extensions.DeliverySlackWebhook, "https://hooks.slack.com/api/chat.postMessage"); err == nil {
		t.Fatal("expected slack path reject")
	}

	teams := []string{
		"https://contoso.webhook.office.com/webhookb2/abc/IncomingWebhook/def",
		"https://outlook.office.com/webhook/abc",
		"https://prod-28.westus.logic.azure.com:443/workflows/abc/triggers/manual/paths/invoke?api-version=2016-06-01&sp=%2Ftriggers%2Fmanual%2Frun&sv=1.0&sig=xyz",
		"https://default123.environment.api.powerplatform.com/powerautomate/automations/direct/workflows/abc/triggers/manual/paths/invoke?api-version=1&sig=xyz",
	}
	for _, raw := range teams {
		if err := validateWebhookURL(extensions.DeliveryTeamsWebhook, raw); err != nil {
			t.Fatalf("teams url %q: %v", raw, err)
		}
	}
	if err := validateWebhookURL(extensions.DeliveryTeamsWebhook, "https://example.com/webhook"); err == nil {
		t.Fatal("expected teams host reject")
	}
}

func TestValidateHTTPWebhookURL(t *testing.T) {
	if err := validateWebhookURL(extensions.DeliveryHTTPWebhook, "https://example.com/hooks/ordryn"); err != nil {
		t.Fatal(err)
	}
	if err := validateWebhookURL(extensions.DeliveryHTTPWebhook, "https://hooks.example.com:8443/path?token=1"); err != nil {
		t.Fatal(err)
	}

	rejects := []string{
		"http://example.com/hooks",
		"https://127.0.0.1/hooks",
		"https://localhost/hooks",
		"https://10.0.0.1/hooks",
		"https://192.168.1.1/hooks",
		"https://169.254.169.254/latest/meta-data/",
		"https://[::1]/hooks",
		"https://example.internal/hooks",
		"https://user:pass@example.com/hooks",
		"https://example.com/hooks#frag",
		"https://metadata.google.internal/",
	}
	for _, raw := range rejects {
		if err := validateWebhookURL(extensions.DeliveryHTTPWebhook, raw); err == nil {
			t.Fatalf("expected reject %q", raw)
		}
	}
}

func TestValidateDeliveryURLUsesType(t *testing.T) {
	d := &extensions.Delivery{Type: extensions.DeliverySlackWebhook}
	if err := ValidateDeliveryURL(d, "https://hooks.slack.com/services/T/B/x"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateDeliveryURL(d, "https://discord.com/api/webhooks/1/x"); err == nil {
		t.Fatal("slack delivery should reject discord host")
	}
}

func TestIsBlockedIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.0.1", "169.254.169.254", "100.64.0.1", "0.0.0.0"}
	for _, s := range blocked {
		ip := net.ParseIP(s)
		if ip == nil {
			t.Fatalf("parse %s", s)
		}
		if !isBlockedIP(ip) {
			t.Fatalf("expected blocked %s", s)
		}
	}
	if isBlockedIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("8.8.8.8 should be allowed")
	}
}

func TestValidateNtfyWebhookURL(t *testing.T) {
	if err := validateWebhookURL(extensions.DeliveryNtfyWebhook, "https://ntfy.sh/my-topic"); err != nil {
		t.Fatal(err)
	}
	if err := validateWebhookURL(extensions.DeliveryNtfyWebhook, "https://ntfy.example.com/alerts"); err != nil {
		t.Fatal(err)
	}
	if err := validateWebhookURL(extensions.DeliveryNtfyWebhook, "https://ntfy.sh/"); err == nil {
		t.Fatal("expected topic path required")
	}
	if err := validateWebhookURL(extensions.DeliveryNtfyWebhook, "https://10.0.0.8/topic"); err == nil {
		t.Fatal("expected LAN ntfy reject")
	}
}
