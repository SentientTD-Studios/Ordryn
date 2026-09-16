package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/extensions"
	"GoTodo/internal/hooks"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type projectExtensionJSON struct {
	ID               string                           `json:"id"`
	Name             string                           `json:"name"`
	Version          string                           `json:"version"`
	HostAPI          int                              `json:"host_api"`
	SiteEnabled      bool                             `json:"site_enabled"`
	Manifest         extensions.Manifest              `json:"manifest"`
	Settings         storage.ExtensionProjectSettings `json:"settings"`
	Secrets          map[string]bool                  `json:"secrets"`
	Member           storage.ExtensionMemberSettings  `json:"member"`
	MemberSecrets    map[string]bool                  `json:"member_secrets"`
	SigningSet       bool                             `json:"signing_set"`
	MemberSigningSet bool                             `json:"member_signing_set"`
	SigningSecret    string                           `json:"signing_secret,omitempty"`
	SampleJSON       string                           `json:"sample_json,omitempty"`
	Deliveries       []extensionDeliveryJSON          `json:"deliveries,omitempty"`
	MemberDeliveries []extensionDeliveryJSON          `json:"member_deliveries,omitempty"`
}

type extensionDeliveryJSON struct {
	ID        int64  `json:"id"`
	Event     string `json:"event"`
	EventID   string `json:"event_id,omitempty"`
	Host      string `json:"host,omitempty"`
	Status    string `json:"status"`
	HTTPCode  int    `json:"http_code,omitempty"`
	Error     string `json:"error,omitempty"`
	Attempts  int    `json:"attempts"`
	CreatedAt string `json:"created_at"`
}

type projectExtensionsListJSON struct {
	Extensions []projectExtensionJSON `json:"extensions"`
	IsOwner    bool                   `json:"is_owner"`
}

type projectExtensionPatch struct {
	Enabled         *bool             `json:"enabled"`
	Triggers        *[]string         `json:"triggers"`
	Templates       map[string]string `json:"templates"`
	StatusOnly      *bool             `json:"status_only"`
	SkipSelf        *bool             `json:"skip_self"`
	MinPriority     *int              `json:"min_priority"`
	TagIDs          *[]int            `json:"tag_ids"`
	ClaimedOnly     *bool             `json:"claimed_only"`
	ClaimedIsMe     *bool             `json:"claimed_is_me"`
	FieldKey        *string           `json:"field_key"`
	FieldValue      *string           `json:"field_value"`
	QuietHoursStart *string           `json:"quiet_hours_start"`
	QuietHoursEnd   *string           `json:"quiet_hours_end"`
	Digest          *string           `json:"digest"`
	MentionMap      map[string]string `json:"mention_map"`
	WebhookURL      *string           `json:"webhook_url"`
	NtfyAuth        *string           `json:"ntfy_auth"`
	RotateSigning   *bool             `json:"rotate_signing"`
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
		projectExtensionsList(w, projectID, userID, isOwner)
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
		projectExtensionPatchHandler(w, r, projectID, extensionID, userID, true)
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
	utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
}

func projectExtensionsList(w http.ResponseWriter, projectID, userID int, isOwner bool) {
	entries := extensions.Snapshot()
	out := make([]projectExtensionJSON, 0)
	for _, e := range entries {
		ok, err := projectExtensionVisible(e)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
			return
		}
		if !ok {
			continue
		}
		item, err := projectExtensionFromEntry(e, projectID, userID, isOwner)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
			return
		}
		out = append(out, item)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(projectExtensionsListJSON{Extensions: out, IsOwner: isOwner})
}

func projectExtensionVisible(e extensions.Entry) (bool, error) {
	if !e.Loaded || !e.Manifest.HasProjectSurface() {
		return false, nil
	}
	site, err := storage.GetExtensionSettings(e.ID)
	if err != nil {
		return false, err
	}
	return site.Enabled, nil
}

