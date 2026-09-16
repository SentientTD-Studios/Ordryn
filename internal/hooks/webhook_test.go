package hooks

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if err := postJSON(srv.URL+"/fail", []byte(`{}`)); err == nil || !strings.Contains(err.Error(), "webhook HTTP 400") {
		t.Fatalf("err=%v", err)
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
