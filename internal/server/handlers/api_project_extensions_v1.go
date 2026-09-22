package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/extensions"
	"GoTodo/internal/hooks"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type projectExtensionJSON struct {
	ID                string                           `json:"id"`
	Name              string                           `json:"name"`
	Version           string                           `json:"version"`
	HostAPI           int                              `json:"host_api"`
	SiteEnabled       bool                             `json:"site_enabled"`
	Manifest          extensions.Manifest              `json:"manifest"`
	Settings          storage.ExtensionProjectSettings `json:"settings"`
	Secrets           map[string]bool                  `json:"secrets"`
	Member            storage.ExtensionMemberSettings  `json:"member"`
	MemberSecrets     map[string]bool                  `json:"member_secrets"`
	SigningSet        bool                             `json:"signing_set"`
	MemberSigningSet  bool                             `json:"member_signing_set"`
	SigningSecret     string                           `json:"signing_secret,omitempty"`
	CallbackToken     string                           `json:"callback_token,omitempty"`
	SampleJSON        string                           `json:"sample_json,omitempty"`
	CallbackSet       bool                             `json:"callback_set,omitempty"`
	MemberCallbackSet bool                             `json:"member_callback_set,omitempty"`
	Deliveries        []extensionDeliveryJSON          `json:"deliveries,omitempty"`
	MemberDeliveries  []extensionDeliveryJSON          `json:"member_deliveries,omitempty"`
	DestinationHost   string                           `json:"destination_host,omitempty"`
}

type extensionDeliveryJSON struct {
	ID            int64  `json:"id"`
	Event         string `json:"event"`
	EventID       string `json:"event_id,omitempty"`
	Host          string `json:"host,omitempty"`
	Status        string `json:"status"`
	HTTPCode      int    `json:"http_code,omitempty"`
	Error         string `json:"error,omitempty"`
	Attempts      int    `json:"attempts"`
	CreatedAt     string `json:"created_at"`
	NextAttemptAt string `json:"next_attempt_at,omitempty"`
}

type projectExtensionsListJSON struct {
	Extensions []projectExtensionJSON `json:"extensions"`
	IsOwner    bool                   `json:"is_owner"`
}

type projectExtensionPatch struct {
	Enabled          *bool             `json:"enabled"`
	Triggers         *[]string         `json:"triggers"`
	Templates        map[string]string `json:"templates"`
	StatusOnly       *bool             `json:"status_only"`
	SkipSelf         *bool             `json:"skip_self"`
	MinPriority      *int              `json:"min_priority"`
	TagIDs           *[]int            `json:"tag_ids"`
	ClaimedOnly      *bool             `json:"claimed_only"`
	ClaimedIsMe      *bool             `json:"claimed_is_me"`
	FieldKey         *string           `json:"field_key"`
	FieldValue       *string           `json:"field_value"`
	QuietHoursStart  *string           `json:"quiet_hours_start"`
	QuietHoursEnd    *string           `json:"quiet_hours_end"`
	Digest           *string           `json:"digest"`
	StatusIDs        *[]int            `json:"status_ids"`
	StatusExcludeIDs *[]int            `json:"status_exclude_ids"`
	MentionMap       map[string]string `json:"mention_map"`
	WebhookURL       *string           `json:"webhook_url"`
	NtfyAuth         *string           `json:"ntfy_auth"`
	RotateSigning    *bool             `json:"rotate_signing"`
	RotateCallback   *bool             `json:"rotate_callback"`
	Values           map[string]string `json:"values"`
}

