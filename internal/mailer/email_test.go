package mailer

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestSendEmailValidation(t *testing.T) {
	from := "noreply@example.com"
	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name:    "empty provider",
			cfg:     Config{FromAddress: from},
			wantErr: "email not configured",
		},
		{
			name:    "none provider",
			cfg:     Config{Provider: "none", FromAddress: from},
			wantErr: "email not configured",
		},
		{
			name:    "whitespace none provider",
			cfg:     Config{Provider: " NONE ", FromAddress: from},
			wantErr: "email not configured",
		},
		{
			name:    "empty from address",
			cfg:     Config{Provider: ProviderSMTP},
			wantErr: "email from address not configured",
		},
		{
			name:    "whitespace from address",
			cfg:     Config{Provider: ProviderMailgun, FromAddress: "  "},
			wantErr: "email from address not configured",
		},
		{
			name:    "unsupported provider",
			cfg:     Config{Provider: "sendgrid", FromAddress: from},
			wantErr: "unsupported email provider",
		},
		{
			name:    "mailgun missing domain",
			cfg:     Config{Provider: ProviderMailgun, FromAddress: from, MailgunAPIKeyEnc: "enc"},
			wantErr: "mailgun credentials not configured",
		},
		{
			name:    "mailgun missing api key",
			cfg:     Config{Provider: ProviderMailgun, FromAddress: from, MailgunDomain: "mg.example.com"},
			wantErr: "mailgun credentials not configured",
		},
		{
			name: "smtp missing host",
			cfg: Config{
				Provider:        ProviderSMTP,
				FromAddress:     from,
				SMTPPort:        587,
				SMTPUsername:    "user",
				SMTPPasswordEnc: "enc",
			},
			wantErr: "smtp credentials not configured",
		},
		{
			name: "smtp missing port",
			cfg: Config{
				Provider:        ProviderSMTP,
				FromAddress:     from,
				SMTPHost:        "smtp.example.com",
				SMTPUsername:    "user",
				SMTPPasswordEnc: "enc",
			},
			wantErr: "smtp credentials not configured",
		},
		{
			name: "smtp missing username",
			cfg: Config{
				Provider:        ProviderSMTP,
				FromAddress:     from,
				SMTPHost:        "smtp.example.com",
				SMTPPort:        587,
				SMTPPasswordEnc: "enc",
			},
			wantErr: "smtp credentials not configured",
		},
		{
			name: "smtp missing password",
			cfg: Config{
				Provider:     ProviderSMTP,
				FromAddress:  from,
				SMTPHost:     "smtp.example.com",
				SMTPPort:     587,
				SMTPUsername: "user",
			},
			wantErr: "smtp credentials not configured",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := SendEmail(tt.cfg, TriggerPasswordReset, "subject", "body", "to@example.com")
			if err == nil {
				t.Fatalf("SendEmail() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("SendEmail() error = %q, want substring %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestFormatFrom(t *testing.T) {
	addr := "noreply@example.com"
	tests := []struct {
		name string
		from string
		want string
	}{
		{name: "", from: addr, want: addr},
		{name: "  ", from: addr, want: addr},
		{name: "GoTodo", from: addr, want: "GoTodo <noreply@example.com>"},
	}
	for _, tt := range tests {
		got := formatFrom(tt.name, tt.from)
		if got != tt.want {
			t.Fatalf("formatFrom(%q, %q) = %q, want %q", tt.name, tt.from, got, tt.want)
		}
	}
}

func TestSendEmailRecordsAudit(t *testing.T) {
	t.Cleanup(func() { SetAuditor(nil) })

	var got AuditEntry
	SetAuditor(func(entry AuditEntry) { got = entry })

	err := SendEmail(Config{FromAddress: "noreply@example.com"}, TriggerPasswordReset, "subject", "body", "user@example.com")
	if err == nil {
		t.Fatal("expected send error")
	}
	if got.Trigger != TriggerPasswordReset {
		t.Fatalf("trigger = %q", got.Trigger)
	}
	if got.ToEmail != "user@example.com" {
		t.Fatalf("to = %q", got.ToEmail)
	}
	if got.Status != StatusNotConfigured {
		t.Fatalf("status = %q, want %s", got.Status, StatusNotConfigured)
	}
	if got.Error == "" {
		t.Fatal("expected error text")
	}
}

func TestClassifyAudit(t *testing.T) {
	if status, _ := classifyAudit(Config{Provider: ProviderSMTP}, nil); status != StatusSent {
		t.Fatalf("nil err status = %q", status)
	}
	if status, _ := classifyAudit(Config{}, fmt.Errorf("email not configured")); status != StatusNotConfigured {
		t.Fatalf("empty provider status = %q", status)
	}
	if status, _ := classifyAudit(Config{Provider: ProviderSMTP}, fmt.Errorf("smtp credentials not configured")); status != StatusNotConfigured {
		t.Fatalf("missing creds status = %q", status)
	}
	if status, msg := classifyAudit(Config{Provider: ProviderSMTP}, fmt.Errorf("failed to send email: connection refused")); status != StatusFailed || msg == "" {
		t.Fatalf("smtp failure status = %q msg = %q", status, msg)
	}
	if status, _ := classifyAudit(Config{Provider: ProviderSMTP}, ErrRateLimited); status != StatusRateLimited {
		t.Fatalf("rate limited status = %q", status)
	}
}

func readySMTPConfig() Config {
	return Config{
		Provider:        ProviderSMTP,
		FromAddress:     "noreply@example.com",
		SMTPHost:        "smtp.example.com",
		SMTPPort:        587,
		SMTPUsername:    "user",
		SMTPPasswordEnc: "enc",
	}
}

func TestSendEmailRateLimited(t *testing.T) {
	t.Cleanup(func() {
		testDeliver = nil
		coreLimiter.reset()
		SetAuditor(nil)
	})
	coreLimiter.reset()
	testDeliver = func(Config, string, string, string, string, string) error { return nil }

	var last AuditEntry
	SetAuditor(func(entry AuditEntry) { last = entry })

	cfg := readySMTPConfig()
	origN := coreLimiter.recipientN
	coreLimiter.recipientN = 2
	t.Cleanup(func() { coreLimiter.recipientN = origN })

	for i := 0; i < 2; i++ {
		if err := SendEmail(cfg, TriggerPasswordReset, "s", "b", "user@example.com"); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
		if last.Status != StatusSent {
			t.Fatalf("status = %q", last.Status)
		}
	}
	err := SendEmail(cfg, TriggerPasswordReset, "s", "b", "user@example.com")
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want ErrRateLimited", err)
	}
	if last.Status != StatusRateLimited {
		t.Fatalf("audit status = %q", last.Status)
	}
	if err := SendEmail(cfg, TriggerSiteInvite, "s", "b", "other@example.com"); err != nil {
		t.Fatalf("other recipient: %v", err)
	}
}

func TestSendEmailUnconfiguredDoesNotConsumeQuota(t *testing.T) {
	t.Cleanup(func() {
		testDeliver = nil
		coreLimiter.reset()
		SetAuditor(nil)
	})
	coreLimiter.reset()
	origN := coreLimiter.recipientN
	coreLimiter.recipientN = 1
	t.Cleanup(func() { coreLimiter.recipientN = origN })
	testDeliver = func(Config, string, string, string, string, string) error { return nil }

	to := "user@example.com"
	for i := 0; i < 3; i++ {
		err := SendEmail(Config{FromAddress: "noreply@example.com"}, TriggerPasswordReset, "s", "b", to)
		if err == nil || !strings.Contains(err.Error(), "email not configured") {
			t.Fatalf("send %d: %v", i, err)
		}
	}
	if err := SendEmail(readySMTPConfig(), TriggerPasswordReset, "s", "b", to); err != nil {
		t.Fatalf("configured send after unconfigured attempts: %v", err)
	}
}
