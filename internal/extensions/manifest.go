package extensions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// CurrentHostAPI is the highest hook host API this build understands.
const CurrentHostAPI = 2

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,32}$`)

var knownEventHooks = map[string]struct{}{
	"task.created":                {},
	"task.updated":                {},
	"task.deleted":                {},
	"task.commented":              {},
	"task.reordered":              {},
	"task.claimed":                {},
	"task.unclaimed":              {},
	"task.due_changed":            {},
	"task.moved":                  {},
	"task.project_changed":        {},
	"task.sprint_changed":         {},
	"task.tagged":                 {},
	"task.overdue":                {},
	"task.mentioned":              {},
	"task.completed":              {},
	"task.reopened":               {},
	"task.due_soon":               {},
	"task.archived":               {},
	"task.restored":               {},
	"task.status_changed":         {},
	"task.comment_edited":         {},
	"task.comment_deleted":        {},
	"task.comment_restored":       {},
	"project.updated":             {},
	"project.created":             {},
	"project.deleted":             {},
	"project.archived":            {},
	"project.restored":            {},
	"project.member_joined":       {},
	"project.member_left":         {},
	"project.member_role_changed": {},
	"project.invite_sent":         {},
	"project.invite_declined":     {},
	"sprint.created":              {},
	"sprint.updated":              {},
	"sprint.deleted":              {},
	"sprint.started":              {},
	"sprint.ended":                {},
	"import.completed":            {},
	"join.request":                {},
	"join.approved":               {},
	"join.denied":                 {},
}

var knownSettingTypes = map[string]struct{}{
	"secret":             {},
	"project_ids":        {},
	"hook_select":        {},
	"bool":               {},
	"string":             {},
	"int":                {},
	"select":             {},
	"status":             {},
	"user":               {},
	"priority":           {},
	"tag_ids":            {},
	"status_ids":         {},
	"status_exclude_ids": {},
	"time":               {},
	"digest":             {},
	"field_filter":       {},
	"mention_map":        {},
}

// wellKnownSettingKeys maps specialized widgets to the storage key they bind to.
var wellKnownSettingKeys = map[string]string{
	"priority":           "min_priority",
	"tag_ids":            "tag_ids",
	"status_ids":         "status_ids",
	"status_exclude_ids": "status_exclude_ids",
	"digest":             "digest",
	"mention_map":        "mention_map",
	"field_filter":       "field_filter",
}

var knownFieldTypes = map[string]struct{}{
	"string":   {},
	"number":   {},
	"boolean":  {},
	"enum":     {},
	"url":      {},
	"user":     {},
	"date":     {},
	"markdown": {},
}

var knownShowOn = map[string]struct{}{
	"sidebar": {},
	"kanban":  {},
	"list":    {},
}

var settingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,32}$`)

const (
	ScopeSite    = "site"
	ScopeProject = "project"
	ScopeKanban  = "kanban"
	ScopeUser    = "user"
	ScopeMember  = "member"
)

const (
	DeliveryDiscordWebhook    = "discord.webhook"
	DeliverySlackWebhook      = "slack.webhook"
	DeliveryTeamsWebhook      = "teams.webhook"
	DeliveryGoogleChatWebhook = "googlechat.webhook"
	DeliveryHTTPWebhook       = "http.webhook"
	DeliveryNtfyWebhook       = "ntfy.webhook"
)

const (
	DeliveryFormatText    = "text"
	DeliveryFormatContent = "content"
	DeliveryFormatJSON    = "json"
)

var knownDeliveryTypes = map[string]struct{}{
	DeliveryDiscordWebhook:    {},
	DeliverySlackWebhook:      {},
	DeliveryTeamsWebhook:      {},
	DeliveryGoogleChatWebhook: {},
	DeliveryHTTPWebhook:       {},
	DeliveryNtfyWebhook:       {},
}

var knownDeliveryFormats = map[string]struct{}{
	DeliveryFormatText:    {},
	DeliveryFormatContent: {},
	DeliveryFormatJSON:    {},
}

