package extensions

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// CurrentHostAPI is the highest hook host API this build understands.
const CurrentHostAPI = 1

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,32}$`)

var knownEventHooks = map[string]struct{}{
	"task.created":          {},
	"task.updated":          {},
	"task.deleted":          {},
	"task.commented":        {},
	"task.reordered":        {},
	"task.claimed":          {},
	"task.unclaimed":        {},
	"task.due_changed":      {},
	"task.moved":            {},
	"task.project_changed":  {},
	"task.sprint_changed":   {},
	"task.tagged":           {},
	"task.overdue":          {},
	"task.mentioned":        {},
	"task.completed":        {},
	"task.reopened":         {},
	"task.due_soon":         {},
	"task.archived":         {},
	"task.restored":         {},
	"project.updated":       {},
	"project.archived":      {},
	"project.restored":      {},
	"project.member_joined": {},
	"project.member_left":   {},
	"sprint.created":        {},
	"sprint.started":        {},
	"sprint.ended":          {},
	"join.request":          {},
	"join.approved":         {},
	"join.denied":           {},
}

var knownSettingTypes = map[string]struct{}{
	"secret":       {},
	"project_ids":  {},
	"hook_select":  {},
	"bool":         {},
	"string":       {},
	"int":          {},
	"select":       {},
	"status":       {},
	"user":         {},
	"priority":     {},
	"tag_ids":      {},
	"time":         {},
	"digest":       {},
	"field_filter": {},
	"mention_map":  {},
}

// wellKnownSettingKeys maps specialized widgets to the storage key they bind to.
var wellKnownSettingKeys = map[string]string{
	"priority":     "min_priority",
	"tag_ids":      "tag_ids",
	"digest":       "digest",
	"mention_map":  "mention_map",
	"field_filter": "field_filter",
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
)

var knownPermissions = map[string]struct{}{
	PermTasksRead:     {},
	PermTasksWrite:    {},
	PermCommentsWrite: {},
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
	Hooks       []Hook            `json:"hooks,omitempty"`
	Delivery    *Delivery         `json:"delivery,omitempty"`
	Settings    []Setting         `json:"settings,omitempty"`
	Templates   map[string]string `json:"templates,omitempty"`
	Fields      []Field           `json:"fields,omitempty"`
	Controls    []string          `json:"controls,omitempty"`
	Permissions []string          `json:"permissions,omitempty"`
	Actions     []string          `json:"actions,omitempty"`
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

// Setting is a schema-driven form field (site admin, project team, or Notify me).
// Project-scoped well-known keys bind to hook filters: skip_self, claimed_only,
// claimed_is_me, min_priority, tag_ids, quiet_hours_start, quiet_hours_end,
// digest, field_key, field_value, field_filter, mention_map, status_only, and triggers.
type Setting struct {
	Key         string        `json:"key"`
	Type        string        `json:"type"`
	Label       string        `json:"label"`
	Description string        `json:"description,omitempty"`
	Required    bool          `json:"required"`
	Scope       string        `json:"scope,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
}

// ScopeName returns site (default), project (team channel), or member (Notify me).
func (s Setting) ScopeName() string {
	switch strings.ToLower(strings.TrimSpace(s.Scope)) {
	case "", ScopeSite:
		return ScopeSite
	case ScopeProject:
		return ScopeProject
	case ScopeMember:
		return ScopeMember
	default:
		return strings.TrimSpace(s.Scope)
	}
}

// ValidateManifest checks host_api 1 rules. folderName must equal id.
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
		switch s.ScopeName() {
		case ScopeSite, ScopeProject, ScopeMember:
		default:
			return fmt.Errorf("unknown settings scope %q for %s", s.Scope, key)
		}
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
		if scope != "" && s.ScopeName() != scope {
			continue
		}
		out = append(out, s.Key)
	}
	return out
}

// HasProjectSettings reports whether any setting is project-scoped.
func (m Manifest) HasProjectSettings() bool {
	for _, s := range m.Settings {
		if s.ScopeName() == ScopeProject {
			return true
		}
	}
	return false
}

// HasMemberSettings reports whether any setting is for Notify me or Profile inbox.
func (m Manifest) HasMemberSettings() bool {
	for _, s := range m.Settings {
		if s.ScopeName() == ScopeMember {
			return true
		}
	}
	return false
}

// HasFields reports whether the manifest registers custom fields.
func (m Manifest) HasFields() bool {
	return len(m.Fields) > 0
}

// HasProjectSurface reports whether the extension should appear on the project Extensions tab.
// Site-only hook extensions (for example join.request) stay in Admin → Extensions.
func (m Manifest) HasProjectSurface() bool {
	return m.HasProjectSettings() || m.HasMemberSettings() || m.HasFields() || m.HasUI()
}

// HasUI reports whether the manifest declares a sandboxed panel.
func (m Manifest) HasUI() bool {
	return strings.TrimSpace(m.UI) != ""
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

// SettingsForScope returns settings with the given scope (site, project, or member).
func (m Manifest) SettingsForScope(scope string) []Setting {
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope == "" {
		scope = ScopeSite
	}
	out := make([]Setting, 0)
	for _, s := range m.Settings {
		if s.ScopeName() == scope {
			out = append(out, s)
		}
	}
	return out
}

// HasSetting reports whether the manifest declares a settings key.
// Type field_filter also counts as field_key / field_value.
func (m Manifest) HasSetting(key string) bool {
	key = strings.TrimSpace(key)
	if key == "" {
		return false
	}
	for _, s := range m.Settings {
		k := strings.TrimSpace(s.Key)
		if k == key {
			return true
		}
		if strings.TrimSpace(s.Type) == "field_filter" && (key == "field_key" || key == "field_value" || key == "field_filter") {
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
		if strings.TrimSpace(p) != "" {
			return true
		}
	}
	return false
}

// ValueSettingKeys returns setting keys stored in the extra values map (select/status/user/string/int).
func (m Manifest) ValueSettingKeys() map[string]struct{} {
	out := make(map[string]struct{})
	for _, s := range m.Settings {
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
