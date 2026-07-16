package handlers

import (
	"GoTodo/internal/config"
	"GoTodo/internal/server/utils"
	"crypto/tls"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCalendarFeedURLForRequest(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		host     string
		tls      bool
		useHTTPS bool
		token    string
		want     string
	}{
		{
			name:     "root path prefix",
			basePath: "/",
			host:     "example.com",
			token:    "abc123",
			want:     "http://example.com/cal/abc123.ics",
		},
		{
			name:     "subpath prefix",
			basePath: "/gotodo",
			host:     "example.com",
			token:    "abc123",
			want:     "http://example.com/gotodo/cal/abc123.ics",
		},
		{
			name:     "full url base path",
			basePath: "http://localhost:8080",
			host:     "ignored.example",
			token:    "abc123",
			want:     "http://localhost:8080/cal/abc123.ics",
		},
		{
			name:     "https via tls",
			basePath: "/",
			host:     "secure.example",
			tls:      true,
			token:    "tok",
			want:     "https://secure.example/cal/tok.ics",
		},
		{
			name:     "useHttps config",
			basePath: "/",
			host:     "example.com",
			useHTTPS: true,
			token:    "tok",
			want:     "https://example.com/cal/tok.ics",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := utils.BasePath
			origHTTPS := config.Cfg.UseHTTPS
			t.Cleanup(func() {
				utils.BasePath = orig
				config.Cfg.UseHTTPS = origHTTPS
			})
			utils.BasePath = tt.basePath
			config.Cfg.UseHTTPS = tt.useHTTPS

			req := httptest.NewRequest("GET", "/profile", nil)
			req.Host = tt.host
			if tt.tls {
				req.TLS = &tls.ConnectionState{}
			}

			got := calendarFeedURLForRequest(req, tt.token)
			if got != tt.want {
				t.Errorf("calendarFeedURLForRequest() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseCalendarTokenFromPath(t *testing.T) {
	orig := utils.BasePath
	t.Cleanup(func() { utils.BasePath = orig })
	utils.BasePath = "/gotodo"

	if got := parseCalendarTokenFromPath("/gotodo/cal/abc.ics"); got != "abc" {
		t.Fatalf("subpath token = %q", got)
	}
	if got := parseCalendarTokenFromPath("/cal/xyz.ics"); got != "xyz" {
		t.Fatalf("root token = %q", got)
	}
}

func TestWriteEmptyICS(t *testing.T) {
	rec := httptest.NewRecorder()
	writeEmptyICS(rec)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "private, no-store" {
		t.Fatalf("cache-control %q", cc)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "BEGIN:VCALENDAR") || !strings.Contains(body, "END:VCALENDAR") {
		t.Fatalf("body: %s", body)
	}
}

func TestPlainDescriptionForICS(t *testing.T) {
	got := plainDescriptionForICS("**bold** text")
	if got == "" || strings.Contains(got, "**") {
		t.Fatalf("got %q", got)
	}
}

func TestICSEscapeAndFold(t *testing.T) {
	if got := icsEscape("a;b,c\nline"); got != `a\;b\,c\nline` {
		t.Fatalf("escape %q", got)
	}
	var b strings.Builder
	icsWriteFolded(&b, "SUMMARY", strings.Repeat("x", 100))
	if !strings.Contains(b.String(), "\r\n ") {
		t.Fatal("expected folded line")
	}
}

func TestParseGoTodoUID(t *testing.T) {
	id, uid, ok := parseGoTodoUID("gotodo-u5-t12@gotodo")
	if !ok || id != 12 || uid != 5 {
		t.Fatalf("got id=%d uid=%d ok=%v", id, uid, ok)
	}
}

func TestRequestSchemeForwardedProto(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := requestScheme(req); got != "https" {
		t.Fatalf("got %q", got)
	}
}

func TestParseICSEvents(t *testing.T) {
	ics := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:gotodo-u1-t9@gotodo
DTSTART;VALUE=DATE:20260715
END:VEVENT
END:VCALENDAR`
	events := parseICSEvents(ics)
	if len(events) != 1 || events[0].StartDate != "20260715" {
		t.Fatalf("events %+v", events)
	}
}
