package domain

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

// SyncCustomFieldDefs upserts defs from loaded extension manifests.
func SyncCustomFieldDefs() error {
	byExt := make(map[string][]storage.CustomFieldDef)
	for _, e := range extensions.LoadedEntries() {
		if len(e.Manifest.Fields) == 0 {
			byExt[e.ID] = nil
			continue
		}
		defs := make([]storage.CustomFieldDef, 0, len(e.Manifest.Fields))
		for _, f := range e.Manifest.Fields {
			opts := make([]storage.CustomFieldOption, 0, len(f.Options))
			for _, o := range f.Options {
				opts = append(opts, storage.CustomFieldOption{Value: o.Value, Label: o.Label, Color: o.Color})
			}
			defs = append(defs, storage.CustomFieldDef{
				FieldKey:    extensions.FieldKey(e.ID, f.Key),
				ExtensionID: e.ID,
				LocalKey:    f.Key,
				Label:       f.Label,
				Description: f.Description,
				Type:        f.Type,
				Required:    f.Required,
				Options:     opts,
				ShowOn:      f.ShowOn,
				Active:      true,
			})
		}
		byExt[e.ID] = defs
	}
	return storage.SyncExtensionFieldDefs(byExt)
}

// ApplicableFieldDefsForProject returns defs visible on this project's tasks.
func ApplicableFieldDefsForProject(projectID int) ([]storage.CustomFieldDef, error) {
	return storage.ListApplicableCustomFieldDefs(projectID, extensions.FieldExtensionIDs())
}

func defsByKey(defs []storage.CustomFieldDef) map[string]storage.CustomFieldDef {
	out := make(map[string]storage.CustomFieldDef, len(defs))
	for _, d := range defs {
		out[d.FieldKey] = d
	}
	return out
}

func keepFieldKeys(defs []storage.CustomFieldDef) []string {
	out := make([]string, 0, len(defs))
	for _, d := range defs {
		out = append(out, d.FieldKey)
	}
	return out
}

// ApplyTaskFields merges fields onto a task. Null / empty JSON clears a key.
func ApplyTaskFields(taskID, projectID, actorUserID int, patch map[string]json.RawMessage) error {
	if len(patch) == 0 {
		return nil
	}
	if projectID <= 0 {
		return fmt.Errorf("%w: custom fields require a project", ErrValidation)
	}
	defs, err := ApplicableFieldDefsForProject(projectID)
	if err != nil {
		return err
	}
	byKey := defsByKey(defs)
	changed := make([]string, 0)
	for key, raw := range patch {
		key = strings.TrimSpace(key)
		def, ok := byKey[key]
		if !ok {
			return fmt.Errorf("%w: unknown field %q", ErrValidation, key)
		}
		normalized, clear, err := normalizeFieldValue(def, projectID, raw)
		if err != nil {
			return fmt.Errorf("%w: %s: %s", ErrValidation, key, err.Error())
		}
		if clear {
			if def.Required {
				return fmt.Errorf("%w: %s is required", ErrValidation, key)
			}
			if err := storage.DeleteCustomFieldValue(taskID, key); err != nil {
				return err
			}
			changed = append(changed, key)
			continue
		}
		if err := storage.UpsertCustomFieldValue(taskID, key, normalized); err != nil {
			return err
		}
		changed = append(changed, key)
	}
	if len(changed) > 0 {
		_ = storage.LogTaskEvent(taskID, actorUserID, "edited", map[string]interface{}{"fields": changed})
	}
	return nil
}

// PruneInapplicableFieldValues drops values that no longer apply after a project change.
func PruneInapplicableFieldValues(taskID, projectID int) error {
	defs, err := ApplicableFieldDefsForProject(projectID)
	if err != nil {
		return err
	}
	return storage.DeleteTaskFieldValuesExcept(taskID, keepFieldKeys(defs))
}

func normalizeFieldValue(def storage.CustomFieldDef, projectID int, raw json.RawMessage) (json.RawMessage, bool, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, true, nil
	}
	switch def.Type {
	case "string":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, false, fmt.Errorf("must be a string")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, true, nil
		}
		out, _ := json.Marshal(s)
		return out, false, nil
	case "enum":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, false, fmt.Errorf("must be a string")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, true, nil
		}
		ok := false
		for _, o := range def.Options {
			if o.Value == s {
				ok = true
				break
			}
		}
		if !ok {
			return nil, false, fmt.Errorf("invalid option")
		}
		out, _ := json.Marshal(s)
		return out, false, nil
	case "url":
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, false, fmt.Errorf("must be a string")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return nil, true, nil
		}
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, false, fmt.Errorf("must be an http(s) URL")
		}
		out, _ := json.Marshal(s)
		return out, false, nil
	case "number":
		dec := json.NewDecoder(strings.NewReader(string(raw)))
		dec.UseNumber()
		var n json.Number
		if err := dec.Decode(&n); err != nil {
			return nil, false, fmt.Errorf("must be a number")
		}
		if _, err := n.Float64(); err != nil {
			return nil, false, fmt.Errorf("must be a number")
		}
		out, _ := json.Marshal(n)
		return out, false, nil
	case "boolean":
		var b bool
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, false, fmt.Errorf("must be a boolean")
		}
		out, _ := json.Marshal(b)
		return out, false, nil
	case "user":
		id, err := parseUserFieldID(raw)
		if err != nil {
			return nil, false, err
		}
		if id <= 0 {
			return nil, true, nil
		}
		if err := validateUserFieldMember(projectID, id); err != nil {
			return nil, false, err
		}
		out, _ := json.Marshal(id)
		return out, false, nil
	default:
		return nil, false, fmt.Errorf("unsupported type")
	}
}

func parseUserFieldID(raw json.RawMessage) (int, error) {
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		id := int(n)
		if float64(id) != n {
			return 0, fmt.Errorf("must be a user id")
		}
		return id, nil
	}
	var obj struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0, fmt.Errorf("must be a user id")
	}
	return obj.ID, nil
}

func validateUserFieldMember(projectID, userID int) error {
	if userID <= 0 {
		return nil
	}
	role, err := storage.GetProjectRole(projectID, userID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(role) == "" {
		return fmt.Errorf("user is not a project member")
	}
	return nil
}