const (
	ControlSendTest       = "send_test"
	ControlRotateSigning  = "rotate_signing"
	ControlSampleJSON     = "sample_json"
	ControlRotateCallback = "rotate_callback"
)

const (
	PermTasksRead     = "tasks:read"
	PermTasksWrite    = "tasks:write"
	PermCommentsWrite = "comments:write"
	PermStoreRead     = "store:read"
	PermStoreWrite    = "store:write"
)

var knownPermissions = map[string]struct{}{
	PermTasksRead:     {},
	PermTasksWrite:    {},
	PermCommentsWrite: {},
	PermStoreRead:     {},
	PermStoreWrite:    {},
}

const (
	SurfaceProjectExtensions = "project.extensions"
	SurfaceKanbanTab         = "kanban.tab"
)

var knownSurfaces = map[string]struct{}{
	SurfaceProjectExtensions: {},
	SurfaceKanbanTab:         {},
}

const (
	ActionComplete = "complete"
	ActionComment  = "comment"
	ActionSetField = "set_field"
)

var knownActions = map[string]struct{}{
	ActionComplete: {},
	ActionComment:  {},
	ActionSetField: {},
}

var knownControls = map[string]struct{}{
	ControlSendTest:       {},
	ControlRotateSigning:  {},
	ControlSampleJSON:     {},
	ControlRotateCallback: {},
}

const maxDescriptionLen = 400
const maxAuthorLen = 80
const maxLicenseLen = 64
const maxHomepageLen = 200
const maxHookLabelLen = 80

// Manifest is the required data/extensions/<id>/manifest.json document.
type Manifest struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	HostAPI     int               `json:"host_api"`
	Description string            `json:"description,omitempty"`
	Author      string            `json:"author,omitempty"`
	Homepage    string            `json:"homepage,omitempty"`
	License     string            `json:"license,omitempty"`
	Icon        string            `json:"icon,omitempty"`
	UI          string            `json:"ui,omitempty"`
	Surfaces    []Surface         `json:"surfaces,omitempty"`
	Hooks       []Hook            `json:"hooks,omitempty"`
	Delivery    *Delivery         `json:"delivery,omitempty"`
	Settings    []Setting         `json:"settings,omitempty"`
	Templates   map[string]string `json:"templates,omitempty"`
	Fields      []Field           `json:"fields,omitempty"`
	Controls    []string          `json:"controls,omitempty"`
	Permissions []string          `json:"permissions,omitempty"`
	Actions     []string          `json:"actions,omitempty"`
}

// Surface is a sandboxed HTML panel placement (host API 2).
type Surface struct {
	ID    string `json:"id"`
	File  string `json:"file"`
	At    string `json:"at"`
	Label string `json:"label,omitempty"`
}

// Field registers a core custom field. Stored values use key "{id}.{key}".
type Field struct {
	Key         string        `json:"key"`
	Type        string        `json:"type"`
	Label       string        `json:"label"`
	Description string        `json:"description,omitempty"`
	Required    bool          `json:"required"`
	ShowOn      []string      `json:"show_on,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
}

// FieldOption is one enum value.
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
	Color string `json:"color,omitempty"`
}

// FieldKey is the persisted custom field key for an extension field.
func FieldKey(extensionID, localKey string) string {
	return strings.TrimSpace(extensionID) + "." + strings.TrimSpace(localKey)
}

// Hook is a registration on a named core extension point.
type Hook struct {
	On    string `json:"on"`
	Label string `json:"label,omitempty"`
}

// Delivery describes how event hooks are sent outbound.
type Delivery struct {
	Type    string `json:"type"`
	URLFrom string `json:"url_from,omitempty"`
	// Format selects the JSON body for http.webhook: text ({"text"}), content ({"content"}), or json (structured event). Ignored for provider-specific types.
	Format string `json:"format,omitempty"`
}

// Setting is a schema-driven form field (site admin, classic project, kanban, or user).
// Project/kanban well-known keys bind to hook filters: skip_self, claimed_only,
// claimed_is_me, min_priority, tag_ids, quiet_hours_start, quiet_hours_end,
// digest, field_key, field_value, field_filter, mention_map, status_only, and triggers.
type Setting struct {
	Key         string        `json:"key"`
	Type        string        `json:"type"`
	Label       string        `json:"label"`
	Description string        `json:"description,omitempty"`
	Required    bool          `json:"required"`
	Scope       ScopeList     `json:"scope,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
}