func apiV1ProjectExtensions(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	proj, err := domain.RequireProjectExtensionMember(userID, projectID)
	if err != nil {
		writeProjectExtensionError(w, err)
		return
	}
	isOwner := storage.RoleCanManage(proj.Role)

	if len(rest) == 0 {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		projectExtensionsList(w, projectID, userID, isOwner, proj.WorkflowMode)
		return
	}

	extensionID := strings.TrimSpace(rest[0])
	if extensionID == "" {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid extension id.")
		return
	}
	if len(rest) == 1 {
		if r.Method != http.MethodPatch {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if !isOwner {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only the project owner can manage the team webhook.")
			return
		}
		projectExtensionPatchHandler(w, r, projectID, extensionID, userID, true, proj.WorkflowMode)
		return
	}
	if rest[1] == "ui" {
		projectExtensionUI(w, r, projectID, extensionID, rest[2:])
		return
	}
	if rest[1] == "store" {
		projectExtensionStore(w, r, projectID, extensionID, userID, rest[2:])
		return
	}
	if rest[1] == "test" && len(rest) == 2 {
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if !isOwner {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only the project owner can manage the team webhook.")
			return
		}
		projectExtensionTest(w, r, projectID, 0, extensionID, proj.Name)
		return
	}
	if rest[1] == "me" {
		if len(rest) == 2 && r.Method == http.MethodGet {
			projectMemberExtensionGet(w, projectID, extensionID, userID)
			return
		}
		if len(rest) == 2 && r.Method == http.MethodPatch {
			projectMemberExtensionPatch(w, r, projectID, extensionID, userID)
			return
		}
		if len(rest) == 3 && rest[2] == "test" && r.Method == http.MethodPost {
			projectExtensionTest(w, r, projectID, userID, extensionID, proj.Name)
			return
		}
	}
	if rest[1] == "deliveries" && len(rest) == 4 && rest[3] == "retry" && r.Method == http.MethodPost {
		if !isOwner {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "Only the project owner can retry the team webhook.")
			return
		}
		projectExtensionRetry(w, r, projectID, 0, extensionID, rest[2])
		return
	}
	if rest[1] == "me" && len(rest) == 5 && rest[2] == "deliveries" && rest[4] == "retry" && r.Method == http.MethodPost {
		projectExtensionRetry(w, r, projectID, userID, extensionID, rest[3])
		return
	}
	utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
}

func projectExtensionsList(w http.ResponseWriter, projectID, userID int, isOwner bool, workflowMode string) {
	entries := extensions.Snapshot()
	out := make([]projectExtensionJSON, 0)
	for _, e := range entries {
		ok, err := projectExtensionVisible(e, workflowMode)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
			return
		}
		if !ok {
			continue
		}
		item, err := projectExtensionFromEntry(e, projectID, userID, isOwner, workflowMode)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
			return
		}
		out = append(out, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(projectExtensionsListJSON{Extensions: out, IsOwner: isOwner})
}

func projectExtensionVisible(e extensions.Entry, workflowMode string) (bool, error) {
	if !e.Loaded || !e.Manifest.VisibleOnProject(workflowMode) {
		return false, nil
	}
	site, err := storage.GetExtensionSettings(e.ID)
	if err != nil {
		return false, err
	}
	return site.Enabled, nil
}

