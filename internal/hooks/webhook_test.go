package hooks

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"GoTodo/internal/extensions"
)

func TestMarshalWebhookPayloads(t *testing.T) {
	vars := map[string]string{
		"id": "9", "name": "Ship", "task": "Ship", "status": "Done",
		"old_status": "Todo", "project": "Ordryn", "actor": "ada", "url": "https://x/tasks/9", "priority": "High",
	}

	raw, err := marshalWebhookPayload(extensions.DeliveryDiscordWebhook, "", "hello", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	var discord map[string]any
	if err := json.Unmarshal(raw, &discord); err != nil {
		t.Fatal(err)
	}
	if discord["content"] != nil || discord["embeds"] == nil {
		t.Fatalf("discord payload=%s", raw)
	}
	if !strings.Contains(string(raw), "Open") && !strings.Contains(string(raw), vars["url"]) {
		t.Fatalf("discord should include task url, got %s", raw)
	}

	raw, err = marshalWebhookPayload(extensions.DeliverySlackWebhook, "", "hello", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	var slack map[string]any
	if err := json.Unmarshal(raw, &slack); err != nil {
		t.Fatal(err)
	}
	if slack["text"] != "hello" || slack["blocks"] == nil {
		t.Fatalf("slack payload=%s", raw)
	}

	raw, err = marshalWebhookPayload(extensions.DeliveryHTTPWebhook, "", "hello", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEq(t, raw, map[string]any{"text": "hello"})

	raw, err = marshalWebhookPayload(extensions.DeliveryHTTPWebhook, extensions.DeliveryFormatContent, "hello", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	assertJSONEq(t, raw, map[string]any{"content": "hello"})

	raw, err = marshalWebhookPayload(extensions.DeliveryHTTPWebhook, extensions.DeliveryFormatJSON, "hello", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	var body webhookJSONBody
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Text != "hello" || body.Content != "hello" || body.Event != "task.updated" || body.ID != "9" || body.Project != "Ordryn" {
		t.Fatalf("json body=%+v", body)
	}
	vars["event_id"] = "evt-1"
	vars["occurred_at"] = "2026-09-15T16:00:00Z"
	vars["changed"] = "status,due_date"
	vars["fields_json"] = `{"severity.level":"high"}`
	raw, err = marshalWebhookPayload(extensions.DeliveryHTTPWebhook, extensions.DeliveryFormatJSON, "hello", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.EventID != "evt-1" || len(body.Changed) != 2 || body.Fields["severity.level"] != "high" {
		t.Fatalf("rich json body=%+v", body)
	}
	vars["actor_id"] = "3"
	vars["description"] = "Ship it"
	vars["parent_id"] = "1"
	vars["estimate"] = "5"
	vars["project_id"] = "7"
	vars["changes_json"] = `[{"field":"status","old":"Todo","new":"Done"}]`
	vars["digest_events"] = "Ship (task.updated)\nLater (task.created)"
	raw, err = marshalWebhookPayload(extensions.DeliveryHTTPWebhook, extensions.DeliveryFormatJSON, "hello", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.ActorObj == nil || body.ActorObj.Name != "ada" || body.TaskObj == nil || body.TaskObj.Description != "Ship it" || body.ProjectObj == nil || body.ProjectObj.ID != 7 {
		t.Fatalf("nested json body=%+v", body)
	}
	if len(body.Changes) != 1 || body.Changes[0].Field != "status" || len(body.DigestEvents) != 2 {
		t.Fatalf("changes/digest json body=%+v", body)
	}

	raw, err = marshalWebhookPayload(extensions.DeliveryTeamsWebhook, "", "hello **world**", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	var teams map[string]any
	if err := json.Unmarshal(raw, &teams); err != nil {
		t.Fatal(err)
	}
	if teams["type"] != "message" {
		t.Fatalf("teams type=%v", teams["type"])
	}
	if !strings.Contains(string(raw), "AdaptiveCard") || !strings.Contains(string(raw), "hello **world**") {
		t.Fatalf("teams payload=%s", raw)
	}
}

func TestPostJSONSuccessAndError(t *testing.T) {
	var gotBody []byte
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotBody, _ = io.ReadAll(r.Body)
		if r.URL.Path == "/fail" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unknown name \"content\""}}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	prev := webhookHTTPClient
	webhookHTTPClient = srv.Client()
	t.Cleanup(func() { webhookHTTPClient = prev })

	if err := postJSON(srv.URL+"/ok", []byte(`{"text":"hi"}`)); err != nil {
		t.Fatal(err)
	}
	if gotUA != "Ordryn-Webhook/1" {
		t.Fatalf("user-agent=%q", gotUA)
	}
	if string(gotBody) != `{"text":"hi"}` {
		t.Fatalf("body=%s", gotBody)
	}
	var gotEvent, gotDelivery string
	hdrSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEvent = r.Header.Get("X-Ordryn-Event-Id")
		gotDelivery = r.Header.Get("X-Ordryn-Delivery-Id")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(hdrSrv.Close)
	webhookHTTPClient = hdrSrv.Client()
	if _, _, err := postJSONOpts(hdrSrv.URL, []byte(`{"text":"hi"}`), sendOpts{EventID: "evt-9", DeliveryID: 44}); err != nil {
		t.Fatal(err)
	}
	if gotEvent != "evt-9" || gotDelivery != "44" {
		t.Fatalf("headers event=%q delivery=%q", gotEvent, gotDelivery)
	}
	err := postJSON(srv.URL+"/fail", []byte(`{}`))
	if err == nil || !strings.Contains(err.Error(), "webhook HTTP 400") || !strings.Contains(err.Error(), "Unknown name") {
		t.Fatalf("err=%v", err)
	}

	limited := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"You are being rate limited.","retry_after":0.64}`))
	}))
	t.Cleanup(limited.Close)
	webhookHTTPClient = limited.Client()
	err = postJSON(limited.URL, []byte(`{"text":"hi"}`))
	if err == nil || !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate limit err=%v", err)
	}
	if !strings.Contains(err.Error(), "rate-limited") {
		t.Fatalf("friendly err=%v", err)
	}
	if httpStatusOf(err) != 429 {
		t.Fatalf("status=%d", httpStatusOf(err))
	}
	if ra := retryAfterOf(err); ra != 7*time.Second {
		t.Fatalf("retry-after header=%s", ra)
	}
}

func TestGoogleChatWebhookPayloadAndURL(t *testing.T) {
	ok := "https://chat.googleapis.com/v1/spaces/AAAAexample/messages?key=abc&token=def"
	if err := validateWebhookURL(extensions.DeliveryGoogleChatWebhook, ok); err != nil {
		t.Fatal(err)
	}
	rejects := []string{
		"https://example.com/v1/spaces/AAA/messages?key=a&token=b",
		"https://chat.googleapis.com/v1/spaces/AAA",
		"https://chat.googleapis.com/v1/spaces/AAA/messages",
		"https://chat.googleapis.com/v1/media/AAA?key=a",
		"https://chat.googleapis.com.evil.com/v1/spaces/AAA/messages?key=a&token=b",
	}
	for _, raw := range rejects {
		if err := validateWebhookURL(extensions.DeliveryGoogleChatWebhook, raw); err == nil {
			t.Fatalf("expected reject %q", raw)
		}
	}

	vars := map[string]string{
		"id": "9", "name": "Ship", "status": "Done", "project": "Ordryn",
		"actor": "ada", "url": "https://x/tasks/9", "priority": "High",
	}
	raw, err := marshalWebhookPayload(extensions.DeliveryGoogleChatWebhook, "", "Task *Ship* updated", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body["text"] != "Task *Ship* updated" {
		t.Fatalf("text=%v", body["text"])
	}
	if _, ok := body["cardsV2"]; !ok {
		t.Fatalf("missing cardsV2: %s", raw)
	}
	if !strings.Contains(string(raw), "Open") || !strings.Contains(string(raw), vars["url"]) {
		t.Fatalf("card should include Open url, got %s", raw)
	}
	if gchatHTML("Task *Ship* updated") != "Task <b>Ship</b> updated" {
		t.Fatalf("gchatHTML=%q", gchatHTML("Task *Ship* updated"))
	}
	thread, _ := body["thread"].(map[string]any)
	if thread["threadKey"] != "ordryn-task-9" {
		t.Fatalf("thread=%v", body["thread"])
	}
	cards, _ := body["cardsV2"].([]any)
	if len(cards) == 0 {
		t.Fatal("missing cardsV2 entries")
	}
	card0, _ := cards[0].(map[string]any)
	if card0["cardId"] != "ordryn-task-9" {
		t.Fatalf("cardId=%v", card0["cardId"])
	}
	vars["event_id"] = "evt-42"
	raw, err = marshalWebhookPayload(extensions.DeliveryGoogleChatWebhook, "", "Task *Ship* updated", "task.updated", vars)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	cards, _ = body["cardsV2"].([]any)
	card0, _ = cards[0].(map[string]any)
	if card0["cardId"] != "ordryn-evt-42" {
		t.Fatalf("event cardId=%v", card0["cardId"])
	}
}

func TestPostJSONSignsBody(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Ordryn-Signature")
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	prev := webhookHTTPClient
	webhookHTTPClient = srv.Client()
	t.Cleanup(func() { webhookHTTPClient = prev })

	body := []byte(`{"text":"hi"}`)
	if _, _, err := postJSONOpts(srv.URL, body, sendOpts{SigningSecret: "s3cret"}); err != nil {
		t.Fatal(err)
	}
	want := signBody("s3cret", body)
	if gotSig != want {
		t.Fatalf("sig=%q want=%q", gotSig, want)
	}
}

func TestSendWebhookRejectsBadURL(t *testing.T) {
	err := sendWebhook(extensions.DeliveryHTTPWebhook, "", "https://127.0.0.1/hooks", "hello", "task.updated", nil)
	if err == nil {
		t.Fatal("expected SSRF reject")
	}
}

func TestDeliverUnsupportedType(t *testing.T) {
	m := extensions.Manifest{
		ID:       "x",
		Delivery: &extensions.Delivery{Type: "smtp", URLFrom: "webhook_url"},
	}
	sent, err := deliver(m, "hello", 3, "task.updated", nil)
	if sent || err == nil || !strings.Contains(err.Error(), "unsupported delivery") {
		t.Fatalf("sent=%v err=%v", sent, err)
	}
}

func assertJSONEq(t *testing.T, raw []byte, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("key %s got=%v want=%v", k, got[k], v)
		}
	}
}
