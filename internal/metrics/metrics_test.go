package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstrumentAndWrite(t *testing.T) {
	reset()
	h := Instrument("/tasks/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	for i := 0; i < 3; i++ {
		h(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v2/tasks/12", nil))
	}
	h(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v2/tasks/12", nil))

	rec := httptest.NewRecorder()
	Write(rec, []Gauge{{Name: "ordryn_users", Help: "Accounts.", Labels: map[string]string{"state": `a"b`}, Value: 7}})
	out := rec.Body.String()
	for _, want := range []string{
		`ordryn_http_requests_total{method="GET",route="/tasks/",status="2xx"} 3`,
		`ordryn_http_requests_total{method="POST",route="/tasks/",status="4xx"} 1`,
		`ordryn_http_request_duration_seconds_count{method="GET",route="/tasks/",status="2xx"} 3`,
		`ordryn_http_request_duration_seconds_bucket{le="+Inf",method="GET",route="/tasks/",status="2xx"} 3`,
		`# TYPE ordryn_users gauge`,
		`ordryn_users{state="a\"b"} 7`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestStatusWriterFlushes(t *testing.T) {
	rec := httptest.NewRecorder()
	sw := &statusWriter{ResponseWriter: rec}
	var _ http.Flusher = sw
	sw.Flush()
	if !rec.Flushed {
		t.Fatal("Flush should reach the underlying writer")
	}
}