func applyHookFiltersToProject(m extensions.Manifest, cur *storage.ExtensionProjectSettings, req projectExtensionPatch) {
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if req.StatusOnly != nil && m.HasSetting("status_only") {
		cur.StatusOnly = *req.StatusOnly
	}
	if req.SkipSelf != nil && m.HasSetting("skip_self") {
		cur.SkipSelf = *req.SkipSelf
	}
	if req.MinPriority != nil && m.HasSetting("min_priority") {
		cur.MinPriority = *req.MinPriority
	}
	if req.TagIDs != nil && m.HasSetting("tag_ids") {
		cur.TagIDs = *req.TagIDs
	}
	if req.ClaimedOnly != nil && m.HasSetting("claimed_only") {
		cur.ClaimedOnly = *req.ClaimedOnly
	}
	if (req.FieldKey != nil || req.FieldValue != nil) && (m.HasSetting("field_key") || m.HasSetting("field_value") || m.HasSetting("field_filter")) {
		if req.FieldKey != nil {
			cur.FieldKey = strings.TrimSpace(*req.FieldKey)
		}
		if req.FieldValue != nil {
			cur.FieldValue = strings.TrimSpace(*req.FieldValue)
		}
	}
	if req.QuietHoursStart != nil && m.HasSetting("quiet_hours_start") {
		cur.QuietHoursStart = strings.TrimSpace(*req.QuietHoursStart)
	}
	if req.QuietHoursEnd != nil && m.HasSetting("quiet_hours_end") {
		cur.QuietHoursEnd = strings.TrimSpace(*req.QuietHoursEnd)
	}
	if req.Digest != nil && m.HasSetting("digest") {
		cur.Digest = strings.ToLower(strings.TrimSpace(*req.Digest))
	}
	if req.MentionMap != nil && m.HasSetting("mention_map") {
		cur.MentionMap = req.MentionMap
	}
}

func applyHookFiltersToMember(m extensions.Manifest, cur *storage.ExtensionMemberSettings, req projectExtensionPatch) {
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if req.StatusOnly != nil && m.HasSetting("status_only") {
		cur.StatusOnly = *req.StatusOnly
	}
	if req.SkipSelf != nil && m.HasSetting("skip_self") {
		cur.SkipSelf = req.SkipSelf
	}
	if req.MinPriority != nil && m.HasSetting("min_priority") {
		cur.MinPriority = *req.MinPriority
	}
	if req.TagIDs != nil && m.HasSetting("tag_ids") {
		cur.TagIDs = *req.TagIDs
	}
	if req.ClaimedOnly != nil && m.HasSetting("claimed_only") {
		cur.ClaimedOnly = *req.ClaimedOnly
	}
	if req.ClaimedIsMe != nil && m.HasSetting("claimed_is_me") {
		cur.ClaimedIsMe = *req.ClaimedIsMe
	}
	if (req.FieldKey != nil || req.FieldValue != nil) && (m.HasSetting("field_key") || m.HasSetting("field_value") || m.HasSetting("field_filter")) {
		if req.FieldKey != nil {
			cur.FieldKey = strings.TrimSpace(*req.FieldKey)
		}
		if req.FieldValue != nil {
			cur.FieldValue = strings.TrimSpace(*req.FieldValue)
		}
	}
	if req.QuietHoursStart != nil && m.HasSetting("quiet_hours_start") {
		cur.QuietHoursStart = strings.TrimSpace(*req.QuietHoursStart)
	}
	if req.QuietHoursEnd != nil && m.HasSetting("quiet_hours_end") {
		cur.QuietHoursEnd = strings.TrimSpace(*req.QuietHoursEnd)
	}
	if req.Digest != nil && m.HasSetting("digest") {
		cur.Digest = strings.ToLower(strings.TrimSpace(*req.Digest))
	}
}

