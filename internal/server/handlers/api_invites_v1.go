package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type inviteCreateRequest struct {
	Email            string  `json:"email"`
	ExpiresAt        *string `json:"expires_at"`
	BypassExpiration bool    `json:"bypass_expiration"`
}

func inviteToJSON(inv storage.Invite) map[string]interface{} {
	m := map[string]interface{}{
		"id":         inv.ID,
		"email":      inv.Email,
		"token":      inv.Token,
		"used":       inv.Used,
		"status":     inv.Status(),
		"created_at": inv.CreatedAt.UTC().Format(time.RFC3339),
	}
	if inv.ExpiresAt != nil {
		m["expires_at"] = inv.ExpiresAt.UTC().Format(time.RFC3339)
	} else {
		m["expires_at"] = nil
	}
	if inv.CreatedBy != nil {
		m["created_by"] = *inv.CreatedBy
	}
	if inv.CreatorUserName != "" {
		m["creator_user_name"] = inv.CreatorUserName
	}
	if inv.CreatorEmail != "" {
		m["creator_email"] = inv.CreatorEmail
	}
	return m
}

func parseInviteExpiration(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			if l == "2006-01-02" {
				t = time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, time.UTC)
			}
			return &t, nil
		}
	}
	return nil, fmt.Errorf("invalid expiration date format")
}

func isUserAdmin(r *http.Request, userID int) bool {
	if _, _, perms, loggedIn := utils.GetSessionUser(r); loggedIn {
		for _, p := range perms {
			if p == "admin" {
				return true
			}
		}
	} else if profile, err := storage.GetUserProfileByID(userID); err == nil {
		for _, p := range profile.Permissions {
			if p == "admin" {
				return true
			}
		}
	}
	return false
}

// APIV1InvitesRouter handles user-scoped /api/v2/invites and /api/v2/invites/{id}.
func APIV1InvitesRouter(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	isAdmin := isUserAdmin(r, userID)

	sub := utils.ParseAPIV1Subpath(r, "invites")
	if sub == "" {
		switch r.Method {
		case http.MethodGet:
			invites, err := storage.ListInvitesForUser(userID)
			if err != nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list invites.")
				return
			}
			out := make([]map[string]interface{}, 0, len(invites))
			for _, inv := range invites {
				out = append(out, inviteToJSON(inv))
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(out)
		case http.MethodPost:
			settings, err := storage.GetSiteSettings()
			if err != nil || settings == nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load site settings.")
				return
			}

			if !isAdmin {
				if !settings.AllowUserInvites {
					utils.APIJSONError(w, http.StatusForbidden, "user_invites_disabled", "User invites are not enabled.")
					return
				}
				if settings.UserInviteLimit > 0 {
					count, err := storage.CountActiveInvitesByUser(userID)
					if err == nil && count >= settings.UserInviteLimit {
						utils.APIJSONError(w, http.StatusBadRequest, "invite_limit_reached",
							fmt.Sprintf("You have reached your limit of %d invites.", settings.UserInviteLimit))
						return
					}
				}
			}

			var req inviteCreateRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}

			var expiresAt *time.Time
			if isAdmin {
				if req.BypassExpiration {
					expiresAt = nil
				} else if req.ExpiresAt != nil && *req.ExpiresAt != "" {
					parsed, err := parseInviteExpiration(*req.ExpiresAt)
					if err != nil {
						utils.APIJSONError(w, http.StatusBadRequest, "invalid_expiration", "Invalid expiration date.")
						return
					}
					if parsed != nil && time.Now().After(*parsed) {
						utils.APIJSONError(w, http.StatusBadRequest, "invalid_expiration", "Expiration date must be in the future.")
						return
					}
					expiresAt = parsed
				} else if settings.InviteExpirationDays > 0 {
					t := time.Now().AddDate(0, 0, settings.InviteExpirationDays)
					expiresAt = &t
				}
			} else {
				// Regular user: ONLY admins can bypass the expire date!
				if req.BypassExpiration {
					utils.APIJSONError(w, http.StatusForbidden, "cannot_bypass_expiration", "Only admins can bypass the invite expiration limit.")
					return
				}
				if settings.InviteExpirationDays > 0 {
					maxExpiry := time.Now().AddDate(0, 0, settings.InviteExpirationDays)
					if req.ExpiresAt != nil && *req.ExpiresAt != "" {
						parsed, err := parseInviteExpiration(*req.ExpiresAt)
						if err != nil {
							utils.APIJSONError(w, http.StatusBadRequest, "invalid_expiration", "Invalid expiration date.")
							return
						}
						if parsed != nil && time.Now().After(*parsed) {
							utils.APIJSONError(w, http.StatusBadRequest, "invalid_expiration", "Expiration date must be in the future.")
							return
						}
						if parsed != nil && parsed.After(maxExpiry.Add(24*time.Hour)) {
							utils.APIJSONError(w, http.StatusForbidden, "cannot_bypass_expiration",
								fmt.Sprintf("Only admins can bypass the invite expiration limit of %d days.", settings.InviteExpirationDays))
							return
						}
						expiresAt = parsed
					} else {
						expiresAt = &maxExpiry
					}
				} else {
					expiresAt = nil
				}
			}

			inv, err := storage.CreateInvite(req.Email, &userID, expiresAt, false)
			if err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			emailSiteInvite(r, inv.Email, inv.Token)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(inviteToJSON(*inv))
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}
	id, err := strconv.Atoi(strings.Trim(sub, "/"))
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid invite id.")
		return
	}
	if r.Method != http.MethodDelete {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	if err := storage.DeleteInvite(id, userID, isAdmin); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
