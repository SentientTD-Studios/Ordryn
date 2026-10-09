package hooks

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"GoTodo/internal/extensions"
)

// A prepared delivery must reach its destination from captured secrets alone:
// by send time the project, its settings, and its stored secrets are deleted.
func TestPreparedSendUsesCapturedSecrets(t *testing.T) {
	type received struct {
		body      []byte
		signature string
		eventID   string
	}
	got := make(chan received, 1)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got <- received{body: body, signature: r.Header.Get("X-Ordryn-Signature"), eventID: r.Header.Get("X-Ordryn-Event-Id")}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)

	// Route the public-looking URL to the test server; the URL itself still has
	// to pass the normal SSRF validation.
	client := srv.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	client.Transport = transport
	prev := webhookHTTPClient
	webhookHTTPClient = client
	t.Cleanup(func() { webhookHTTPClient = prev })

	entry := extensions.Entry{
		ID:     "prepared-test",
		Loaded: true,
		Manifest: extensions.Manifest{
			ID:       "prepared-test",
			Delivery: &extensions.Delivery{Type: extensions.DeliveryHTTPWebhook, URLFrom: "webhook_url", Format: "json"},
		},
	}
	p := &Prepared{items: []preparedDelivery{{
		entry: entry,
		ctx: destContext{
			ProjectID: 987654, // no such project, settings, or stored secret
			Event:     Event{Type: EventProjectDeleted, ProjectID: 987654, EventID: "evt-prepared-1"},
			Vars:      map[string]string{"event": EventProjectDeleted, "project": "Gone"},
			Message:   "Project Gone was deleted",
			Secrets:   &destSecrets{URL: "https://example.com/hook", Signing: "sign-me"},
		},
	}}}
	if p.Len() != 1 {
		t.Fatalf("Len=%d want 1", p.Len())
	}

	p.Send()

	select {
	case r := <-got:
		var payload map[string]any
		if err := json.Unmarshal(r.body, &payload); err != nil {
			t.Fatalf("body is not JSON: %v (%s)", err, r.body)
		}
		if payload["event"] != EventProjectDeleted {
			t.Fatalf("event=%v want %s (body %s)", payload["event"], EventProjectDeleted, r.body)
		}
		if r.eventID != "evt-prepared-1" {
			t.Fatalf("X-Ordryn-Event-Id=%q", r.eventID)
		}
		if r.signature == "" {
			t.Fatal("expected the captured signing secret to sign the request")
		}
	default:
		t.Fatal("prepared delivery was not sent")
	}
}

func TestPrepareWithoutExtensionsIsEmpty(t *testing.T) {
	p := Prepare(Event{Type: EventProjectDeleted, ProjectID: 1})
	if p.Len() != 0 {
		t.Fatalf("Len=%d want 0 with no loaded extensions", p.Len())
	}
	p.Send() // no-op
	var nilPrepared *Prepared
	nilPrepared.Send()
}
