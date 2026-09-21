package hooks

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const maxTitleRunes = 120
const maxContentRunes = 2000

var tokenRe = regexp.MustCompile(`\{([a-zA-Z_]+)\}`)
var mentionRe = regexp.MustCompile(`(?i)@(everyone|here)\b`)

// Interpolate replaces {tokens}. Unknown tokens become empty.
func Interpolate(tmpl string, vars map[string]string) string {
	return tokenRe.ReplaceAllStringFunc(tmpl, func(m string) string {
		name := strings.Trim(m, "{}")
		if vars == nil {
			return ""
		}
		return vars[strings.ToLower(name)]
	})
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max < 1 {
		return ""
	}
	return string(runes[:max]) + "…"
}

func stripBroadcastMentions(s string) string {
	return strings.TrimSpace(mentionRe.ReplaceAllString(s, ""))
}

func prepareContent(s string) string {
	s = stripBroadcastMentions(s)
	s = strings.TrimSpace(s)
	return truncateRunes(s, maxContentRunes)
}

func truncateTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\n", " ")
	return truncateRunes(s, maxTitleRunes)
}

func priorityLabel(p int) string {
	switch p {
	case 1:
		return "Low"
	case 2:
		return "Medium"
	case 3:
		return "High"
	default:
		return "None"
	}
}
