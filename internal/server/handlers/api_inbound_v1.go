package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type inboundWebhookBody struct {
	ProjectID   int    `json:"project_id"`
	Action      string `json:"action"`
	Title       string `json:"title"`
	Description string `json:"description"`
	TaskID      int    `json:"task_id"`
	Comment     string `json:"comment"`
}

const inboundWebhookSecretHdr = "X-Ordryn-Webhook-Secret"
const inboundSignatureHdr = "X-Ordryn-Signature"

// APIV1InboundWebhook handles POST /api/v1/webhooks/inbound (public).
func APIV1InboundWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Failed to read body.")
		return
	}
	defer r.Body.Close()
	var payload inboundWebhookBody
	if err := json.Unmarshal(body, &payload); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid webhook payload.")
		return
	}
	err = domain.ApplyInboundWebhook(r.Context(), r.Header.Get(inboundWebhookSecretHdr), r.Header.Get(inboundSignatureHdr), body, domain.InboundWebhookInput{
		ProjectID:   payload.ProjectID,
		Action:      payload.Action,
		Title:       payload.Title,
		Description: payload.Description,
		TaskID:      payload.TaskID,
		Comment:     payload.Comment,
	})
	if err != nil {
		writeInboundDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func writeInboundDomainError(w http.ResponseWriter, err error) {
	msg := err.Error()
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrForbidden):
		code := http.StatusForbidden
		if strings.Contains(msg, "provide X-Ordryn") {
			code = http.StatusUnauthorized
		}
		utils.APIJSONError(w, code, "forbidden", strings.TrimPrefix(strings.TrimPrefix(msg, "forbidden: "), "Forbidden."))
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", strings.TrimPrefix(msg, "validation: "))
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}
