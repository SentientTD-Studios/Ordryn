package mailer

import (
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	TriggerPasswordReset   = "password_reset"
	TriggerPasswordChanged = "password_changed"
	TriggerSiteInvite      = "site_invite"
	TriggerJoinRequest     = "join_request"
	TriggerProjectInvite   = "project_invite"

	StatusSent          = "sent"
	StatusFailed        = "failed"
	StatusNotConfigured = "not_configured"
	StatusRateLimited   = "rate_limited"

	maxAuditErrorLen = 1024
)

// AuditEntry is a single outbound email attempt recorded for admins.
type AuditEntry struct {
	Trigger  string
	ToEmail  string
	Status   string
	Error    string
	Provider string
}

// Auditor records an email send attempt. It must not panic or block for long.
type Auditor func(entry AuditEntry)

var (
	auditorMu sync.RWMutex
	auditor   Auditor
)

// SetAuditor registers the sink used after every SendEmail attempt.
func SetAuditor(a Auditor) {
	auditorMu.Lock()
	defer auditorMu.Unlock()
	auditor = a
}

// KnownTrigger reports whether t is a first-class outbound email trigger.
func KnownTrigger(t string) bool {
	switch strings.TrimSpace(t) {
	case TriggerPasswordReset, TriggerPasswordChanged, TriggerSiteInvite,
		TriggerJoinRequest, TriggerProjectInvite:
		return true
	default:
		return false
	}
}

// KnownStatus reports whether s is a stored audit status.
func KnownStatus(s string) bool {
	switch strings.TrimSpace(s) {
	case StatusSent, StatusFailed, StatusNotConfigured, StatusRateLimited:
		return true
	default:
		return false
	}
}

func recordAudit(cfg Config, trigger, toEmail string, sendErr error) {
	auditorMu.RLock()
	a := auditor
	auditorMu.RUnlock()
	if a == nil {
		return
	}

	trigger = strings.TrimSpace(trigger)
	if trigger == "" {
		trigger = "unknown"
	}
	status, errText := classifyAudit(cfg, sendErr)
	entry := AuditEntry{
		Trigger:  trigger,
		ToEmail:  strings.TrimSpace(toEmail),
		Status:   status,
		Error:    errText,
		Provider: strings.ToLower(strings.TrimSpace(cfg.Provider)),
	}

	defer func() {
		if rec := recover(); rec != nil {
			fmt.Printf("email audit: auditor panic: %v\n", rec)
		}
	}()
	a(entry)
}

func classifyAudit(cfg Config, sendErr error) (status, errText string) {
	if sendErr == nil {
		return StatusSent, ""
	}
	errText = truncateAuditError(sendErr.Error())
	if errors.Is(sendErr, ErrRateLimited) {
		return StatusRateLimited, errText
	}
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" || provider == "none" {
		return StatusNotConfigured, errText
	}
	msg := sendErr.Error()
	if strings.Contains(msg, "not configured") || strings.Contains(msg, "unsupported email provider") {
		return StatusNotConfigured, errText
	}
	return StatusFailed, errText
}

func truncateAuditError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxAuditErrorLen {
		return s
	}
	return s[:maxAuditErrorLen]
}