// ScopeList is settings.scope: a JSON string or array of strings.
type ScopeList []string

// UnmarshalJSON accepts "project" or ["project", "kanban"].
func (s *ScopeList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*s = nil
		return nil
	}
	if b[0] == '"' {
		var one string
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		if strings.TrimSpace(one) == "" {
			*s = nil
			return nil
		}
		*s = ScopeList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("scope must be a string or array of strings")
	}
	*s = many
	return nil
}

// MarshalJSON writes a single scope as a string and multiple scopes as an array.
func (s ScopeList) MarshalJSON() ([]byte, error) {
	if len(s) == 0 {
		return []byte("null"), nil
	}
	if len(s) == 1 {
		return json.Marshal(s[0])
	}
	return json.Marshal([]string(s))
}

// CanonicalScope maps a raw scope name to site, project, kanban, or user.
// member is accepted as an alias of user. Empty defaults to site.
func CanonicalScope(name string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", ScopeSite:
		return ScopeSite, true
	case ScopeProject:
		return ScopeProject, true
	case ScopeKanban:
		return ScopeKanban, true
	case ScopeUser, ScopeMember:
		return ScopeUser, true
	default:
		return strings.ToLower(strings.TrimSpace(name)), false
	}
}

// WorkflowSettingScope is the team settings scope for a project's workflow mode.
func WorkflowSettingScope(workflowMode string) string {
	if strings.EqualFold(strings.TrimSpace(workflowMode), "kanban") {
		return ScopeKanban
	}
	return ScopeProject
}

