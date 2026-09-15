package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"GoTodo/internal/extensions"
)

const maxWebhookResponseBytes = 8 << 10

var webhookHTTPClient = newWebhookHTTPClient()

func newWebhookHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           webhookDialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return fmt.Errorf("redirects are not allowed")
		},
	}
}

func webhookDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("unsupported network")
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: 5 * time.Second}
	var last error
	for _, ipa := range ips {
		if !ipNetworkOK(network, ipa.IP) {
			continue
		}
		if isBlockedIP(ipa.IP) {
			last = fmt.Errorf("webhook URL host is not allowed")
			continue
		}
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ipa.IP.String(), port))
		if err != nil {
			last = err
			continue
		}
		return conn, nil
	}
	if last == nil {
		last = fmt.Errorf("webhook URL host is not allowed")
	}
	return nil, last
}

func ipNetworkOK(network string, ip net.IP) bool {
	switch network {
	case "tcp4":
		return ip.To4() != nil
	case "tcp6":
		return ip.To4() == nil
	default:
		return true
	}
}

func sendWebhook(deliveryType, format, webhookURL, content, eventType string, vars map[string]string) error {
	if err := validateWebhookURL(deliveryType, webhookURL); err != nil {
		return err
	}
	content = prepareContent(content)
	if content == "" {
		return fmt.Errorf("message is empty")
	}
	body, err := marshalWebhookPayload(deliveryType, format, content, eventType, vars)
	if err != nil {
		return err
	}
	return postJSON(webhookURL, body)
}

func marshalWebhookPayload(deliveryType, format, content, eventType string, vars map[string]string) ([]byte, error) {
	switch strings.TrimSpace(deliveryType) {
	case extensions.DeliveryDiscordWebhook:
		return json.Marshal(map[string]string{"content": content})
	case extensions.DeliverySlackWebhook:
		return json.Marshal(map[string]string{"text": content})
	case extensions.DeliveryTeamsWebhook:
		return json.Marshal(teamsPayload(content))
	case extensions.DeliveryHTTPWebhook:
		return json.Marshal(httpWebhookPayload(format, content, eventType, vars))
	default:
		return nil, fmt.Errorf("unsupported delivery %q", deliveryType)
	}
}

func httpWebhookPayload(format, content, eventType string, vars map[string]string) any {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case extensions.DeliveryFormatContent:
		return map[string]string{"content": content}
	case extensions.DeliveryFormatJSON:
		return webhookJSONBody{
			Text:      content,
			Content:   content,
			Event:     eventType,
			ID:        vars["id"],
			Name:      vars["name"],
			Task:      vars["task"],
			Status:    vars["status"],
			OldStatus: vars["old_status"],
			Project:   vars["project"],
			Actor:     vars["actor"],
			URL:       vars["url"],
			Priority:  vars["priority"],
		}
	default:
		return map[string]string{"text": content}
	}
}

type webhookJSONBody struct {
	Text      string `json:"text"`
	Content   string `json:"content"`
	Event     string `json:"event,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Task      string `json:"task,omitempty"`
	Status    string `json:"status,omitempty"`
	OldStatus string `json:"old_status,omitempty"`
	Project   string `json:"project,omitempty"`
	Actor     string `json:"actor,omitempty"`
	URL       string `json:"url,omitempty"`
	Priority  string `json:"priority,omitempty"`
}

func teamsPayload(content string) map[string]any {
	return map[string]any{
		"type": "message",
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content": map[string]any{
					"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
					"type":    "AdaptiveCard",
					"version": "1.4",
					"body": []map[string]any{
						{
							"type": "TextBlock",
							"text": content,
							"wrap": true,
						},
					},
				},
			},
		},
	}
}

func postJSON(webhookURL string, body []byte) error {
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Ordryn-Webhook/1")
	resp, err := webhookHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxWebhookResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook HTTP %d", resp.StatusCode)
	}
	return nil
}
