package mods

import (
	"fmt"
	"regexp"
	"strings"
)

// CurrentHostAPI is the highest hook host API this build understands.
const CurrentHostAPI = 1

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,32}$`)

var knownEventHooks = map[string]struct{}{
	"task.created":    {},
	"task.updated":    {},
	"task.deleted":    {},
	"task.commented":  {},
	"task.reordered":  {},
	"project.updated": {},
	"join.request":    {},
}

var knownSettingTypes = map[string]struct{}{
	"secret":      {},
	"project_ids": {},
	"hook_select": {},
	"bool":        {},
}

var settingKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,32}$`)

const (
	ScopeSite    = "site"
	ScopeProject = "project"
)

var knownDeliveryTypes = map[string]struct{}{
	"discord.webhook": {},
}

// Manifest is the required data/mods/<id>/manifest.json document.
type Manifest struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Version   string            `json:"version"`
	HostAPI   int               `json:"host_api"`
	UI        string            `json:"ui,omitempty"`
	Hooks     []Hook            `json:"hooks,omitempty"`
	Delivery  *Delivery         `json:"delivery,omitempty"`
	Settings  []Setting         `json:"settings,omitempty"`
	Templates map[string]string `json:"templates,omitempty"`
}

// Hook is a registration on a named core extension point.
type Hook struct {
	On string `json:"on"`
}

// Delivery describes how event hooks are sent outbound.
type Delivery struct {
	Type    string `json:"type"`
	URLFrom string `json:"url_from"`
}

// Setting is a schema-driven form field (site admin or project owner).
type Setting struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Scope    string `json:"scope,omitempty"`
}

// ScopeName returns site (default) or project.
func (s Setting) ScopeName() string {
	switch strings.ToLower(strings.TrimSpace(s.Scope)) {
	case "", ScopeSite:
		return ScopeSite
	case ScopeProject:
		return ScopeProject
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
	if strings.TrimSpace(m.Version) == "" {
		return fmt.Errorf("version is required")
	}
	if m.HostAPI < 1 {
		return fmt.Errorf("host_api is required")
	}
	if m.HostAPI > CurrentHostAPI {
		return fmt.Errorf("needs Ordryn that supports host_api %d (this build supports %d)", m.HostAPI, CurrentHostAPI)
	}
	if ui := strings.TrimSpace(m.UI); ui != "" {
		if strings.Contains(ui, "..") || strings.HasPrefix(ui, "/") || strings.Contains(ui, ":") {
			return fmt.Errorf("ui path %q is not allowed", m.UI)
		}
	}
	seenOn := make(map[string]struct{})
	for _, h := range m.Hooks {
		on := strings.TrimSpace(h.On)
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
	}
	if m.Delivery != nil {
		typ := strings.TrimSpace(m.Delivery.Type)
		if typ == "" {
			return fmt.Errorf("delivery.type is required")
		}
		if _, ok := knownDeliveryTypes[typ]; !ok {
			return fmt.Errorf("unknown delivery type %q", typ)
		}
		if strings.TrimSpace(m.Delivery.URLFrom) == "" {
			return fmt.Errorf("delivery.url_from is required")
		}
	}
	seenKeys := make(map[string]struct{})
	for _, s := range m.Settings {
		key := strings.TrimSpace(s.Key)
		if !settingKeyPattern.MatchString(key) {
			return fmt.Errorf("settings key %q is invalid", s.Key)
		}
		if _, dup := seenKeys[key]; dup {
			return fmt.Errorf("duplicate settings key %q", key)
		}
		seenKeys[key] = struct{}{}
		typ := strings.TrimSpace(s.Type)
		if _, ok := knownSettingTypes[typ]; !ok {
			return fmt.Errorf("unknown settings type %q for %s", s.Type, key)
		}
		if strings.TrimSpace(s.Label) == "" {
			return fmt.Errorf("settings label is required for %s", key)
		}
		switch s.ScopeName() {
		case ScopeSite, ScopeProject:
		default:
			return fmt.Errorf("unknown settings scope %q for %s", s.Scope, key)
		}
	}
	if m.Delivery != nil {
		from := strings.TrimSpace(m.Delivery.URLFrom)
		if _, ok := seenKeys[from]; !ok {
			return fmt.Errorf("delivery.url_from %q is not a settings key", from)
		}
	}
	for on := range m.Templates {
		if _, ok := knownEventHooks[on]; !ok {
			return fmt.Errorf("unknown template hook %q", on)
		}
	}
	return nil
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

// SettingsForScope returns settings with the given scope (site or project).
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