// Scopes returns the normalized scope list. Missing/empty scope is site.
func (s Setting) Scopes() []string {
	if len(s.Scope) == 0 {
		return []string{ScopeSite}
	}
	out := make([]string, 0, len(s.Scope))
	seen := make(map[string]struct{}, len(s.Scope))
	for _, raw := range s.Scope {
		name, ok := CanonicalScope(raw)
		if !ok {
			name = strings.ToLower(strings.TrimSpace(raw))
		}
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	if len(out) == 0 {
		return []string{ScopeSite}
	}
	return out
}

// ScopeName returns the first normalized scope (site when unset).
func (s Setting) ScopeName() string {
	return s.Scopes()[0]
}

// HasScope reports whether the setting is configurable at scope.
// user and member match each other.
func (s Setting) HasScope(scope string) bool {
	want, ok := CanonicalScope(scope)
	if !ok {
		want = strings.ToLower(strings.TrimSpace(scope))
	}
	for _, got := range s.Scopes() {
		name, known := CanonicalScope(got)
		if !known {
			name = got
		}
		if name == want {
			return true
		}
	}
	return false
}

// ValidateManifest checks host API rules. folderName must equal id.
func ValidateManifest(folderName string, m Manifest) error {
	id := strings.TrimSpace(m.ID)
	if !idPattern.MatchString(id) {
		return fmt.Errorf("id %q is invalid (use lowercase letters, digits, and hyphens)", m.ID)
	}
	if folderName != id {
		return fmt.Errorf("id %q must match folder name %q", id, folderName)
	}
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(strings.TrimSpace(m.Description)) > maxDescriptionLen {
		return fmt.Errorf("description must be %d characters or less", maxDescriptionLen)
	}
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if m.HostAPI < 1 {
		return fmt.Errorf("host_api is required")
	}
	if m.HostAPI > CurrentHostAPI {
		return fmt.Errorf("needs Ordryn that supports host_api %d (this build supports %d)", m.HostAPI, CurrentHostAPI)
	}
	if err := validateRelPath("ui", m.UI); err != nil {
		return err
	}
	if err := validateRelPath("icon", m.Icon); err != nil {
		return err
	}
	if author := strings.TrimSpace(m.Author); len(author) > maxAuthorLen {
		return fmt.Errorf("author must be %d characters or less", maxAuthorLen)
	}
	if license := strings.TrimSpace(m.License); len(license) > maxLicenseLen {
		return fmt.Errorf("license must be %d characters or less", maxLicenseLen)
	}
	if homepage := strings.TrimSpace(m.Homepage); homepage != "" {
		if len(homepage) > maxHomepageLen {
			return fmt.Errorf("homepage must be %d characters or less", maxHomepageLen)
		}
		u, err := url.Parse(homepage)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("homepage must be an http(s) URL")
		}
	}
	seenOn := make(map[string]struct{})
	for i := range m.Hooks {
		on := strings.TrimSpace(m.Hooks[i].On)
		if on == "" {
			return fmt.Errorf("hooks.on is required")
		}
		if _, ok := knownEventHooks[on]; !ok {
			return fmt.Errorf("unknown hook %q", on)
		}
		if _, dup := seenOn[on]; dup {
			return fmt.Errorf("duplicate hook %q", on)
		}
		seenOn[on] = struct{}{}
		label := strings.TrimSpace(m.Hooks[i].Label)
		if label == "" {
			label = HookLabel(on)
		}
		if len(label) > maxHookLabelLen {
			return fmt.Errorf("hooks.label is too long for %s", on)
		}
		m.Hooks[i].On = on
		m.Hooks[i].Label = label
	}
	if m.Delivery != nil {
		typ := strings.TrimSpace(m.Delivery.Type)
		if typ == "" {
			return fmt.Errorf("delivery.type is required")
		}
		if typ == "email" {
			return fmt.Errorf("delivery type %q is not allowed (site email is not available to extensions)", typ)
		}
		if _, ok := knownDeliveryTypes[typ]; !ok {
			return fmt.Errorf("unknown delivery type %q", typ)
		}
		m.Delivery.Type = typ
		m.Delivery.URLFrom = strings.TrimSpace(m.Delivery.URLFrom)
		if m.Delivery.URLFrom == "" {
			return fmt.Errorf("delivery.url_from is required")
		}
		format := strings.ToLower(strings.TrimSpace(m.Delivery.Format))
		if format != "" {
			if typ != DeliveryHTTPWebhook {
				return fmt.Errorf("delivery.format is only allowed for %s", DeliveryHTTPWebhook)
			}
			if _, ok := knownDeliveryFormats[format]; !ok {
				return fmt.Errorf("unknown delivery format %q", m.Delivery.Format)
			}
			m.Delivery.Format = format
		}
	}
	seenKeys := make(map[string]struct{})
	for i := range m.Settings {
		s := &m.Settings[i]
		key := strings.TrimSpace(s.Key)
		if !settingKeyPattern.MatchString(key) {
			return fmt.Errorf("settings key %q is invalid", s.Key)
		}
		if _, dup := seenKeys[key]; dup {
			return fmt.Errorf("duplicate settings key %q", key)
		}
		seenKeys[key] = struct{}{}
		s.Key = key
		typ := strings.TrimSpace(s.Type)
		if _, ok := knownSettingTypes[typ]; !ok {
			return fmt.Errorf("unknown settings type %q for %s", s.Type, key)
		}
		s.Type = typ
		if strings.TrimSpace(s.Label) == "" {
			return fmt.Errorf("settings label is required for %s", key)
		}
		if len(strings.TrimSpace(s.Description)) > maxDescriptionLen {
			return fmt.Errorf("settings description is too long for %s", key)
		}
		if want, ok := wellKnownSettingKeys[typ]; ok && key != want {
			return fmt.Errorf("settings type %s requires key %s", typ, want)
		}
		normalized := ScopeList{}
		seenScope := make(map[string]struct{})
		for _, raw := range s.Scope {
			name, ok := CanonicalScope(raw)
			if !ok {
				return fmt.Errorf("unknown settings scope %q for %s", strings.TrimSpace(raw), key)
			}
			if _, dup := seenScope[name]; dup {
				continue
			}
			seenScope[name] = struct{}{}
			normalized = append(normalized, name)
		}
		s.Scope = normalized
		if typ == "select" {
			opts, err := normalizeFieldOptions(s.Options)
			if err != nil {
				return fmt.Errorf("settings %s: %w", key, err)
			}
			s.Options = opts
		} else if len(s.Options) > 0 {
			return fmt.Errorf("settings %s: options are only allowed on select", key)
		}
	}
	if m.Delivery != nil {
		from := strings.TrimSpace(m.Delivery.URLFrom)
		if from != "" {
			if _, ok := seenKeys[from]; !ok {
				return fmt.Errorf("delivery.url_from %q is not a settings key", from)
			}
		}
	}
	for on := range m.Templates {
		if _, ok := knownEventHooks[on]; !ok {
			return fmt.Errorf("unknown template hook %q", on)
		}
	}
	seenControls := make(map[string]struct{})
	for i, raw := range m.Controls {
		c := strings.ToLower(strings.TrimSpace(raw))
		if c == "" {
			continue
		}
		if _, ok := knownControls[c]; !ok {
			return fmt.Errorf("unknown control %q", raw)
		}
		if _, dup := seenControls[c]; dup {
			return fmt.Errorf("duplicate control %q", raw)
		}
		seenControls[c] = struct{}{}
		m.Controls[i] = c
	}
	seenFields := make(map[string]struct{})
	for i := range m.Fields {
		f := &m.Fields[i]
		key := strings.TrimSpace(f.Key)
		if !settingKeyPattern.MatchString(key) {
			return fmt.Errorf("fields key %q is invalid", f.Key)
		}
		if _, dup := seenFields[key]; dup {
			return fmt.Errorf("duplicate fields key %q", key)
		}
		seenFields[key] = struct{}{}
		f.Key = key
		typ := strings.TrimSpace(f.Type)
		if _, ok := knownFieldTypes[typ]; !ok {
			return fmt.Errorf("unknown fields type %q for %s", f.Type, key)
		}
		f.Type = typ
		if strings.TrimSpace(f.Label) == "" {
			return fmt.Errorf("fields label is required for %s", key)
		}
		if len(strings.TrimSpace(f.Description)) > maxDescriptionLen {
			return fmt.Errorf("fields description is too long for %s", key)
		}
		showOn, err := normalizeShowOn(f.ShowOn)
		if err != nil {
			return fmt.Errorf("fields %s: %w", key, err)
		}
		f.ShowOn = showOn
		if typ == "enum" {
			opts, err := normalizeFieldOptions(f.Options)
			if err != nil {
				return fmt.Errorf("fields %s: %w", key, err)
			}
			f.Options = opts
		} else if len(f.Options) > 0 {
			return fmt.Errorf("fields %s: options are only allowed on enum", key)
		}
	}
	seenPerms := make(map[string]struct{})
	for i, raw := range m.Permissions {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			continue
		}
		if _, ok := knownPermissions[p]; !ok {
			return fmt.Errorf("unknown permission %q", raw)
		}
		if _, dup := seenPerms[p]; dup {
			return fmt.Errorf("duplicate permission %q", raw)
		}
		seenPerms[p] = struct{}{}
		m.Permissions[i] = p
	}
	seenActions := make(map[string]struct{})
	for i, raw := range m.Actions {
		a := strings.ToLower(strings.TrimSpace(raw))
		if a == "" {
			continue
		}
		if _, ok := knownActions[a]; !ok {
			return fmt.Errorf("unknown action %q", raw)
		}
		if _, dup := seenActions[a]; dup {
			return fmt.Errorf("duplicate action %q", raw)
		}
		seenActions[a] = struct{}{}
		m.Actions[i] = a
	}
	needsHost2 := len(m.Surfaces) > 0
	for _, p := range m.Permissions {
		if p == PermStoreRead || p == PermStoreWrite {
			needsHost2 = true
			break
		}
	}
	if needsHost2 && m.HostAPI < 2 {
		return fmt.Errorf("surfaces and store permissions require host_api 2")
	}
	seenSurfaceIDs := make(map[string]struct{})
	for i := range m.Surfaces {
		s := &m.Surfaces[i]
		id := strings.TrimSpace(s.ID)
		if !settingKeyPattern.MatchString(id) {
			return fmt.Errorf("surfaces id %q is invalid", s.ID)
		}
		if _, dup := seenSurfaceIDs[id]; dup {
			return fmt.Errorf("duplicate surfaces id %q", id)
		}
		seenSurfaceIDs[id] = struct{}{}
		s.ID = id
		file := strings.TrimSpace(s.File)
		if file == "" {
			return fmt.Errorf("surfaces.file is required for %s", id)
		}
		if err := validateRelPath("surfaces.file", file); err != nil {
			return err
		}
		s.File = file
		at := strings.ToLower(strings.TrimSpace(s.At))
		if _, ok := knownSurfaces[at]; !ok {
			return fmt.Errorf("unknown surfaces.at %q for %s", s.At, id)
		}
		s.At = at
		label := strings.TrimSpace(s.Label)
		if len(label) > maxHookLabelLen {
			return fmt.Errorf("surfaces.label is too long for %s", id)
		}
		s.Label = label
	}
	hasKanban := false
	hasEnableSurface := m.HasUI() || m.HasFields() || m.HasProjectSettings() || m.HasKanbanSettings() || m.HasMemberSettings()
	for _, s := range m.Surfaces {
		if s.At == SurfaceKanbanTab {
			hasKanban = true
		}
		if s.At == SurfaceProjectExtensions {
			hasEnableSurface = true
		}
	}
	if hasKanban && !hasEnableSurface {
		return fmt.Errorf("kanban.tab requires a project.extensions surface (or ui) so the extension can be enabled")
	}
	return nil
}