func applyHookFiltersToProject(m extensions.Manifest, cur *storage.ExtensionProjectSettings, req projectExtensionPatch, scope string) {
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if req.StatusOnly != nil && m.HasSettingForScope("status_only", scope) {
		cur.StatusOnly = *req.StatusOnly
	}
	if req.SkipSelf != nil && m.HasSettingForScope("skip_self", scope) {
		cur.SkipSelf = *req.SkipSelf
	}
	if req.MinPriority != nil && m.HasSettingForScope("min_priority", scope) {
		cur.MinPriority = *req.MinPriority
	}
	if req.TagIDs != nil && m.HasSettingForScope("tag_ids", scope) {
		cur.TagIDs = *req.TagIDs
	}
	if req.StatusIDs != nil && m.HasSettingForScope("status_ids", scope) {
		cur.StatusIDs = *req.StatusIDs
	}
	if req.StatusExcludeIDs != nil && m.HasSettingForScope("status_exclude_ids", scope) {
		cur.StatusExcludeIDs = *req.StatusExcludeIDs
	}
	if req.ClaimedOnly != nil && m.HasSettingForScope("claimed_only", scope) {
		cur.ClaimedOnly = *req.ClaimedOnly
	}
	if (req.FieldKey != nil || req.FieldValue != nil) && (m.HasSettingForScope("field_key", scope) || m.HasSettingForScope("field_value", scope) || m.HasSettingForScope("field_filter", scope)) {
		if req.FieldKey != nil {
			cur.FieldKey = strings.TrimSpace(*req.FieldKey)
		}
		if req.FieldValue != nil {
			cur.FieldValue = strings.TrimSpace(*req.FieldValue)
		}
	}
	if req.QuietHoursStart != nil && m.HasSettingForScope("quiet_hours_start", scope) {
		cur.QuietHoursStart = strings.TrimSpace(*req.QuietHoursStart)
	}
	if req.QuietHoursEnd != nil && m.HasSettingForScope("quiet_hours_end", scope) {
		cur.QuietHoursEnd = strings.TrimSpace(*req.QuietHoursEnd)
	}
	if req.Digest != nil && m.HasSettingForScope("digest", scope) {
		cur.Digest = strings.ToLower(strings.TrimSpace(*req.Digest))
	}
	if req.MentionMap != nil && m.HasSettingForScope("mention_map", scope) {
		cur.MentionMap = req.MentionMap
	}
	applySettingValues(m, &cur.Values, req.Values, scope)
}

func applyHookFiltersToMember(m extensions.Manifest, cur *storage.ExtensionMemberSettings, req projectExtensionPatch) {
	scope := extensions.ScopeUser
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if req.StatusOnly != nil && m.HasSettingForScope("status_only", scope) {
		cur.StatusOnly = *req.StatusOnly
	}
	if req.SkipSelf != nil && m.HasSettingForScope("skip_self", scope) {
		cur.SkipSelf = req.SkipSelf
	}
	if req.MinPriority != nil && m.HasSettingForScope("min_priority", scope) {
		cur.MinPriority = *req.MinPriority
	}
	if req.TagIDs != nil && m.HasSettingForScope("tag_ids", scope) {
		cur.TagIDs = *req.TagIDs
	}
	if req.StatusIDs != nil && m.HasSettingForScope("status_ids", scope) {
		cur.StatusIDs = *req.StatusIDs
	}
	if req.StatusExcludeIDs != nil && m.HasSettingForScope("status_exclude_ids", scope) {
		cur.StatusExcludeIDs = *req.StatusExcludeIDs
	}
	if req.ClaimedOnly != nil && m.HasSettingForScope("claimed_only", scope) {
		cur.ClaimedOnly = *req.ClaimedOnly
	}
	if req.ClaimedIsMe != nil && m.HasSettingForScope("claimed_is_me", scope) {
		cur.ClaimedIsMe = *req.ClaimedIsMe
	}
	if (req.FieldKey != nil || req.FieldValue != nil) && (m.HasSettingForScope("field_key", scope) || m.HasSettingForScope("field_value", scope) || m.HasSettingForScope("field_filter", scope)) {
		if req.FieldKey != nil {
			cur.FieldKey = strings.TrimSpace(*req.FieldKey)
		}
		if req.FieldValue != nil {
			cur.FieldValue = strings.TrimSpace(*req.FieldValue)
		}
	}
	if req.QuietHoursStart != nil && m.HasSettingForScope("quiet_hours_start", scope) {
		cur.QuietHoursStart = strings.TrimSpace(*req.QuietHoursStart)
	}
	if req.QuietHoursEnd != nil && m.HasSettingForScope("quiet_hours_end", scope) {
		cur.QuietHoursEnd = strings.TrimSpace(*req.QuietHoursEnd)
	}
	if req.Digest != nil && m.HasSettingForScope("digest", scope) {
		cur.Digest = strings.ToLower(strings.TrimSpace(*req.Digest))
	}
	applySettingValues(m, &cur.Values, req.Values, scope)
}

