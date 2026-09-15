package hooks

import "testing"

func TestValidateDiscordWebhookURL(t *testing.T) {
	ok := "https://discord.com/api/webhooks/123/abc"
	if err := ValidateDiscordWebhookURL(ok); err != nil {
		t.Fatal(err)
	}
	legacy := "https://discordapp.com/api/webhooks/123/abc"
	if err := ValidateDiscordWebhookURL(legacy); err != nil {
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
	}
	for _, raw := range cases {
		if err := ValidateDiscordWebhookURL(raw); err == nil {
			t.Fatalf("expected reject %q", raw)
		}
	}
}
