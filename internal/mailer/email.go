package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"GoTodo/internal/crypto/secret"

	"github.com/mailgun/mailgun-go/v5"
)

const (
	ProviderMailgun = "mailgun"
	ProviderSMTP    = "smtp"
)

type Config struct {
	Provider         string
	FromAddress      string
	FromName         string
	MailgunDomain    string
	MailgunAPIKeyEnc string
	SMTPHost         string
	SMTPPort         int
	SMTPUsername     string
	SMTPPasswordEnc  string
	SMTPTLS          bool
}

// SendEmail sends an email using the given provider config (Mailgun or SMTP).
// trigger identifies the product event (password reset, site invite, …) for audit logs.
func SendEmail(cfg Config, trigger, subject, message, toEmail string) error {
	err := sendEmail(cfg, subject, message, toEmail)
	recordAudit(cfg, trigger, toEmail, err)
	return err
}

func sendEmail(cfg Config, subject, message, toEmail string) error {
	fromAddr, from, err := checkConfigured(cfg)
	if err != nil {
		return err
	}
	if err := allowCoreSend(toEmail); err != nil {
		return err
	}
	return deliverConfigured(cfg, subject, message, toEmail, from, fromAddr)
}

func checkConfigured(cfg Config) (fromAddr, from string, err error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" || provider == "none" {
		return "", "", fmt.Errorf("email not configured")
	}

	fromAddr = strings.TrimSpace(cfg.FromAddress)
	if fromAddr == "" {
		return "", "", fmt.Errorf("email from address not configured")
	}

	switch provider {
	case ProviderMailgun:
		if strings.TrimSpace(cfg.MailgunDomain) == "" || cfg.MailgunAPIKeyEnc == "" {
			return "", "", fmt.Errorf("mailgun credentials not configured")
		}
	case ProviderSMTP:
		if strings.TrimSpace(cfg.SMTPHost) == "" || cfg.SMTPPort <= 0 || strings.TrimSpace(cfg.SMTPUsername) == "" || cfg.SMTPPasswordEnc == "" {
			return "", "", fmt.Errorf("smtp credentials not configured")
		}
	default:
		return "", "", fmt.Errorf("unsupported email provider %q", provider)
	}
	return fromAddr, formatFrom(cfg.FromName, fromAddr), nil
}

func deliverConfigured(cfg Config, subject, message, toEmail, from, fromAddr string) error {
	if f := testDeliver; f != nil {
		return f(cfg, subject, message, toEmail, from, fromAddr)
	}
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case ProviderMailgun:
		return sendViaMailgun(cfg, subject, message, toEmail, from, fromAddr)
	case ProviderSMTP:
		return sendViaSMTP(cfg, subject, message, toEmail, from, fromAddr)
	default:
		return fmt.Errorf("unsupported email provider %q", cfg.Provider)
	}
}

// testDeliver, when set, replaces SMTP/Mailgun so tests can exercise rate limits.
var testDeliver func(cfg Config, subject, message, toEmail, from, fromAddr string) error

func formatFrom(name, address string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return address
	}
	return fmt.Sprintf("%s <%s>", name, address)
}

func sendViaMailgun(cfg Config, subject, message, toEmail, from, fromAddr string) error {
	domain := strings.TrimSpace(cfg.MailgunDomain)
	if domain == "" || cfg.MailgunAPIKeyEnc == "" {
		return fmt.Errorf("mailgun credentials not configured")
	}
	apiKey, err := secret.Decrypt(cfg.MailgunAPIKeyEnc)
	if err != nil {
		return fmt.Errorf("decrypt mailgun api key: %w", err)
	}

	mg := mailgun.NewMailgun(apiKey)
	m := mailgun.NewMessage(domain, from, subject, message, toEmail)
	htmlBody := strings.ReplaceAll(message, "\n", "<br/>")
	m.SetHTML(htmlBody)
	m.SetReplyTo(fromAddr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if _, err := mg.Send(ctx, m); err != nil {
		fmt.Println("Email send error:", err)
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}

func sendViaSMTP(cfg Config, subject, message, toEmail, from, fromAddr string) error {
	host := strings.TrimSpace(cfg.SMTPHost)
	port := cfg.SMTPPort
	username := strings.TrimSpace(cfg.SMTPUsername)
	if host == "" || port <= 0 || username == "" || cfg.SMTPPasswordEnc == "" {
		return fmt.Errorf("smtp credentials not configured")
	}
	password, err := secret.Decrypt(cfg.SMTPPasswordEnc)
	if err != nil {
		return fmt.Errorf("decrypt smtp password: %w", err)
	}

	htmlBody := strings.ReplaceAll(message, "\n", "<br/>")
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + toEmail,
		"Reply-To: " + fromAddr,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
		"",
		htmlBody,
	}, "\r\n")

	addr := fmt.Sprintf("%s:%d", host, port)
	auth := smtp.PlainAuth("", username, password, host)

	if port == 465 {
		return sendSMTPWithTLS(addr, host, auth, fromAddr, []string{toEmail}, []byte(msg))
	}
	if cfg.SMTPTLS {
		return sendSMTPWithStartTLS(addr, host, auth, fromAddr, []string{toEmail}, []byte(msg))
	}
	if err := smtp.SendMail(addr, auth, fromAddr, []string{toEmail}, []byte(msg)); err != nil {
		fmt.Println("Email send error:", err)
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}

func sendSMTPWithTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer client.Close()

	return smtpClientSend(client, auth, from, to, msg)
}

func sendSMTPWithStartTLS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("failed to send email: %w", err)
	}
	defer client.Close()

	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
	}
	return smtpClientSend(client, auth, from, to, msg)
}

func smtpClientSend(client *smtp.Client, auth smtp.Auth, from string, to []string, msg []byte) error {
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(auth); err != nil {
				fmt.Println("Email send error:", err)
				return fmt.Errorf("failed to send email: %w", err)
			}
		}
	}
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	for _, addr := range to {
		if err := client.Rcpt(addr); err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return fmt.Errorf("failed to send email: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}
	if err := client.Quit(); err != nil {
		fmt.Println("Email send error:", err)
		return fmt.Errorf("failed to send email: %w", err)
	}
	return nil
}
