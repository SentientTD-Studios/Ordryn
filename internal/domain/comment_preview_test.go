package domain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommentPreviewStripsImages(t *testing.T) {
	body, img := commentPreview("Fixed it\n\n![image](https://cdn.example.com/a.png)")
	if body != "Fixed it" || img != "https://cdn.example.com/a.png" {
		t.Fatalf("got %q %q", body, img)
	}
	body, img = commentPreview("![image](/uploads/a.png)")
	if body != "📷 Image" || img != "/uploads/a.png" {
		t.Fatalf("image-only: got %q %q", body, img)
	}
	body, img = commentPreview("")
	if body != "New comment" || img != "" {
		t.Fatalf("empty: got %q %q", body, img)
	}
	long := strings.Repeat("é", 200) + " ![x](https://cdn.example.com/a.png)"
	body, _ = commentPreview(long)
	if !utf8.ValidString(body) || strings.Contains(body, "![") || utf8.RuneCountInString(body) > maxCommentPreview {
		t.Fatalf("long: got %q", body)
	}
}
