package branding

import "strings"

// DefaultSlug names keys and MCP servers when the site has no usable name.
const DefaultSlug = "ordryn"

const maxSlugLen = 24

// Slug turns the site name into lowercase letters, digits, and single dashes
// (at most 24 characters), e.g. "Acme Tasks" -> "acme-tasks". It returns
// DefaultSlug when nothing usable is left. The web client's siteSlug matches it.
func Slug(siteName string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(siteName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
		default:
			dash = true
		}
	}
	s := b.String()
	if len(s) > maxSlugLen {
		s = strings.TrimRight(s[:maxSlugLen], "-")
	}
	if s == "" {
		return DefaultSlug
	}
	return s
}

// KeyPrefix is what new API keys start with, before the underscore: the slug
// without dashes, e.g. "Acme Tasks" -> "acmetasks". Keys are looked up by hash,
// so renaming the site never breaks keys already handed out.
func KeyPrefix(siteName string) string {
	return strings.ReplaceAll(Slug(siteName), "-", "")
}
