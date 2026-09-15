package hooks

import (
	"fmt"
	"net"
	"net/url"
	"strings"

	"GoTodo/internal/extensions"
)

// ValidateDiscordWebhookURL rejects non-Discord hosts (SSRF).
func ValidateDiscordWebhookURL(raw string) error {
	return validateWebhookURL(extensions.DeliveryDiscordWebhook, raw)
}

// ValidateDeliveryURL checks a project webhook URL against the extension's delivery type.
func ValidateDeliveryURL(d *extensions.Delivery, raw string) error {
	typ := extensions.DeliveryHTTPWebhook
	if d != nil && strings.TrimSpace(d.Type) != "" {
		typ = strings.TrimSpace(d.Type)
	}
	return validateWebhookURL(typ, raw)
}

func validateWebhookURL(deliveryType, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("webhook URL is required")
	}
	if strings.ContainsAny(raw, "\r\n") {
		return fmt.Errorf("invalid webhook URL")
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
	if u.Fragment != "" || u.RawFragment != "" {
		return fmt.Errorf("webhook URL must not include a fragment")
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	if host == "" {
		return fmt.Errorf("invalid webhook URL")
	}
	if err := checkWebhookHost(host); err != nil {
		return err
	}
	port := u.Port()
	if port != "" && port != "443" && deliveryType != extensions.DeliveryHTTPWebhook {
		return fmt.Errorf("webhook URL host is not allowed")
	}

	path := strings.ToLower(u.EscapedPath())
	switch deliveryType {
	case extensions.DeliveryDiscordWebhook:
		if u.RawQuery != "" {
			return fmt.Errorf("webhook URL must not include query or fragment")
		}
		if !discordHostAllowed(host) {
			return fmt.Errorf("webhook URL host is not allowed")
		}
		if !strings.HasPrefix(path, "/api/webhooks/") {
			return fmt.Errorf("webhook URL path is not a Discord webhook")
		}
	case extensions.DeliverySlackWebhook:
		if host != "hooks.slack.com" {
			return fmt.Errorf("webhook URL host is not allowed")
		}
		if !slackPathAllowed(path) {
			return fmt.Errorf("webhook URL path is not a Slack webhook")
		}
	case extensions.DeliveryTeamsWebhook:
		if !teamsHostAllowed(host) {
			return fmt.Errorf("webhook URL host is not allowed")
		}
		if path == "" || path == "/" {
			return fmt.Errorf("webhook URL path is not a Teams webhook")
		}
	case extensions.DeliveryHTTPWebhook:
		// Public HTTPS only; host allowlist is the caller's choice.
	default:
		return fmt.Errorf("unsupported delivery %q", deliveryType)
	}
	return nil
}

func checkWebhookHost(host string) error {
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("webhook URL host is not allowed")
		}
		return nil
	}
	if isBlockedHostname(host) {
		return fmt.Errorf("webhook URL host is not allowed")
	}
	if !strings.Contains(host, ".") {
		return fmt.Errorf("webhook URL host is not allowed")
	}
	return nil
}

func isBlockedHostname(host string) bool {
	h := strings.ToLower(strings.TrimSpace(host))
	if h == "" || h == "localhost" || h == "metadata.google.internal" {
		return true
	}
	for _, suf := range []string{".localhost", ".local", ".internal", ".lan", ".corp", ".home"} {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	return false
}

func discordHostAllowed(host string) bool {
	switch host {
	case "discord.com", "discordapp.com", "canary.discord.com", "ptb.discord.com":
		return true
	default:
		return false
	}
}

func slackPathAllowed(path string) bool {
	return strings.HasPrefix(path, "/services/") ||
		strings.HasPrefix(path, "/triggers/") ||
		strings.HasPrefix(path, "/workflows/")
}

func teamsHostAllowed(host string) bool {
	return hostMatches(host, "webhook.office.com") ||
		hostMatches(host, "outlook.office.com") ||
		hostMatches(host, "outlook.office365.com") ||
		hostMatches(host, "logic.azure.com") ||
		hostMatches(host, "api.powerplatform.com")
}

func hostMatches(host, domain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	domain = strings.ToLower(strings.TrimSpace(domain))
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 0 {
			return true
		}
		// Carrier-grade NAT (includes some cloud metadata addresses).
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
	}
	return false
}