func applySettingValues(m extensions.Manifest, dest *map[string]string, req map[string]string, scope string) {
	if req == nil {
		return
	}
	allowed := m.ValueSettingKeysForScope(scope)
	if *dest == nil {
		*dest = map[string]string{}
	}
	for k, v := range req {
		if _, ok := allowed[k]; !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			delete(*dest, k)
			continue
		}
		(*dest)[k] = v
	}
}

func saveDeliverySecrets(w http.ResponseWriter, e extensions.Entry, projectID, userID int, req projectExtensionPatch, scope string) (shownSigning, shownCallback string, ok bool) {
	key := "webhook_url"
	if e.Manifest.Delivery != nil {
		if k := e.Manifest.Delivery.DestinationKey(); k != "" {
			key = k
		}
	}
	if req.WebhookURL != nil && strings.TrimSpace(*req.WebhookURL) != "" {
		allowDest := e.Manifest.HasSetting(key)
		if userID == 0 {
			allowDest = e.Manifest.HasSettingForScope(key, scope)
		}
		if allowDest {
			url := strings.TrimSpace(*req.WebhookURL)
			if e.Manifest.Delivery != nil {
				if err := hooks.ValidateDeliveryURL(e.Manifest.Delivery, url); err != nil {
					utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error()+".")
					return "", "", false
				}
			}
			if err := storage.SetExtensionSecretForUser(e.ID, projectID, userID, key, url); err != nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save webhook URL.")
				return "", "", false
			}
		}
	}
	if req.NtfyAuth != nil && strings.TrimSpace(*req.NtfyAuth) != "" {
		allowAuth := e.Manifest.HasSetting("ntfy_auth")
		if userID == 0 {
			allowAuth = e.Manifest.HasSettingForScope("ntfy_auth", scope)
		}
		if allowAuth {
			if err := storage.SetExtensionSecretForUser(e.ID, projectID, userID, "ntfy_auth", strings.TrimSpace(*req.NtfyAuth)); err != nil {
				utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save ntfy token.")
				return "", "", false
			}
		}
	}
	if req.RotateSigning != nil && *req.RotateSigning {
		if !e.Manifest.HasControl(extensions.ControlRotateSigning) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "This extension does not expose signing secret rotation.")
			return "", "", false
		}
		sec, err := hooks.RotateSigningSecret(e.ID, projectID, userID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to rotate signing secret.")
			return "", "", false
		}
		shownSigning = sec
	}
	if req.RotateCallback != nil && *req.RotateCallback {
		if !e.Manifest.HasControl(extensions.ControlRotateCallback) && !e.Manifest.HasCallbackPermissions() {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "This extension does not expose callback token rotation.")
			return "", "", false
		}
		tok, err := hooks.RotateCallbackToken(e.ID, projectID, userID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to rotate callback token.")
			return "", "", false
		}
		shownCallback = tok
	}
	return shownSigning, shownCallback, true
}