func saveDeliverySecrets(w http.ResponseWriter, e extensions.Entry, projectID, userID int, req projectExtensionPatch) (shownSecret string, ok bool) {
	key := "webhook_url"
	if e.Manifest.Delivery != nil {
		if k := e.Manifest.Delivery.DestinationKey(); k != "" {
			key = k
		}
	}
	if req.WebhookURL != nil && strings.TrimSpace(*req.WebhookURL) != "" {
		url := strings.TrimSpace(*req.WebhookURL)
		if e.Manifest.Delivery != nil {
			if err := hooks.ValidateDeliveryURL(e.Manifest.Delivery, url); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error()+".")
				return "", false
			}
		}
		if err := storage.SetExtensionSecretForUser(e.ID, projectID, userID, key, url); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save webhook URL.")
			return "", false
		}
	}
	if req.NtfyAuth != nil && strings.TrimSpace(*req.NtfyAuth) != "" {
		if err := storage.SetExtensionSecretForUser(e.ID, projectID, userID, "ntfy_auth", strings.TrimSpace(*req.NtfyAuth)); err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save ntfy token.")
			return "", false
		}
	}
	if req.RotateSigning != nil && *req.RotateSigning {
		if !e.Manifest.HasControl(extensions.ControlRotateSigning) {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "This extension does not expose signing secret rotation.")
			return "", false
		}
		sec, err := hooks.RotateSigningSecret(e.ID, projectID, userID)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to rotate signing secret.")
			return "", false
		}
		shownSecret = sec
	}
	return shownSecret, true
}

func projectExtensionPatchHandler(w http.ResponseWriter, r *http.Request, projectID int, extensionID string, userID int, isOwner bool) {
	e, ok := extensions.Get(extensionID)
	if !ok {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Extension not found.")
		return
	}
	visible, err := projectExtensionVisible(e)
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
	applyHookFiltersToProject(e.Manifest, &cur, req)
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
	sec, ok := saveDeliverySecrets(w, e, projectID, 0, req)
	if !ok {
		return
	}
	if err := storage.UpsertExtensionProjectSettings(extensionID, projectID, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save extension settings.")
		return
	}
	item, err := projectExtensionFromEntry(e, projectID, userID, isOwner)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load extension settings.")
		return
	}
	item.SigningSecret = sec
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
	item, err := projectExtensionFromEntry(e, projectID, userID, false)
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
	sec, ok := saveDeliverySecrets(w, e, projectID, userID, req)
	if !ok {
		return
	}
	if err := storage.UpsertExtensionMemberSettings(extensionID, projectID, userID, cur); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to save settings.")
		return
	}
	item, err := projectExtensionFromEntry(e, projectID, userID, false)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load settings.")
		return
	}
	item.SigningSecret = sec
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

func projectExtensionFromEntry(e extensions.Entry, projectID, userID int, isOwner bool) (projectExtensionJSON, error) {
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
		if isOwner {
			for _, key := range e.Manifest.ProjectSecretKeys() {
				item.Secrets[key] = storage.ExtensionSecretIsSet(e.ID, projectID, key)
			}
			item.SigningSet = storage.ExtensionSecretIsSet(e.ID, projectID, storage.SigningSecretKey)
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
		for _, key := range e.Manifest.ProjectSecretKeys() {
			item.MemberSecrets[key] = storage.ExtensionSecretIsSetForUser(e.ID, projectID, userID, key)
		}
		if rows, err := storage.ListRecentDeliveries(e.ID, projectID, userID, 10); err == nil {
			item.MemberDeliveries = deliveryJSON(rows)
		}
		item.MemberSigningSet = storage.ExtensionSecretIsSetForUser(e.ID, projectID, userID, storage.SigningSecretKey)
	}
	return item, nil
}

func deliveryJSON(rows []storage.ExtensionDelivery) []extensionDeliveryJSON {
	out := make([]extensionDeliveryJSON, 0, len(rows))
	for _, r := range rows {
		out = append(out, extensionDeliveryJSON{
			ID:        r.ID,
			Event:     r.EventType,
			EventID:   r.EventID,
			Host:      r.URLHost,
			Status:    r.Status,
			HTTPCode:  r.HTTPCode,
			Error:     r.Error,
			Attempts:  r.Attempts,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
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
