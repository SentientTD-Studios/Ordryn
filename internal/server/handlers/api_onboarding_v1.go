package handlers

import (
	"encoding/json"
	"net/http"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
)

type apiOnboardingStepJSON struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Href        string `json:"href"`
	Done        bool   `json:"done"`
	AdminOnly   bool   `json:"admin_only,omitempty"`
}

type apiOnboardingJSON struct {
	Dismissed bool                    `json:"dismissed"`
	Complete  bool                    `json:"complete"`
	Steps     []apiOnboardingStepJSON `json:"steps"`
}

type apiOnboardingPatch struct {
	Dismissed *bool `json:"dismissed"`
}

// APIV1MeOnboarding handles GET/PATCH /api/v2/me/onboarding.
func APIV1MeOnboarding(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPatch:
		var req apiOnboardingPatch
		if err := decodeJSONBody(r, &req); err != nil || req.Dismissed == nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Send {\"dismissed\": true|false}.")
			return
		}
		if err := domain.SetOnboardingDismissedForUser(r.Context(), userID, *req.Dismissed); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save.")
			return
		}
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	ob, err := domain.GetOnboardingForUser(r.Context(), userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load onboarding.")
		return
	}
	out := apiOnboardingJSON{Dismissed: ob.Dismissed, Complete: ob.Complete, Steps: make([]apiOnboardingStepJSON, 0, len(ob.Steps))}
	for _, s := range ob.Steps {
		out.Steps = append(out.Steps, apiOnboardingStepJSON{
			ID: s.ID, Label: s.Label, Description: s.Description, Href: s.Href, Done: s.Done, AdminOnly: s.AdminOnly,
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}
