package utils

import (
	"strings"
	"testing"
)

func TestRenderMarkdownStripsScript(t *testing.T) {
	html := RenderMarkdown("Hello **world**\n\n<script>alert(1)</script>")
	if strings.Contains(html, "<script>") {
		t.Fatalf("expected script tag stripped, got: %s", html)
	}
	if !strings.Contains(html, "<strong>world</strong>") {
		t.Fatalf("expected bold markdown rendered, got: %s", html)
	}
}

func TestTruncateDescription(t *testing.T) {
	if got := TruncateDescription("hello world", 20); got != "hello world" {
		t.Fatalf("short text unchanged: %q", got)
	}
	if got := TruncateDescription("abcdefghijklmnopqrstuvwxyz", 10); got != "abcdefg..." {
		t.Fatalf("expected ellipsis truncation, got %q", got)
	}
}
