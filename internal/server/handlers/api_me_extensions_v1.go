package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"GoTodo/internal/config"
	"GoTodo/internal/domain"
	"GoTodo/internal/extensions"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// APIV1MeExtensions handles personal-inbox hook destinations.
func APIV1MeExtensions(w http.ResponseWriter, r *http.Request) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	sub := strings.Trim(utils.ParseAPIV1Subpath(r, "me/extensions"), "/")
	if sub == "" {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		personalExtensionsList(w, userID)
		return
	}
	parts := strings.Split(sub, "/")
	extensionID := strings.TrimSpace(parts[0])
	if extensionID == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid extension id.")
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		projectMemberExtensionGet(w, 0, extensionID, userID)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodPatch {
		projectMemberExtensionPatch(w, r, 0, extensionID, userID)
		return
	}
	if len(parts) == 2 && parts[1] == "test" && r.Method == http.MethodPost {
		projectExtensionTest(w, r, 0, userID, extensionID, "Inbox")
		return
	}
	utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
}

func personalExtensionsList(w http.ResponseWriter, userID int) {
	entries := extensions.Snapshot()
	out := make([]projectExtensionJSON, 0)
	for _, e := range entries {
		if !e.Loaded || e.Manifest.Delivery == nil || !e.Manifest.HasProjectSettings() {
			continue
		}
		item, err := projectExtensionFromEntry(e, 0, userID, false)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
			return
		}
		out = append(out, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(projectExtensionsListJSON{Extensions: out})
}

type projectInboundJSON struct {
	Enabled        bool    `json:"enabled"`
	AllowCreate    bool    `json:"allow_create"`
	AllowComment   bool    `json:"allow_comment"`
	SecretSet      bool    `json:"secret_set"`
	Secret         string  `json:"secret,omitempty"`
	URL            string  `json:"url"`
	LastError      string  `json:"last_error,omitempty"`
	LastDeliveryAt *string `json:"last_delivery_at,omitempty"`
}

func apiV1ProjectInbound(w http.ResponseWriter, r *http.Request, projectID int) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	if _, err := domain.RequireProjectExtensionOwner(userID, projectID); err != nil {
		writeProjectExtensionError(w, err)
		return
	}
	switch r.Method {
	case http.MethodGet:
		cfg, err := storage.GetProjectInboundWebhook(projectID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load inbound webhook.")
			return
		}
		writeInboundJSON(w, cfg, "")
	case http.MethodPatch:
		var req struct {
			Enabled      *bool `json:"enabled"`
			AllowCreate  *bool `json:"allow_create"`
			AllowComment *bool `json:"allow_comment"`
			RotateSecret *bool `json:"rotate_secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
			return
		}
		cur, err := storage.GetProjectInboundWebhook(projectID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load inbound webhook.")
			return
		}
		enabled, allowCreate, allowComment := cur.Enabled, cur.AllowCreate, cur.AllowComment
		if req.Enabled != nil {
			enabled = *req.Enabled
		}
		if req.AllowCreate != nil {
			allowCreate = *req.AllowCreate
		}
		if req.AllowComment != nil {
			allowComment = *req.AllowComment
		}
		shown := ""
		newSecret := ""
		if (req.RotateSecret != nil && *req.RotateSecret) || !cur.SecretSet {
			shown = randomInboundSecret()
			newSecret = shown
		}
		cfg, err := storage.UpsertProjectInboundWebhook(projectID, enabled, allowCreate, allowComment, newSecret)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save inbound webhook.")
			return
		}
		writeInboundJSON(w, cfg, shown)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func writeInboundJSON(w http.ResponseWriter, cfg *storage.ProjectInboundWebhook, shownSecret string) {
	if cfg == nil {
		cfg = &storage.ProjectInboundWebhook{AllowCreate: true, AllowComment: true}
	}
	out := projectInboundJSON{
		Enabled:      cfg.Enabled,
		AllowCreate:  cfg.AllowCreate,
		AllowComment: cfg.AllowComment,
		SecretSet:    cfg.SecretSet,
		Secret:       shownSecret,
		URL:          publicInboundURL(),
		LastError:    cfg.LastError,
	}
	if cfg.LastDeliveryAt != nil {
		s := cfg.LastDeliveryAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		out.LastDeliveryAt = &s
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func publicInboundURL() string {
	base := strings.TrimSpace(os.Getenv("PUBLIC_URL"))
	if base == "" {
		base = strings.TrimSpace(config.Cfg.BasePath)
	}
	if !strings.Contains(base, "://") {
		return "/api/v1/webhooks/inbound"
	}
	return strings.TrimSuffix(base, "/") + "/api/v1/webhooks/inbound"
}

func randomInboundSecret() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte("inbound-secret"))
	}
	return hex.EncodeToString(b)
}