func validateRelPath(field, rel string) error {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return nil
	}
	if strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") || strings.Contains(rel, ":") {
		return fmt.Errorf("%s path %q is not allowed", field, rel)
	}
	return nil
}

func normalizeShowOn(in []string) ([]string, error) {
	if len(in) == 0 {
		return []string{"sidebar"}, nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{})
	for _, raw := range in {
		v := strings.ToLower(strings.TrimSpace(raw))
		if v == "" {
			continue
		}
		if _, ok := knownShowOn[v]; !ok {
			return nil, fmt.Errorf("unknown show_on %q", raw)
		}
		if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	if len(out) == 0 {
		return []string{"sidebar"}, nil
	}
	return out, nil
}

func normalizeFieldOptions(in []FieldOption) ([]FieldOption, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("enum requires options")
	}
	out := make([]FieldOption, 0, len(in))
	seen := make(map[string]struct{})
	for _, opt := range in {
		val := strings.TrimSpace(opt.Value)
		if val == "" {
			return nil, fmt.Errorf("option value is required")
		}
		if _, dup := seen[val]; dup {
			return nil, fmt.Errorf("duplicate option %q", val)
		}
		seen[val] = struct{}{}
		label := strings.TrimSpace(opt.Label)
		if label == "" {
			label = val
		}
		out = append(out, FieldOption{Value: val, Label: label, Color: strings.TrimSpace(opt.Color)})
	}
	return out, nil
}

