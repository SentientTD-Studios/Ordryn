package utils

import "strings"

// AllowedTimezones is the set of IANA timezones accepted by profile APIs.
var AllowedTimezones = []string{
	"UTC",
	"America/New_York",
	"America/Chicago",
	"America/Denver",
	"America/Los_Angeles",
	"America/Anchorage",
	"America/Phoenix",
	"America/Toronto",
	"America/Vancouver",
	"America/Mexico_City",
	"America/Sao_Paulo",
	"America/Buenos_Aires",
	"Europe/London",
	"Europe/Paris",
	"Europe/Berlin",
	"Europe/Amsterdam",
	"Europe/Madrid",
	"Europe/Rome",
	"Europe/Stockholm",
	"Europe/Moscow",
	"Asia/Dubai",
	"Asia/Kolkata",
	"Asia/Bangkok",
	"Asia/Shanghai",
	"Asia/Hong_Kong",
	"Asia/Singapore",
	"Asia/Tokyo",
	"Asia/Seoul",
	"Australia/Sydney",
	"Australia/Melbourne",
	"Australia/Perth",
	"Pacific/Auckland",
	"Pacific/Honolulu",
	"Africa/Cairo",
	"Africa/Johannesburg",
}

var allowedTimezoneSet map[string]struct{}

func init() {
	allowedTimezoneSet = make(map[string]struct{}, len(AllowedTimezones))
	for _, tz := range AllowedTimezones {
		allowedTimezoneSet[tz] = struct{}{}
	}
}

// IsValidTimezone returns true if tz is in the allowed list.
func IsValidTimezone(tz string) bool {
	_, ok := allowedTimezoneSet[strings.TrimSpace(tz)]
	return ok
}

// ValidItemsPerPage returns true for supported pagination sizes.
func ValidItemsPerPage(n int) bool {
	switch n {
	case 10, 15, 25, 50:
		return true
	default:
		return false
	}
}
