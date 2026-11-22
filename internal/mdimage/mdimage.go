// Package mdimage pulls inline images out of comment markdown so plain-text
// surfaces (notifications, chat, webhooks) can show the text and the images
// separately instead of leaking raw `![alt](url)` syntax.
package mdimage

import (
	"regexp"
	"strings"
)

// imageRe matches `![alt](url)` and `![alt](<url> "title")`.
var imageRe = regexp.MustCompile(`!\[([^\]\n]*)\]\(\s*<?([^\s<>()]+)>?(?:\s+(?:"[^"\n]*"|'[^'\n]*'))?\s*\)`)

var blankLinesRe = regexp.MustCompile(`\n[ \t]*\n(?:[ \t]*\n)+`)

// Split removes markdown images from s and returns the remaining text and the
// image URLs in order (deduplicated). Only http(s) and /uploads/ URLs are
// returned; other images are dropped from the text all the same.
func Split(s string) (string, []string) {
	if !strings.Contains(s, "![") {
		return s, nil
	}
	var urls []string
	seen := map[string]bool{}
	text := imageRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := imageRe.FindStringSubmatch(m)
		if u := strings.TrimSpace(sub[2]); SafeURL(u) && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
		return ""
	})
	text = blankLinesRe.ReplaceAllString(text, "\n\n")
	return strings.TrimSpace(text), urls
}

// SafeURL reports whether u is an image URL other services may be given.
func SafeURL(u string) bool {
	switch {
	case strings.HasPrefix(u, "https://"), strings.HasPrefix(u, "http://"):
		return true
	case strings.HasPrefix(u, "/uploads/"):
		return !strings.Contains(u, "..")
	}
	return false
}

// Absolute resolves a site-relative /uploads/ URL against base (no trailing
// slash). It returns "" when u is relative and base is unknown.
func Absolute(u, base string) string {
	if strings.HasPrefix(u, "/") {
		if base == "" {
			return ""
		}
		return base + u
	}
	return u
}

// Placeholder is the text shown when a message held only images.
func Placeholder(n int) string {
	if n == 1 {
		return "📷 Image"
	}
	return "📷 Images"
}