// EventHooks returns the registered event hook names.
func (m Manifest) EventHooks() []string {
	out := make([]string, 0, len(m.Hooks))
	for _, h := range m.Hooks {
		if on := strings.TrimSpace(h.On); on != "" {
			out = append(out, on)
		}
	}
	return out
}

// SecretKeys returns setting keys with type secret (any scope).
func (m Manifest) SecretKeys() []string {
	return m.secretKeys("")
}

// SiteSecretKeys returns site-scoped secret keys.
func (m Manifest) SiteSecretKeys() []string {
	return m.secretKeys(ScopeSite)
}

// ProjectSecretKeys returns project-scoped secret keys.
func (m Manifest) ProjectSecretKeys() []string {
	return m.secretKeys(ScopeProject)
}

func (m Manifest) secretKeys(scope string) []string {
	var out []string
	for _, s := range m.Settings {
		if s.Type != "secret" {
			continue
		}
		if scope != "" && !s.HasScope(scope) {
			continue
		}
		out = append(out, s.Key)
	}
	return out
}

// KanbanSecretKeys returns kanban-scoped secret keys.
func (m Manifest) KanbanSecretKeys() []string {
	return m.secretKeys(ScopeKanban)
}

// UserSecretKeys returns user-scoped secret keys (Notify me / Profile).
func (m Manifest) UserSecretKeys() []string {
	return m.secretKeys(ScopeUser)
}

