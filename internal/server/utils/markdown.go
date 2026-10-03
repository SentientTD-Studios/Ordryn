package utils

import (
	"bytes"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

var markdownPolicy = bluemonday.UGCPolicy()

// markdownConverter renders GFM. Raw HTML is never passed through (goldmark's
// default), and the UGC policy strips anything unsafe, including code classes.
var markdownConverter = goldmark.New(goldmark.WithExtensions(extension.GFM))

// RenderMarkdown converts Markdown to sanitized HTML for task descriptions and comments.
func RenderMarkdown(md string) string {
	if strings.TrimSpace(md) == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := markdownConverter.Convert([]byte(md), &buf); err != nil {
		return ""
	}
	return markdownPolicy.Sanitize(buf.String())
}

// TruncateDescription returns plain text truncated to limit with ellipsis.
func TruncateDescription(text string, limit int) string {
	text = strings.TrimSpace(text)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	if limit <= 3 {
		return text[:limit]
	}
	return text[:limit-3] + "..."
}
