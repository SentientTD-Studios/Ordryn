package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type extCallbackBody struct {
	Action  string `json:"action"`
	TaskID  int    `json:"task_id"`
	Comment string `json:"comment"`
	Field   string `json:"field"`
	Value   string `json:"value"`
}

// APIV1ExtCallback handles POST /api/v1/ext/callback (Bearer callback token).
func APIV1ExtCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	token := bearerToken(r)
	if token == "" {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Provide a Bearer callback token.")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Failed to read body.")
		return
	}
	defer r.Body.Close()
	var payload extCallbackBody
	if err := json.Unmarshal(body, &payload); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid callback payload.")
		return
	}
	out, err := domain.ApplyExtensionCallback(r.Context(), token, domain.ExtensionCallbackInput{
		Action:  payload.Action,
		TaskID:  payload.TaskID,
		Comment: payload.Comment,
		Field:   payload.Field,
		Value:   payload.Value,
	})
	if err != nil {
		writeInboundDomainError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func bearerToken(r *http.Request) string {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) < 8 || !strings.EqualFold(h[:7], "bearer ") {
		return ""
	}
	return strings.TrimSpace(h[7:])
}