func projectExtensionPatchHandler(w http.ResponseWriter, r *http.Request, projectID int, extensionID string, userID int, isOwner bool, workflowMode string) {
	e, ok := extensions.Get(extensionID)
	if !ok {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	visible, err := projectExtensionVisible(e, workflowMode)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	if !visible {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	var req projectExtensionPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
		return
	}
	cur, err := storage.GetExtensionProjectSettings(extensionID, projectID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	scope := extensions.WorkflowSettingScope(workflowMode)
	applyHookFiltersToProject(e.Manifest, &cur, req, scope)
	if req.Triggers != nil {
		cur.Triggers = filterDeclaredTriggers(e.Manifest, *req.Triggers)
	}
	if req.Templates != nil {
		if cur.Templates == nil {
			cur.Templates = map[string]string{}
		}
		for k, v := range req.Templates {
			if !hookNameDeclared(e.Manifest, k) {
				continue
			}
			cur.Templates[k] = v
		}
	}
	sec, callback, ok := saveDeliverySecrets(w, e, projectID, 0, req, scope)
	if !ok {
		return
	}
	if err := storage.UpsertExtensionProjectSettings(extensionID, projectID, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save extension settings.")
		return
	}
	item, err := projectExtensionFromEntry(e, projectID, userID, isOwner, workflowMode)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	item.SigningSecret = sec
	item.CallbackToken = callback
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func projectMemberExtensionGet(w http.ResponseWriter, projectID int, extensionID string, userID int) {
	e, ok := extensions.Get(extensionID)
	if !ok || !e.Loaded || e.Manifest.Delivery == nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if !e.Manifest.HasMemberSettings() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if projectID > 0 && !hooks.MemberDestinationsAllowed(projectID, "") {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Member destinations are not available on kanban projects.")
		return
	}
	item, err := projectExtensionFromEntry(e, projectID, userID, false, "")
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load settings.")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func projectMemberExtensionPatch(w http.ResponseWriter, r *http.Request, projectID int, extensionID string, userID int) {
	e, ok := extensions.Get(extensionID)
	if !ok || !e.Loaded || e.Manifest.Delivery == nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if !e.Manifest.HasMemberSettings() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if projectID > 0 && !hooks.MemberDestinationsAllowed(projectID, "") {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Member destinations are not available on kanban projects.")
		return
	}
	site, err := storage.GetExtensionSettings(extensionID)
	if err != nil || !site.Enabled {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "This extension is not enabled.")
		return
	}
	if projectID > 0 {
		ps, err := storage.GetExtensionProjectSettings(extensionID, projectID)
		if err != nil || !ps.Enabled {
			utils.APIJSONError(w, http.StatusForbidden, "forbidden", "The project owner has not enabled this extension.")
			return
		}
	}
	var req projectExtensionPatch
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
		return
	}
	cur, err := storage.GetExtensionMemberSettings(extensionID, projectID, userID)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load settings.")
		return
	}
	applyHookFiltersToMember(e.Manifest, &cur, req)
	if req.Triggers != nil {
		cur.Triggers = filterDeclaredTriggers(e.Manifest, *req.Triggers)
	}
	if req.Templates != nil {
		if cur.Templates == nil {
			cur.Templates = map[string]string{}
		}
		for k, v := range req.Templates {
			if !hookNameDeclared(e.Manifest, k) {
				continue
			}
			cur.Templates[k] = v
		}
	}
	sec, callback, ok := saveDeliverySecrets(w, e, projectID, userID, req, extensions.ScopeUser)
	if !ok {
		return
	}
	if err := storage.UpsertExtensionMemberSettings(extensionID, projectID, userID, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save settings.")
		return
	}
	item, err := projectExtensionFromEntry(e, projectID, userID, false, "")
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load settings.")
		return
	}
	item.SigningSecret = sec
	item.CallbackToken = callback
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(item)
}

