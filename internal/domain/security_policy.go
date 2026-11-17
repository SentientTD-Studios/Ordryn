package domain

import (
	"fmt"
	"slices"
	"strings"

	"GoTodo/internal/storage"
)

// Session length limits admins can set.
const (
	MaxSessionIdleMinutes = 30 * 24 * 60
	MaxSessionHours       = 90 * 24
)

// SecurityPolicyInput patches the security policy. Nil fields stay as they are.
type SecurityPolicyInput struct {
	RequireMFA          *string
	MFARoleIDs          *[]int
	MinPasswordLength   *int
	SessionIdleMinutes  *int
	SessionMaxHours     *int
	AllowedEmailDomains *[]string
}

// UpdateSecurityPolicy validates and saves a patch. Only admins reach it.
func UpdateSecurityPolicy(in SecurityPolicyInput) (storage.SecurityPolicy, error) {
	p, err := storage.LoadSecurityPolicy()
	if err != nil {
		return p, err
	}
	if in.RequireMFA != nil {
		switch v := strings.TrimSpace(*in.RequireMFA); v {
		case storage.MFARequireOff, storage.MFARequireAll, storage.MFARequireRoles:
			p.RequireMFA = v
		default:
			return p, fmt.Errorf("%w: require_mfa must be off, all, or roles", ErrValidation)
		}
	}
	if in.MFARoleIDs != nil {
		ids := []int{}
		for _, id := range *in.MFARoleIDs {
			if _, err := storage.GetSiteRole(id); err != nil {
				return p, fmt.Errorf("%w: role %d not found", ErrValidation, id)
			}
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		p.MFARoleIDs = ids
	}
	if p.RequireMFA == storage.MFARequireRoles && len(p.MFARoleIDs) == 0 {
		return p, fmt.Errorf("%w: choose at least one role that must use two-factor authentication", ErrValidation)
	}
	if in.MinPasswordLength != nil {
		n := *in.MinPasswordLength
		if n < storage.PolicyMinPasswordFloor || n > storage.PolicyMinPasswordCeiling {
			return p, fmt.Errorf("%w: minimum password length must be between %d and %d", ErrValidation,
				storage.PolicyMinPasswordFloor, storage.PolicyMinPasswordCeiling)
		}
		p.MinPasswordLength = n
	}
	if in.SessionIdleMinutes != nil {
		n := *in.SessionIdleMinutes
		if n != 0 && (n < 5 || n > MaxSessionIdleMinutes) {
			return p, fmt.Errorf("%w: idle timeout must be 0 (off) or between 5 minutes and 30 days", ErrValidation)
		}
		p.SessionIdleMinutes = n
	}
	if in.SessionMaxHours != nil {
		n := *in.SessionMaxHours
		if n < 0 || n > MaxSessionHours {
			return p, fmt.Errorf("%w: maximum session length must be 0 (default) or up to 90 days", ErrValidation)
		}
		p.SessionMaxHours = n
	}
	if in.AllowedEmailDomains != nil {
		out := []string{}
		for _, d := range *in.AllowedEmailDomains {
			d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
			if d == "" {
				continue
			}
			if strings.ContainsAny(d, " /@") || !strings.Contains(d, ".") {
				return p, fmt.Errorf("%w: %q isn't an email domain", ErrValidation, d)
			}
			if !slices.Contains(out, d) {
				out = append(out, d)
			}
		}
		p.AllowedEmailDomains = out
	}
	if err := storage.SaveSecurityPolicy(p); err != nil {
		return p, err
	}
	return p, nil
}
