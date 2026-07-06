package handlers

import (
	"GoTodo/internal/server/utils"
	"crypto/tls"
	"net/http/httptest"
	"testing"
)

func TestCalendarFeedURLForRequest(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		host     string
		tls      bool
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
			name:     "full url base path with trailing slash",
			basePath: "http://localhost:8080/",
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := utils.BasePath
			t.Cleanup(func() { utils.BasePath = orig })
			utils.BasePath = tt.basePath

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
