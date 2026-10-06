package hooks

import (
	"sync"

	"GoTodo/internal/extensions"
)

// Sink receives every outbound event alongside extensions. Active is checked
// before any event work is done, so it must be cheap.
type Sink struct {
	Name   string
	Active func() bool
	Handle func(Event)
}

var (
	sinksMu sync.RWMutex
	sinks   []Sink
)

// RegisterSink adds or replaces (by name) an in-process event sink.
func RegisterSink(s Sink) {
	if s.Handle == nil {
		return
	}
	sinksMu.Lock()
	defer sinksMu.Unlock()
	for i := range sinks {
		if sinks[i].Name == s.Name {
			sinks[i] = s
			return
		}
	}
	sinks = append(sinks, s)
}

// SinksActive reports whether any registered sink currently wants events.
func SinksActive() bool {
	sinksMu.RLock()
	defer sinksMu.RUnlock()
	for _, s := range sinks {
		if s.Active == nil || s.Active() {
			return true
		}
	}
	return false
}

// RunSinks hands ev to each active sink on its own goroutine.
func RunSinks(ev Event) {
	sinksMu.RLock()
	list := append([]Sink(nil), sinks...)
	sinksMu.RUnlock()
	for _, s := range list {
		if s.Active != nil && !s.Active() {
			continue
		}
		go s.Handle(ev)
	}
}

// PublicBaseURL is the site's absolute base URL, or "" when PUBLIC_URL (or an
// absolute BASE_PATH) is not configured.
func PublicBaseURL() string {
	return publicBaseURL()
}

// ValidateAgentWebhookURL checks an AI agent webhook URL: public HTTPS only.
func ValidateAgentWebhookURL(raw string) error {
	return validateWebhookURL(extensions.DeliveryHTTPWebhook, raw)
}

// PostAgentWebhook sends a signed JSON body to an agent webhook through the
// same SSRF-guarded client used for extension deliveries.
func PostAgentWebhook(webhookURL, signingSecret, eventType, eventID string, body []byte) error {
	if err := ValidateAgentWebhookURL(webhookURL); err != nil {
		return err
	}
	_, _, err := postJSONOpts(webhookURL, body, sendOpts{
		SigningSecret: signingSecret,
		EventID:       eventID,
		ExtraHeaders:  map[string]string{"X-Ordryn-Event": eventType},
	})
	return err
}

// PublicTaskURL is a task's absolute URL, or a relative one when PUBLIC_URL is unset.
func PublicTaskURL(taskID int) string {
	return publicTaskURL(taskID)
}