// TeamSecretKeys returns secret keys for a project's workflow (project or kanban).
func (m Manifest) TeamSecretKeys(workflowMode string) []string {
	return m.secretKeys(WorkflowSettingScope(workflowMode))
}

// HasProjectSettings reports whether any setting is configurable on classic projects.
func (m Manifest) HasProjectSettings() bool {
	return m.hasScopeSettings(ScopeProject)
}

// HasKanbanSettings reports whether any setting is configurable on kanban boards.
func (m Manifest) HasKanbanSettings() bool {
	return m.hasScopeSettings(ScopeKanban)
}

// HasMemberSettings reports whether any setting is for Notify me or Profile inbox.
func (m Manifest) HasMemberSettings() bool {
	return m.hasScopeSettings(ScopeUser)
}

func (m Manifest) hasScopeSettings(scope string) bool {
	for _, s := range m.Settings {
		if s.HasScope(scope) {
			return true
		}
	}
	return false
}

// HasFields reports whether the manifest registers custom fields.
func (m Manifest) HasFields() bool {
	return len(m.Fields) > 0
}

// HasProjectSurface reports whether the extension should appear on some project Extensions tab.
// Site-only hook extensions (for example join.request) stay in Admin → Extensions.
func (m Manifest) HasProjectSurface() bool {
	return m.HasProjectSettings() || m.HasKanbanSettings() || m.HasMemberSettings() || m.HasFields() || m.HasUI() || m.HasSurfaces()
}

// VisibleOnProject reports whether the extension should appear on this project's Extensions tab.
// Fields, sandboxed UI, and surfaces stay visible on both workflows. Team settings follow
// project (classic) vs kanban. User/member settings are a classic-project surface only.
func (m Manifest) VisibleOnProject(workflowMode string) bool {
	if m.HasFields() || m.HasUI() || m.HasSurfaces() {
		return true
	}
	if WorkflowSettingScope(workflowMode) == ScopeKanban {
		return m.HasKanbanSettings()
	}
	return m.HasProjectSettings() || m.HasMemberSettings()
}

// HasUI reports whether the manifest declares a sandboxed panel.
func (m Manifest) HasUI() bool {
	return strings.TrimSpace(m.UI) != ""
}

// HasSurfaces reports whether the manifest declares host API 2 surfaces (or a legacy ui panel).
func (m Manifest) HasSurfaces() bool {
	return len(m.ResolvedSurfaces()) > 0
}

// ResolvedSurfaces returns declared surfaces, synthesizing project.extensions from ui when needed.
func (m Manifest) ResolvedSurfaces() []Surface {
	out := make([]Surface, 0, len(m.Surfaces)+1)
	hasSettings := false
	for _, s := range m.Surfaces {
		id := strings.TrimSpace(s.ID)
		file := strings.TrimSpace(s.File)
		at := strings.ToLower(strings.TrimSpace(s.At))
		if id == "" || file == "" || at == "" {
			continue
		}
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = strings.TrimSpace(m.Name)
		}
		if at == SurfaceProjectExtensions {
			hasSettings = true
		}
		out = append(out, Surface{ID: id, File: file, At: at, Label: label})
	}
	if !hasSettings {
		if ui := strings.TrimSpace(m.UI); ui != "" {
			label := strings.TrimSpace(m.Name)
			out = append([]Surface{{ID: "settings", File: ui, At: SurfaceProjectExtensions, Label: label}}, out...)
		}
	}
	return out
}

// SurfaceByID returns a resolved surface by id.
func (m Manifest) SurfaceByID(id string) (Surface, bool) {
	id = strings.TrimSpace(id)
	for _, s := range m.ResolvedSurfaces() {
		if s.ID == id {
			return s, true
		}
	}
	return Surface{}, false
}

