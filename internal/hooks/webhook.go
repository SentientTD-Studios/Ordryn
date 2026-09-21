package hooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/extensions"
)

const maxWebhookResponseBytes = 8 << 10

var webhookHTTPClient = newWebhookHTTPClient()

type sendOpts struct {
	SigningSecret string
	AuthHeader    string
	ExtraHeaders  map[string]string
	Wait          bool
	ThreadID      string
	EventID       string
	DeliveryID    int64
}

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
	_, err := sendWebhookOpts(deliveryType, format, webhookURL, content, eventType, vars, sendOpts{})
	return err
}

func sendWebhookOpts(deliveryType, format, webhookURL, content, eventType string, vars map[string]string, opts sendOpts) (string, error) {
	if err := validateWebhookURL(deliveryType, webhookURL); err != nil {
		return "", err
	}
	content = prepareContent(content)
	if content == "" {
		return "", fmt.Errorf("message is empty")
	}
	body, err := marshalWebhookPayload(deliveryType, format, content, eventType, vars)
	if err != nil {
		return "", err
	}
	u := webhookURL
	if deliveryType == extensions.DeliveryDiscordWebhook {
		u = withQuery(u, "wait", opts.Wait)
		if opts.ThreadID != "" {
			u = withQueryValue(u, "thread_id", opts.ThreadID)
		}
	}
	if deliveryType == extensions.DeliveryGoogleChatWebhook {
		u = withQueryValue(u, "messageReplyOption", "REPLY_MESSAGE_FALLBACK_TO_NEW_THREAD")
	}
	respBody, status, err := postJSONOpts(u, body, opts)
	if err != nil {
		return "", err
	}
	_ = status
	return parseProviderMessageID(deliveryType, respBody), nil
}

func withQuery(raw string, key string, on bool) string {
	if !on {
		return raw
	}
	return withQueryValue(raw, key, "true")
}

func withQueryValue(raw, key, val string) string {
	if strings.Contains(raw, key+"=") {
		return raw
	}
	sep := "?"
	if strings.Contains(raw, "?") {
		sep = "&"
	}
	return raw + sep + key + "=" + urlQueryEscape(val)
}

func urlQueryEscape(s string) string {
	r := strings.NewReplacer(" ", "%20", "&", "%26", "=", "%3D")
	return r.Replace(s)
}