func projectExtensionTest(w http.ResponseWriter, r *http.Request, projectID, userID int, extensionID, projectName string) {
	_ = r
	e, ok := extensions.Get(extensionID)
	if !ok || !e.Loaded || e.Manifest.Delivery == nil {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if userID > 0 && !e.Manifest.HasMemberSettings() {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if userID > 0 && projectID > 0 && !hooks.MemberDestinationsAllowed(projectID, "") {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Member destinations are not available on kanban projects.")
		return
	}
	if !e.Manifest.HasControl(extensions.ControlSendTest) {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if err := hooks.DeliverTestForUser(extensionID, projectID, userID, projectName); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	out := map[string]any{"ok": true, "message": "Test message sent."}
	if e.Manifest.HasControl(extensions.ControlSampleJSON) {
		out["sample_json"] = hooks.SampleJSONBody()
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

func projectExtensionRetry(w http.ResponseWriter, r *http.Request, projectID, userID int, extensionID, rawID string) {
	_ = r
	id, err := strconv.ParseInt(strings.TrimSpace(rawID), 10, 64)
	if err != nil || id <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid delivery id.")
		return
	}
	if _, ok := extensions.Get(extensionID); !ok {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	if err := hooks.ReplayDelivery(extensionID, projectID, userID, id); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "not found") {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Delivery not found.")
			return
		}
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", msg+".")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func teamDestinationHost(e extensions.Entry, projectID int) string {
	if projectID <= 0 || e.Manifest.Delivery == nil {
		return ""
	}
	key := e.Manifest.Delivery.DestinationKey()
	if key == "" {
		return ""
	}
	raw, err := storage.GetExtensionSecret(e.ID, projectID, key)
	if err != nil {
		return ""
	}
	return hooks.DestinationHost(raw)
}

func memberSecretKeys(m extensions.Manifest, workflowMode string) []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(keys []string) {
		for _, key := range keys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	add(m.UserSecretKeys())
	if dest := m.Delivery.DestinationKey(); dest != "" && m.HasSetting(dest) {
		add([]string{dest})
	}
	if m.HasSetting("ntfy_auth") {
		add([]string{"ntfy_auth"})
	}
	if workflowMode != "" {
		add(m.TeamSecretKeys(workflowMode))
	}
	return out
}

func projectExtensionFromEntry(e extensions.Entry, projectID, userID int, isOwner bool, workflowMode string) (projectExtensionJSON, error) {
	item := projectExtensionJSON{
		ID:            e.ID,
		Name:          e.Manifest.Name,
		Version:       e.Manifest.Version,
		HostAPI:       e.Manifest.HostAPI,
		Manifest:      e.Manifest,
		Secrets:       map[string]bool{},
		MemberSecrets: map[string]bool{},
		Settings:      storage.ExtensionProjectSettings{Triggers: []string{}, Templates: map[string]string{}},
		Member:        storage.ExtensionMemberSettings{Triggers: []string{}, Templates: map[string]string{}},
	}
	if item.Name == "" {
		item.Name = e.ID
	}
	site, err := storage.GetExtensionSettings(e.ID)
	if err != nil {
		return item, err
	}
	item.SiteEnabled = site.Enabled
	if projectID > 0 {
		s, err := storage.GetExtensionProjectSettings(e.ID, projectID)
		if err != nil {
			return item, err
		}
		item.Settings = s
		item.DestinationHost = teamDestinationHost(e, projectID)
		if isOwner {
			for _, key := range e.Manifest.TeamSecretKeys(workflowMode) {
				item.Secrets[key] = storage.ExtensionSecretIsSet(e.ID, projectID, key)
			}
			item.SigningSet = storage.ExtensionSecretIsSet(e.ID, projectID, storage.SigningSecretKey)
			item.CallbackSet = storage.CallbackTokenIsSet(e.ID, projectID, 0)
			if rows, err := storage.ListRecentDeliveries(e.ID, projectID, 0, 10); err == nil {
				item.Deliveries = deliveryJSON(rows)
			}
		}
	}
	if userID > 0 {
		mem, err := storage.GetExtensionMemberSettings(e.ID, projectID, userID)
		if err != nil {
			return item, err
		}
		item.Member = mem
		for _, key := range memberSecretKeys(e.Manifest, workflowMode) {
			item.MemberSecrets[key] = storage.ExtensionSecretIsSetForUser(e.ID, projectID, userID, key)
		}
		if rows, err := storage.ListRecentDeliveries(e.ID, projectID, userID, 10); err == nil {
			item.MemberDeliveries = deliveryJSON(rows)
		}
		item.MemberSigningSet = storage.ExtensionSecretIsSetForUser(e.ID, projectID, userID, storage.SigningSecretKey)
		item.MemberCallbackSet = storage.CallbackTokenIsSet(e.ID, projectID, userID)
	}
	return item, nil
}

func deliveryJSON(rows []storage.ExtensionDelivery) []extensionDeliveryJSON {
	out := make([]extensionDeliveryJSON, 0, len(rows))
	for _, r := range rows {
		item := extensionDeliveryJSON{
			ID:        r.ID,
			Event:     r.EventType,
			EventID:   r.EventID,
			Host:      r.URLHost,
			Status:    r.Status,
			HTTPCode:  r.HTTPCode,
			Error:     r.Error,
			Attempts:  r.Attempts,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		}
		if !r.NextAttemptAt.IsZero() && (r.Status == storage.DeliveryStatusPending || r.Status == storage.DeliveryStatusFailed) {
			item.NextAttemptAt = r.NextAttemptAt.UTC().Format(time.RFC3339)
		}
		out = append(out, item)
	}
	return out
}

func writeProjectExtensionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Project not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "You cannot manage this extension.")
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}

func projectExtensionUI(w http.ResponseWriter, r *http.Request, projectID int, extensionID string, extra []string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}
	e, ok := extensions.Get(extensionID)
	if !ok || !e.Loaded {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	mode, modeErr := storage.GetProjectWorkflowMode(projectID)
	if modeErr != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	visible, err := projectExtensionVisible(e, mode)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	if !visible || (!e.Manifest.HasUI() && !e.Manifest.HasSurfaces()) {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension UI not found.")
		return
	}
	rel := strings.TrimSpace(e.Manifest.UI)
	if surfaceID := strings.TrimSpace(r.URL.Query().Get("surface")); surfaceID != "" {
		s, ok := e.Manifest.SurfaceByID(surfaceID)
		if !ok {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension UI not found.")
			return
		}
		rel = s.File
	} else if rel == "" {
		surfaces := e.Manifest.ResolvedSurfaces()
		if len(surfaces) == 0 {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension UI not found.")
			return
		}
		rel = surfaces[0].File
	}
	asPanel := true
	if len(extra) > 0 {
		asPanel = false
		joined := strings.Trim(strings.Join(extra, "/"), "/")
		base := strings.TrimSuffix(strings.ReplaceAll(rel, "\\", "/"), "/")
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[:i]
		} else {
			base = ""
		}
		if base != "" {
			rel = base + "/" + joined
		} else {
			rel = joined
		}
	}
	_ = projectID
	serveExtensionFile(w, r, e.Dir, rel, asPanel)
}

type extensionStorePutBody struct {
	Revision int             `json:"revision"`
	Value    json.RawMessage `json:"value"`
}

func projectExtensionStore(w http.ResponseWriter, r *http.Request, projectID int, extensionID string, userID int, extra []string) {
	if len(extra) == 0 {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		keys, err := domain.ListExtensionStoreKeys(userID, projectID, extensionID)
		if err != nil {
			writeExtensionStoreError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": keys})
		return
	}
	key := strings.Trim(strings.Join(extra, "/"), "/")
	switch r.Method {
	case http.MethodGet:
		doc, err := domain.GetExtensionStoreDoc(userID, projectID, extensionID, key)
		if err != nil {
			writeExtensionStoreError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(doc)
	case http.MethodPut:
		var body extensionStorePutBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON.")
			return
		}
		revision := body.Revision
		if match := strings.TrimSpace(r.Header.Get("If-Match")); match != "" {
			n, err := strconv.Atoi(match)
			if err != nil || n < 0 {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "If-Match must be a revision number.")
				return
			}
			revision = n
		}
		doc, err := domain.PutExtensionStoreDoc(userID, projectID, extensionID, key, revision, body.Value)
		if err != nil {
			writeExtensionStoreError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(doc)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func writeExtensionStoreError(w http.ResponseWriter, err error) {
	var conflict *domain.StoreConflictError
	if errors.As(err, &conflict) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "conflict",
			"message": "Store revision does not match.",
			"current": conflict.Current,
		})
		return
	}
	switch {
	case errors.Is(err, domain.ErrNotFound):
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	case errors.Is(err, domain.ErrForbidden):
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", "You cannot access this extension store.")
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
	default:
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
	}
}