// SurfacesAt returns resolved surfaces for a placement.
func (m Manifest) SurfacesAt(at string) []Surface {
	at = strings.ToLower(strings.TrimSpace(at))
	out := make([]Surface, 0)
	for _, s := range m.ResolvedSurfaces() {
		if s.At == at {
			out = append(out, s)
		}
	}
	return out
}

// DestinationKey is the settings key used to look up the outbound URL.
func (d *Delivery) DestinationKey() string {
	if d == nil {
		return ""
	}
	return strings.TrimSpace(d.URLFrom)
}

// DeclaresHook reports whether the manifest registers on.
func (m Manifest) DeclaresHook(on string) bool {
	on = strings.TrimSpace(on)
	for _, h := range m.Hooks {
		if strings.TrimSpace(h.On) == on {
			return true
		}
	}
	return false
}

// ShowsOnList reports whether a field should appear in list/kanban task JSON.
func ShowsOnList(showOn []string) bool {
	for _, s := range showOn {
		if s == "kanban" || s == "list" {
			return true
		}
	}
	return false
}

// SettingsForScope returns settings configurable at the given scope.
func (m Manifest) SettingsForScope(scope string) []Setting {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = ScopeSite
	}
	out := make([]Setting, 0)
	for _, s := range m.Settings {
		if s.HasScope(scope) {
			out = append(out, s)
		}
	}
	return out
}

func settingMatchesKey(s Setting, key string) bool {
	if strings.TrimSpace(s.Key) == key {
		return true
	}
	if strings.TrimSpace(s.Type) == "field_filter" && (key == "field_key" || key == "field_value" || key == "field_filter") {
		return true
	}
	return wellKnownSettingKeys[strings.TrimSpace(s.Type)] == key
}

// HasSetting reports whether the manifest declares a settings key.
// Type field_filter also counts as field_key / field_value.
func (m Manifest) HasSetting(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for _, s := range m.Settings {
		if settingMatchesKey(s, key) {
			return true
		}
	}
	return false
}

// HasSettingForScope reports whether key is declared and configurable at scope.
func (m Manifest) HasSettingForScope(key, scope string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for _, s := range m.Settings {
		if s.HasScope(scope) && settingMatchesKey(s, key) {
			return true
		}
	}
	return false
}

// HasControl reports whether the manifest opts into a project UI control.
func (m Manifest) HasControl(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return false
	}
	for _, c := range m.Controls {
		if strings.ToLower(strings.TrimSpace(c)) == name {
			return true
		}
	}
	return false
}

// HasPermission reports whether the manifest requests a callback permission.
func (m Manifest) HasPermission(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, p := range m.Permissions {
		if strings.ToLower(strings.TrimSpace(p)) == name {
			return true
		}
	}
	return false
}

// DeclaresAction reports whether the manifest opts into an inbound action.
func (m Manifest) DeclaresAction(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, a := range m.Actions {
		if strings.ToLower(strings.TrimSpace(a)) == name {
			return true
		}
	}
	return false
}

// HasCallbackPermissions reports whether outbound JSON payloads may include a callback token.
func (m Manifest) HasCallbackPermissions() bool {
	for _, p := range m.Permissions {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" || p == PermStoreRead || p == PermStoreWrite {
			continue
		}
		return true
	}
	return false
}

// ValueSettingKeys returns setting keys stored in the extra values map (select/status/user/string/int).
func (m Manifest) ValueSettingKeys() map[string]struct{} {
	return m.ValueSettingKeysForScope("")
}

// ValueSettingKeysForScope returns value-map keys declared for scope. Empty scope means any.
func (m Manifest) ValueSettingKeysForScope(scope string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, s := range m.Settings {
		if scope != "" && !s.HasScope(scope) {
			continue
		}
		switch strings.TrimSpace(s.Type) {
		case "select", "status", "user", "string", "int":
			if _, wellKnown := wellKnownSettingKeys[s.Type]; wellKnown {
				continue
			}
			out[strings.TrimSpace(s.Key)] = struct{}{}
		}
	}
	return out
}