func marshalWebhookPayload(deliveryType, format, content, eventType string, vars map[string]string) ([]byte, error) {
	switch strings.TrimSpace(deliveryType) {
	case extensions.DeliveryDiscordWebhook:
		return json.Marshal(discordPayload(content, vars))
	case extensions.DeliverySlackWebhook:
		return json.Marshal(slackPayload(content, vars))
	case extensions.DeliveryTeamsWebhook:
		return json.Marshal(teamsPayload(content, vars))
	case extensions.DeliveryGoogleChatWebhook:
		return json.Marshal(gchatPayload(content, vars))
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
		body := webhookJSONBody{
			Text:       content,
			Content:    content,
			Event:      eventType,
			ID:         vars["id"],
			Name:       vars["name"],
			Task:       vars["task"],
			Status:     vars["status"],
			OldStatus:  vars["old_status"],
			Project:    vars["project"],
			Actor:      vars["actor"],
			URL:        vars["url"],
			Priority:   vars["priority"],
			Comment:    vars["comment"],
			ClaimedBy:  vars["claimed_by"],
			DueDate:    vars["due_date"],
			Sprint:     vars["sprint"],
			Tags:       vars["tags"],
			EventID:    vars["event_id"],
			OccurredAt: vars["occurred_at"],
			Changed:    splitCSV(vars["changed"]),
			Count:      vars["count"],
			Mentions:   vars["mentions"],
			Member:     vars["member"],
			JoinEmail:  vars["join_email"],
		}
		if raw := strings.TrimSpace(vars["fields_json"]); raw != "" {
			_ = json.Unmarshal([]byte(raw), &body.Fields)
		}
		if raw := strings.TrimSpace(vars["config_json"]); raw != "" {
			_ = json.Unmarshal([]byte(raw), &body.Config)
		}
		if tok := strings.TrimSpace(vars["callback_token"]); tok != "" {
			body.CallbackToken = tok
			body.CallbackURL = vars["callback_url"]
		}
		if id := atoiSafe(vars["actor_id"]); id > 0 || vars["actor"] != "" {
			body.ActorObj = &webhookActor{ID: id, Name: vars["actor"]}
		}
		if id := atoiSafe(vars["id"]); id > 0 || vars["task"] != "" {
			body.TaskObj = &webhookTask{
				ID:             id,
				Title:          vars["task"],
				Description:    vars["description"],
				Status:         vars["status"],
				Priority:       vars["priority"],
				ParentID:       atoiSafe(vars["parent_id"]),
				EstimatePoints: atoiSafe(vars["estimate"]),
				DueDate:        vars["due_date"],
			}
		}
		if pid := atoiSafe(vars["project_id"]); pid > 0 || vars["project"] != "" {
			body.ProjectObj = &webhookProject{ID: pid, Name: vars["project"]}
		}
		if raw := strings.TrimSpace(vars["changes_json"]); raw != "" {
			_ = json.Unmarshal([]byte(raw), &body.Changes)
		}
		if raw := strings.TrimSpace(vars["digest_events"]); raw != "" {
			body.DigestEvents = splitCSV(raw)
			if strings.Contains(raw, "\n") {
				body.DigestEvents = nil
				for _, line := range strings.Split(raw, "\n") {
					line = strings.TrimSpace(line)
					if line != "" {
						body.DigestEvents = append(body.DigestEvents, line)
					}
				}
			}
		}
		return body
	default:
		return map[string]string{"text": content}
	}
}

type webhookJSONBody struct {
	Text          string            `json:"text"`
	Content       string            `json:"content"`
	Event         string            `json:"event,omitempty"`
	ID            string            `json:"id,omitempty"`
	Name          string            `json:"name,omitempty"`
	Task          string            `json:"task,omitempty"`
	Status        string            `json:"status,omitempty"`
	OldStatus     string            `json:"old_status,omitempty"`
	Project       string            `json:"project,omitempty"`
	Actor         string            `json:"actor,omitempty"`
	URL           string            `json:"url,omitempty"`
	Priority      string            `json:"priority,omitempty"`
	Comment       string            `json:"comment,omitempty"`
	ClaimedBy     string            `json:"claimed_by,omitempty"`
	DueDate       string            `json:"due_date,omitempty"`
	Sprint        string            `json:"sprint,omitempty"`
	Tags          string            `json:"tags,omitempty"`
	EventID       string            `json:"event_id,omitempty"`
	OccurredAt    string            `json:"occurred_at,omitempty"`
	Changed       []string          `json:"changed,omitempty"`
	Count         string            `json:"count,omitempty"`
	Mentions      string            `json:"mentions,omitempty"`
	Member        string            `json:"member,omitempty"`
	JoinEmail     string            `json:"join_email,omitempty"`
	Fields        map[string]string `json:"fields,omitempty"`
	Config        map[string]string `json:"config,omitempty"`
	CallbackToken string            `json:"callback_token,omitempty"`
	CallbackURL   string            `json:"callback_url,omitempty"`
	ActorObj      *webhookActor     `json:"actor_detail,omitempty"`
	TaskObj       *webhookTask      `json:"task_detail,omitempty"`
	ProjectObj    *webhookProject   `json:"project_detail,omitempty"`
	Changes       []FieldChange     `json:"changes,omitempty"`
	DigestEvents  []string          `json:"digest_events,omitempty"`
}

type webhookActor struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

