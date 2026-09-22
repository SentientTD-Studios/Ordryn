package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// CustomFieldOption is one enum choice stored on a field definition.
type CustomFieldOption struct {
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
	Color string `json:"color,omitempty"`
}

// CustomFieldDef is a registered custom field (from an extension, later admin-created).
type CustomFieldDef struct {
	FieldKey    string              `json:"field_key"`
	ExtensionID string              `json:"extension_id,omitempty"`
	LocalKey    string              `json:"local_key"`
	Label       string              `json:"label"`
	Description string              `json:"description,omitempty"`
	Type        string              `json:"type"`
	Required    bool                `json:"required"`
	Options     []CustomFieldOption `json:"options,omitempty"`
	ShowOn      []string            `json:"show_on"`
	Active      bool                `json:"active"`
}

// CustomFieldValue is one stored value for a task.
type CustomFieldValue struct {
	TaskID   int
	FieldKey string
	Value    json.RawMessage
}

// CreateCustomFieldTables creates field definition and value tables.
func CreateCustomFieldTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS custom_field_defs (
			field_key TEXT PRIMARY KEY,
			extension_id TEXT,
			local_key TEXT NOT NULL,
			label TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			type TEXT NOT NULL,
			required BOOLEAN NOT NULL DEFAULT FALSE,
			options JSONB NOT NULL DEFAULT '[]',
			show_on TEXT[] NOT NULL DEFAULT ARRAY['sidebar']::TEXT[],
			active BOOLEAN NOT NULL DEFAULT TRUE,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE IF NOT EXISTS custom_field_values (
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			field_key TEXT NOT NULL,
			value JSONB NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (task_id, field_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_custom_field_values_task_id ON custom_field_values(task_id)`,
		`CREATE INDEX IF NOT EXISTS idx_custom_field_defs_extension_id ON custom_field_defs(extension_id)`,
	}
	for _, q := range stmts {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			return fmt.Errorf("create custom field tables: %w", err)
		}
	}
	return nil
}

// SyncExtensionFieldDefs upserts defs for loaded extensions and deactivates missing ones.
func SyncExtensionFieldDefs(byExtension map[string][]CustomFieldDef) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	keep := make([]string, 0)
	ctx := context.Background()
	for _, defs := range byExtension {
		for _, d := range defs {
			key := strings.TrimSpace(d.FieldKey)
			if key == "" {
				continue
			}
			keep = append(keep, key)
			opts, err := json.Marshal(d.Options)
			if err != nil {
				return err
			}
			if d.Options == nil {
				opts = []byte("[]")
			}
			showOn := d.ShowOn
			if len(showOn) == 0 {
				showOn = []string{"sidebar"}
			}
			var ext any
			if id := strings.TrimSpace(d.ExtensionID); id != "" {
				ext = id
			}
			if _, err := pool.Exec(ctx, `
				INSERT INTO custom_field_defs (
					field_key, extension_id, local_key, label, description, type, required, options, show_on, active, updated_at
				) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, TRUE, NOW())
				ON CONFLICT (field_key) DO UPDATE SET
					extension_id = EXCLUDED.extension_id,
					local_key = EXCLUDED.local_key,
					label = EXCLUDED.label,
					description = EXCLUDED.description,
					type = EXCLUDED.type,
					required = EXCLUDED.required,
					options = EXCLUDED.options,
					show_on = EXCLUDED.show_on,
					active = TRUE,
					updated_at = NOW()`,
				key, ext, d.LocalKey, d.Label, d.Description, d.Type, d.Required, opts, showOn); err != nil {
				return fmt.Errorf("upsert custom field def %s: %w", key, err)
			}
		}
	}

	if len(keep) == 0 {
		_, err = pool.Exec(ctx, `UPDATE custom_field_defs SET active = FALSE, updated_at = NOW() WHERE extension_id IS NOT NULL AND active = TRUE`)
		return err
	}
	_, err = pool.Exec(ctx, `
		UPDATE custom_field_defs SET active = FALSE, updated_at = NOW()
		WHERE extension_id IS NOT NULL AND active = TRUE AND NOT (field_key = ANY($1))`, keep)
	return err
}

// ListApplicableCustomFieldDefs returns active defs for extensions enabled on this project.
func ListApplicableCustomFieldDefs(projectID int, extensionIDs []string) ([]CustomFieldDef, error) {
	if projectID <= 0 || len(extensionIDs) == 0 {
		return nil, nil
	}
	enabled := make([]string, 0, len(extensionIDs))
	for _, id := range extensionIDs {
		site, err := GetExtensionSettings(id)
		if err != nil {
			return nil, err
		}
		if !site.Enabled {
			continue
		}
		proj, err := GetExtensionProjectSettings(id, projectID)
		if err != nil {
			return nil, err
		}
		if !proj.Enabled {
			continue
		}
		enabled = append(enabled, id)
	}
	return ListCustomFieldDefsForExtensions(enabled)
}

func ListCustomFieldDefsForExtensions(extensionIDs []string) ([]CustomFieldDef, error) {
	out := make([]CustomFieldDef, 0)
	if len(extensionIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), `
		SELECT field_key, COALESCE(extension_id, ''), local_key, label, description, type, required, options, show_on, active
		FROM custom_field_defs
		WHERE active = TRUE AND extension_id = ANY($1)
		ORDER BY extension_id, local_key`, extensionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		d, err := scanCustomFieldDef(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func scanCustomFieldDef(row interface{ Scan(dest ...any) error }) (CustomFieldDef, error) {
	var d CustomFieldDef
	var opts []byte
	if err := row.Scan(&d.FieldKey, &d.ExtensionID, &d.LocalKey, &d.Label, &d.Description, &d.Type, &d.Required, &opts, &d.ShowOn, &d.Active); err != nil {
		return d, err
	}
	if len(opts) > 0 && string(opts) != "null" {
		if err := json.Unmarshal(opts, &d.Options); err != nil {
			return d, err
		}
	}
	if d.Options == nil {
		d.Options = []CustomFieldOption{}
	}
	if d.ShowOn == nil {
		d.ShowOn = []string{"sidebar"}
	}
	return d, nil
}

// GetCustomFieldValuesForTasks returns stored values keyed by task id then field key.
func GetCustomFieldValuesForTasks(taskIDs []int) (map[int]map[string]json.RawMessage, error) {
	out := make(map[int]map[string]json.RawMessage)
	if len(taskIDs) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)

	rows, err := pool.Query(context.Background(),
		`SELECT task_id, field_key, value FROM custom_field_values WHERE task_id = ANY($1)`, taskIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var taskID int
		var key string
		var raw []byte
		if err := rows.Scan(&taskID, &key, &raw); err != nil {
			return nil, err
		}
		if out[taskID] == nil {
			out[taskID] = make(map[string]json.RawMessage)
		}
		out[taskID][key] = json.RawMessage(raw)
	}
	return out, rows.Err()
}

// UpsertCustomFieldValue writes one task field value.
func UpsertCustomFieldValue(taskID int, fieldKey string, value json.RawMessage) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO custom_field_values (task_id, field_key, value, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (task_id, field_key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()`,
		taskID, fieldKey, []byte(value))
	return err
}

// DeleteCustomFieldValue clears one task field.
func DeleteCustomFieldValue(taskID int, fieldKey string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`DELETE FROM custom_field_values WHERE task_id = $1 AND field_key = $2`, taskID, fieldKey)
	return err
}

// DeleteTaskFieldValuesExcept removes values whose keys are not in keepKeys.
func DeleteTaskFieldValuesExcept(taskID int, keepKeys []string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	if len(keepKeys) == 0 {
		_, err = pool.Exec(context.Background(), `DELETE FROM custom_field_values WHERE task_id = $1`, taskID)
		return err
	}
	_, err = pool.Exec(context.Background(),
		`DELETE FROM custom_field_values WHERE task_id = $1 AND NOT (field_key = ANY($2))`, taskID, keepKeys)
	return err
}

// UserDisplayNames returns a display name for each user id (username, else email).
func UserDisplayNames(ids []int) (map[int]string, error) {
	out := make(map[int]string)
	if len(ids) == 0 {
		return out, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT id, COALESCE(NULLIF(user_name, ''), email, '') FROM users WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
