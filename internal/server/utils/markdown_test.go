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

func TestRenderMarkdownImages(t *testing.T) {
	html := RenderMarkdown("See ![cat](https://cdn.example.com/cat.png)")
	if !strings.Contains(html, `<img`) {
		t.Fatalf("expected img tag, got: %s", html)
	}
	if !strings.Contains(html, `src="https://cdn.example.com/cat.png"`) {
		t.Fatalf("expected image src, got: %s", html)
	}
	if strings.Contains(html, "javascript:") {
		t.Fatal("javascript URL should not survive")
	}
	unsafe := RenderMarkdown(`![x](javascript:alert(1))`)
	if strings.Contains(unsafe, "javascript:") {
		t.Fatalf("javascript src leaked: %s", unsafe)
	}
}

func TestRenderMarkdownEscapesCodeBlocks(t *testing.T) {
	cases := []string{
		"```html\n<script>alert(1)</script>\n```",
		"```js\n</code></pre><img src=x onerror=alert(1)>\n```",
		"```\"><img src=x onerror=alert(1)>\nx\n```",
		"    <img src=x onerror=alert(1)>",
		"a `<img src=x onerror=alert(1)>` b",
		"- [ ] <img src=x onerror=alert(1)>",
		"| a |\n|---|\n| <script>alert(1)</script> |",
	}
	for _, md := range cases {
		html := RenderMarkdown(md)
		for _, bad := range []string{"<script", "<img"} {
			if strings.Contains(html, bad) {
				t.Fatalf("%q leaked %q: %s", md, bad, html)
			}
		}
	}
	html := RenderMarkdown("```go\nx := 1 < 2\n```")
	if !strings.Contains(html, "<pre><code>x := 1 &lt; 2") {
		t.Fatalf("expected escaped code block, got: %s", html)
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
