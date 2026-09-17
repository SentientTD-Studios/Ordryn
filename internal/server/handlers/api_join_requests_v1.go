package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/live"
	"GoTodo/internal/mailer"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

const joinRequestMessageMax = 500

type joinRequestCreateBody struct {
	Email   string `json:"email"`
	Message string `json:"message"`
}

func joinRequestJSON(jr storage.JoinRequest) map[string]interface{} {
	out := map[string]interface{}{
		"id":         jr.ID,
		"email":      jr.Email,
		"message":    jr.Message,
		"status":     jr.Status,
		"created_at": jr.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if jr.InviteID != nil {
		out["invite_id"] = *jr.InviteID
	}
	if jr.ReviewedAt != nil {
		out["reviewed_at"] = jr.ReviewedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if jr.ReviewedBy != nil {
		out["reviewed_by"] = *jr.ReviewedBy
	}
	if jr.InviteToken != "" {
		out["invite_token"] = jr.InviteToken
	}
	return out
}

func writeJoinRequestOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"message": "If this site accepts join requests, we will be in touch.",
	})
}

// APIV1JoinRequestsCreate handles POST /api/v2/join-requests (public).
func APIV1JoinRequestsCreate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var req joinRequestCreateBody
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	message := strings.TrimSpace(req.Message)
	if email == "" || !strings.Contains(email, "@") {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "A valid email is required.")
		return
	}
	if len(message) > joinRequestMessageMax {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Message must be 500 characters or less.")
		return
	}

	settings, err := storage.GetSiteSettings()
	if err != nil || settings == nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load site settings.")
		return
	}
	if !settings.EnableJoinRequests {
		utils.APIJSONError(w, http.StatusForbidden, "join_requests_disabled",
			"This site is not accepting join requests.")
		return
	}

	exists, err := storage.UserExistsByEmail(email)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	pending, err := storage.HasPendingJoinRequest(email)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	unusedInvite, err := storage.HasUnusedInvite(email)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Internal server error.")
		return
	}
	if exists || pending || unusedInvite {
		writeJoinRequestOK(w)
		return
	}

	if err := storage.CreateJoinRequest(email, message); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to submit request.")
		return
	}

	notifyAdminsOfJoinRequest(r, settings.SiteName, email, message)
	writeJoinRequestOK(w)
}

func notifyAdminsOfJoinRequest(r *http.Request, siteName, email, message string) {
	domain.NotifyAdminsOfJoinRequest(email, message)
	settings, err := storage.GetSiteSettings()
	if err != nil || settings == nil {
		return
	}
	if strings.TrimSpace(siteName) == "" {
		siteName = "GoTodo"
	}
	admins, err := storage.ListAdminEmails()
	if err != nil || len(admins) == 0 {
		return
	}
	adminURL := utils.AbsoluteURLForRequest(r, "/admin/requests")
	body := fmt.Sprintf("%s requested to join %s.\n\n", email, siteName)
	if message != "" {
		body += "Message:\n" + message + "\n\n"
	}
	body += "Review join requests in Admin:\n" + adminURL + "\n"
	subject := fmt.Sprintf("%s - New join request", siteName)
	for _, to := range admins {
		_ = mailer.SendEmail(storage.SiteEmailConfig(settings), mailer.TriggerJoinRequest, subject, body, to)
	}
}

// APIV1AdminJoinRequestsRouter handles /api/v2/admin/join-requests and approve/deny.
func APIV1AdminJoinRequestsRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "admin/join-requests")
	if sub == "" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		list, err := storage.ListJoinRequests()
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to list join requests.")
			return
		}
		out := make([]map[string]interface{}, 0, len(list))
		for _, jr := range list {
			out = append(out, joinRequestJSON(jr))
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(out)
		return
	}

	parts := strings.Split(sub, "/")
	if len(parts) != 2 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid join request path.")
		return
	}
	id, err := strconv.Atoi(parts[0])
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid join request id.")
		return
	}
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	reviewerID, _ := utils.GetAPIUserID(r)
	switch parts[1] {
	case "approve":
		jr, inv, err := storage.ApproveJoinRequest(id, reviewerID)
		if err != nil {
			writeJoinRequestReviewError(w, err)
			return
		}
		emailSiteInvite(r, jr.Email, inv.Token)
		live.AfterJoinReviewed(jr.Email, jr.Message, true)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"request": joinRequestJSON(*jr),
			"invite": map[string]interface{}{
				"id":    inv.ID,
				"email": inv.Email,
				"token": inv.Token,
				"used":  inv.Used,
			},
		})
	case "deny":
		jr, err := storage.DenyJoinRequest(id, reviewerID)
		if err != nil {
			writeJoinRequestReviewError(w, err)
			return
		}
		live.AfterJoinReviewed(jr.Email, jr.Message, false)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":      true,
			"request": joinRequestJSON(*jr),
		})
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Unknown action.")
	}
}

func writeJoinRequestReviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrJoinRequestNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Join request not found.")
	case errors.Is(err, storage.ErrJoinRequestNotPending):
		utils.APIJSONError(w, http.StatusConflict, "not_pending", "Join request is not pending.")
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to update join request.")
	}
}