type webhookTask struct {
	ID             int    `json:"id,omitempty"`
	Title          string `json:"title,omitempty"`
	Description    string `json:"description,omitempty"`
	Status         string `json:"status,omitempty"`
	Priority       string `json:"priority,omitempty"`
	ParentID       int    `json:"parent_id,omitempty"`
	EstimatePoints int    `json:"estimate_points,omitempty"`
	DueDate        string `json:"due_date,omitempty"`
}

type webhookProject struct {
	ID   int    `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func atoiSafe(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func splitCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func discordPayload(content string, vars map[string]string) map[string]any {
	embed := map[string]any{
		"description": content,
	}
	if name := strings.TrimSpace(vars["name"]); name != "" {
		embed["title"] = name
	}
	if vars["url"] != "" {
		embed["url"] = vars["url"]
	}
	fields := []map[string]any{}
	if vars["status"] != "" {
		fields = append(fields, map[string]any{"name": "Status", "value": vars["status"], "inline": true})
	}
	if vars["actor"] != "" {
		fields = append(fields, map[string]any{"name": "Actor", "value": vars["actor"], "inline": true})
	}
	if vars["project"] != "" {
		fields = append(fields, map[string]any{"name": "Project", "value": vars["project"], "inline": true})
	}
	if len(fields) > 0 {
		embed["fields"] = fields
	}
	return map[string]any{"embeds": []any{embed}}
}

func slackPayload(content string, vars map[string]string) map[string]any {
	blocks := []map[string]any{
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": content}},
	}
	if vars["url"] != "" {
		blocks = append(blocks, map[string]any{
			"type": "actions",
			"elements": []map[string]any{{
				"type": "button",
				"text": map[string]any{"type": "plain_text", "text": "Open"},
				"url":  vars["url"],
			}},
		})
	}
	return map[string]any{"text": content, "blocks": blocks}
}

func teamsPayload(content string, vars map[string]string) map[string]any {
	body := []map[string]any{
		{"type": "TextBlock", "text": vars["name"], "weight": "Bolder", "size": "Medium", "wrap": true},
		{"type": "TextBlock", "text": content, "wrap": true},
	}
	actions := []map[string]any{}
	if vars["url"] != "" {
		actions = append(actions, map[string]any{
			"type":  "Action.OpenUrl",
			"title": "Open",
			"url":   vars["url"],
		})
	}
	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"body":    body,
	}
	if len(actions) > 0 {
		card["actions"] = actions
	}
	return map[string]any{
		"type": "message",
		"attachments": []map[string]any{
			{
				"contentType": "application/vnd.microsoft.card.adaptive",
				"content":     card,
			},
		},
	}
}

var gchatStarBold = regexp.MustCompile(`\*(.+?)\*`)
var gchatStarStarBold = regexp.MustCompile(`\*\*(.+?)\*\*`)

func gchatPayload(content string, vars map[string]string) map[string]any {
	if vars == nil {
		vars = map[string]string{}
	}
	widgets := []map[string]any{
		{"textParagraph": map[string]any{"text": gchatHTML(content)}},
	}
	for _, row := range []struct{ label, key string }{
		{"Status", "status"},
		{"Actor", "actor"},
		{"Priority", "priority"},
		{"Due", "due_date"},
	} {
		if v := strings.TrimSpace(vars[row.key]); v != "" {
			widgets = append(widgets, map[string]any{
				"decoratedText": map[string]any{
					"topLabel": row.label,
					"text":     html.EscapeString(v),
				},
			})
		}
	}
	if u := strings.TrimSpace(vars["url"]); u != "" {
		widgets = append(widgets, map[string]any{
			"buttonList": map[string]any{
				"buttons": []map[string]any{{
					"text": "Open",
					"onClick": map[string]any{
						"openLink": map[string]any{"url": u},
					},
				}},
			},
		})
	}
	card := map[string]any{
		"sections": []map[string]any{{"widgets": widgets}},
	}
	header := map[string]any{}
	if name := strings.TrimSpace(vars["name"]); name != "" {
		header["title"] = name
	}
	if project := strings.TrimSpace(vars["project"]); project != "" {
		header["subtitle"] = project
	}
	if len(header) > 0 {
		card["header"] = header
	}
	cardID := "ordryn"
	if id := strings.TrimSpace(vars["event_id"]); id != "" {
		cardID = "ordryn-" + id
	} else if id := strings.TrimSpace(vars["id"]); id != "" {
		cardID = "ordryn-task-" + id
	}
	out := map[string]any{
		"text": content,
		"cardsV2": []map[string]any{{
			"cardId": cardID,
			"card":   card,
		}},
	}
	if id := strings.TrimSpace(vars["id"]); id != "" {
		out["thread"] = map[string]any{"threadKey": "ordryn-task-" + id}
	}
	return out
}

func gchatHTML(s string) string {
	s = html.EscapeString(s)
	s = strings.ReplaceAll(s, "\n", "<br>")
	s = gchatStarStarBold.ReplaceAllString(s, "<b>$1</b>")
	return gchatStarBold.ReplaceAllString(s, "<b>$1</b>")
}

func sendNtfy(webhookURL, auth, content string, vars map[string]string, opts sendOpts) error {
	if err := validateWebhookURL(extensions.DeliveryNtfyWebhook, webhookURL); err != nil {
		return err
	}
	content = prepareContent(content)
	if content == "" {
		return fmt.Errorf("message is empty")
	}
	headers := map[string]string{
		"Title": vars["name"],
		"Click": vars["url"],
	}
	if p := ntfyPriority(vars["priority"]); p != "" {
		headers["Priority"] = p
	}
	opts.ExtraHeaders = headers
	if strings.TrimSpace(auth) != "" {
		opts.AuthHeader = "Bearer " + strings.TrimSpace(auth)
	}
	_, _, err := postJSONOpts(webhookURL, []byte(content), opts)
	return err
}

func ntfyPriority(label string) string {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "high":
		return "high"
	case "low":
		return "low"
	default:
		return "default"
	}
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func postJSON(webhookURL string, body []byte) error {
	_, _, err := postJSONOpts(webhookURL, body, sendOpts{})
	return err
}

func postJSONOpts(webhookURL string, body []byte, opts sendOpts) (respBody []byte, status int, err error) {
	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	if json.Valid(body) {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	}
	req.Header.Set("User-Agent", "Ordryn-Webhook/1")
	if strings.TrimSpace(opts.EventID) != "" {
		req.Header.Set("X-Ordryn-Event-Id", opts.EventID)
	}
	if opts.DeliveryID > 0 {
		req.Header.Set("X-Ordryn-Delivery-Id", strconv.FormatInt(opts.DeliveryID, 10))
	}
	if strings.TrimSpace(opts.SigningSecret) != "" {
		req.Header.Set("X-Ordryn-Signature", signBody(opts.SigningSecret, body))
	}
	if opts.AuthHeader != "" {
		req.Header.Set("Authorization", opts.AuthHeader)
	}
	for k, v := range opts.ExtraHeaders {
		if strings.TrimSpace(v) != "" {
			req.Header.Set(k, v)
		}
	}
	resp, err := webhookHTTPClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxWebhookResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return raw, resp.StatusCode, webhookStatusError(resp.StatusCode, raw, resp.Header)
	}
	return raw, resp.StatusCode, nil
}

type webhookHTTPError struct {
	Status     int
	Snippet    string
	RetryAfter time.Duration
}

func (e *webhookHTTPError) Error() string {
	if e == nil {
		return ""
	}
	if e.Status == http.StatusTooManyRequests {
		if e.RetryAfter > 0 {
			return "Destination is rate-limited. Next retry in " + formatRetryAfter(e.RetryAfter) + "."
		}
		return "Destination is rate-limited. Deliveries will retry automatically."
	}
	if e.Snippet == "" {
		return fmt.Sprintf("webhook HTTP %d", e.Status)
	}
	return fmt.Sprintf("webhook HTTP %d: %s", e.Status, e.Snippet)
}

func (e *webhookHTTPError) Unwrap() error {
	if e != nil && e.Status == http.StatusTooManyRequests {
		return ErrRateLimited
	}
	return nil
}

func webhookStatusError(status int, body []byte, header http.Header) error {
	snippet := strings.Join(strings.Fields(strings.TrimSpace(string(body))), " ")
	err := &webhookHTTPError{
		Status:  status,
		Snippet: truncateRunes(snippet, 240),
	}
	if status == http.StatusTooManyRequests {
		err.RetryAfter = parseRetryAfter(header, body)
	}
	return err
}

func parseRetryAfter(header http.Header, body []byte) time.Duration {
	if header != nil {
		raw := strings.TrimSpace(header.Get("Retry-After"))
		if raw != "" {
			if secs, err := strconv.Atoi(raw); err == nil && secs > 0 {
				return clampRetryAfter(time.Duration(secs) * time.Second)
			}
			if t, err := http.ParseTime(raw); err == nil {
				if d := time.Until(t); d > 0 {
					return clampRetryAfter(d)
				}
			}
		}
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return 0
	}
	return clampRetryAfter(retryAfterFromJSON(obj))
}

func retryAfterFromJSON(obj map[string]any) time.Duration {
	if obj == nil {
		return 0
	}
	if d := durationFromJSONNumber(obj["retry_after"]); d > 0 {
		return d
	}
	if errObj, ok := obj["error"].(map[string]any); ok {
		if d := durationFromJSONNumber(errObj["retry_after"]); d > 0 {
			return d
		}
	}
	return 0
}

func durationFromJSONNumber(v any) time.Duration {
	switch n := v.(type) {
	case float64:
		if n <= 0 {
			return 0
		}
		return time.Duration(n * float64(time.Second))
	case json.Number:
		f, err := n.Float64()
		if err != nil || f <= 0 {
			return 0
		}
		return time.Duration(f * float64(time.Second))
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil || f <= 0 {
			return 0
		}
		return time.Duration(f * float64(time.Second))
	default:
		return 0
	}
}

func clampRetryAfter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if d > 30*time.Minute {
		return 30 * time.Minute
	}
	return d
}

func formatRetryAfter(d time.Duration) string {
	if d < time.Second {
		d = time.Second
	}
	if d < time.Minute {
		secs := int(d.Round(time.Second) / time.Second)
		if secs <= 1 {
			return "1 second"
		}
		return strconv.Itoa(secs) + " seconds"
	}
	mins := int((d + time.Minute/2) / time.Minute)
	if mins <= 1 {
		return "1 minute"
	}
	return strconv.Itoa(mins) + " minutes"
}

func parseProviderMessageID(deliveryType string, body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return ""
	}
	switch strings.TrimSpace(deliveryType) {
	case extensions.DeliveryDiscordWebhook:
		if id, _ := obj["id"].(string); id != "" {
			return id
		}
	case extensions.DeliverySlackWebhook:
		if ts, _ := obj["ts"].(string); ts != "" {
			return ts
		}
	case extensions.DeliveryGoogleChatWebhook:
		if name, _ := obj["name"].(string); name != "" {
			return name
		}
	}
	return ""
}

func httpStatusOf(err error) int {
	if err == nil {
		return 200
	}
	var he *webhookHTTPError
	if errors.As(err, &he) && he != nil {
		return he.Status
	}
	var n int
	_, _ = fmt.Sscanf(err.Error(), "webhook HTTP %d", &n)
	return n
}

func retryAfterOf(err error) time.Duration {
	var he *webhookHTTPError
	if errors.As(err, &he) && he != nil {
		return he.RetryAfter
	}
	return 0
}

func retryableStatus(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrRateLimited) {
		return true
	}
	code := httpStatusOf(err)
	if code == 429 || code >= 500 {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "connection")
}
