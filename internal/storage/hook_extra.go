package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"GoTodo/internal/crypto/secret"

	"github.com/jackc/pgx/v5"
)

const (
	DeliveryStatusPending = "pending"
	DeliveryStatusSent    = "sent"
	DeliveryStatusFailed  = "failed"
	DeliveryStatusDigest  = "digest"

	maxDeliveryPayload = 8000
)

// GetExtensionMemberSettings returns per-user settings (project_id 0 = personal inbox).
func GetExtensionMemberSettings(extensionID string, projectID, userID int) (ExtensionMemberSettings, error) {
	out := emptyExtensionMemberSettings()
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" || userID <= 0 {
		return out, fmt.Errorf("extension id and user required")
	}
	if projectID < 0 {
		return out, fmt.Errorf("project id required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return out, err
	}
	defer CloseDatabase(pool)
	var raw []byte
	err = pool.QueryRow(context.Background(),
		`SELECT data FROM extension_member_settings WHERE extension_id = $1 AND project_id = $2 AND user_id = $3`,
		extensionID, projectID, userID).Scan(&raw)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return out, nil
		}
		return out, err
	}
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return emptyExtensionMemberSettings(), err
	}
	return out, nil
}

func emptyExtensionMemberSettings() ExtensionMemberSettings {
	return ExtensionMemberSettings{
		Triggers:  []string{},
		Templates: map[string]string{},
	}
}

// UpsertExtensionMemberSettings writes per-user hook settings and preserves last delivery info.
func UpsertExtensionMemberSettings(extensionID string, projectID, userID int, next ExtensionMemberSettings) error {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" || userID <= 0 {
		return fmt.Errorf("extension id and user required")
	}
	if projectID < 0 {
		return fmt.Errorf("project id required")
	}
	prev, err := GetExtensionMemberSettings(extensionID, projectID, userID)
	if err != nil {
		return err
	}
	if next.Triggers == nil {
		next.Triggers = []string{}
	}
	if next.Templates == nil {
		next.Templates = map[string]string{}
	}
	next.LastError = prev.LastError
	next.LastDeliveryAt = prev.LastDeliveryAt
	return writeExtensionMemberSettings(extensionID, projectID, userID, next)
}

func writeExtensionMemberSettings(extensionID string, projectID, userID int, data ExtensionMemberSettings) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(), `
		INSERT INTO extension_member_settings (extension_id, project_id, user_id, data, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (extension_id, project_id, user_id) DO UPDATE SET data = EXCLUDED.data, updated_at = NOW()`,
		extensionID, projectID, userID, raw)
	return err
}

// RecordExtensionMemberDelivery stores the last hook delivery attempt on the member settings row.
func RecordExtensionMemberDelivery(extensionID string, projectID, userID int, lastErr string) error {
	if userID <= 0 {
		return fmt.Errorf("user required")
	}
	s, err := GetExtensionMemberSettings(extensionID, projectID, userID)
	if err != nil {
		return err
	}
	s.LastError = lastErr
	s.LastDeliveryAt = time.Now().UTC().Format(time.RFC3339)
	return writeExtensionMemberSettings(extensionID, projectID, userID, s)
}

// ListEnabledMemberSettings returns enabled member destinations for a project (or personal when projectID is 0).
func ListEnabledMemberSettings(extensionID string, projectID int) ([]MemberDestination, error) {
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return nil, fmt.Errorf("extension id required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT user_id, data FROM extension_member_settings WHERE extension_id = $1 AND project_id = $2`,
		extensionID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemberDestination
	for rows.Next() {
		var userID int
		var raw []byte
		if err := rows.Scan(&userID, &raw); err != nil {
			return nil, err
		}
		s := emptyExtensionMemberSettings()
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &s)
		}
		if !s.Enabled || userID <= 0 {
			continue
		}
		out = append(out, MemberDestination{UserID: userID, Settings: s})
	}
	return out, rows.Err()
}

// MemberDestination is an enabled per-user hook destination.
type MemberDestination struct {
	UserID   int
	Settings ExtensionMemberSettings
}

// ListOverdueHookTasks returns incomplete project tasks whose due date has passed and has not been notified.
func ListOverdueHookTasks(limit int) ([]int, error) {
	if limit <= 0 {
		limit = 200
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT t.id FROM tasks t
		WHERE t.project_id IS NOT NULL
		  AND t.due_date IS NOT NULL
		  AND t.due_date < CURRENT_DATE
		  AND COALESCE(t.completed, false) = false
		  AND NOT EXISTS (
		    SELECT 1 FROM hook_overdue_sent s WHERE s.task_id = t.id AND s.due_date = t.due_date
		  )
		ORDER BY t.due_date, t.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkOverdueHookSent records that overdue was sent for this task's current due date.
func MarkOverdueHookSent(taskID int) error {
	if taskID <= 0 {
		return fmt.Errorf("task required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO hook_overdue_sent (task_id, due_date, sent_at)
		SELECT id, due_date, NOW() FROM tasks WHERE id = $1 AND due_date IS NOT NULL
		ON CONFLICT (task_id, due_date) DO NOTHING`, taskID)
	return err
}

// ListDueSoonHookTasks returns incomplete project tasks due tomorrow that have not been notified.
func ListDueSoonHookTasks(limit int) ([]int, error) {
	if limit <= 0 {
		limit = 200
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT t.id FROM tasks t
		WHERE t.project_id IS NOT NULL
		  AND t.due_date IS NOT NULL
		  AND t.due_date = CURRENT_DATE + 1
		  AND COALESCE(t.completed, false) = false
		  AND NOT EXISTS (
		    SELECT 1 FROM hook_due_soon_sent s WHERE s.task_id = t.id AND s.due_date = t.due_date
		  )
		ORDER BY t.due_date, t.id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// MarkDueSoonHookSent records that due-soon was sent for this task's current due date.
func MarkDueSoonHookSent(taskID int) error {
	if taskID <= 0 {
		return fmt.Errorf("task required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO hook_due_soon_sent (task_id, due_date, sent_at)
		SELECT id, due_date, NOW() FROM tasks WHERE id = $1 AND due_date IS NOT NULL
		ON CONFLICT (task_id, due_date) DO NOTHING`, taskID)
	return err
}

// ExtensionDelivery is one outbound attempt (or queued retry).
type ExtensionDelivery struct {
	ID            int64
	ExtensionID   string
	ProjectID     int
	UserID        int
	TaskID        int
	EventType     string
	EventID       string
	URLHost       string
	Status        string
	HTTPCode      int
	Error         string
	Attempts      int
	CoalesceKey   string
	Payload       string
	NextAttemptAt time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// InsertExtensionDelivery stores a delivery row and returns its id.
func InsertExtensionDelivery(d ExtensionDelivery) (int64, error) {
	d.ExtensionID = strings.TrimSpace(d.ExtensionID)
	if d.ExtensionID == "" {
		return 0, fmt.Errorf("extension id required")
	}
	if d.Status == "" {
		d.Status = DeliveryStatusPending
	}
	if len(d.Payload) > maxDeliveryPayload {
		d.Payload = d.Payload[:maxDeliveryPayload]
	}
	if d.NextAttemptAt.IsZero() {
		d.NextAttemptAt = time.Now().UTC()
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var id int64
	err = pool.QueryRow(context.Background(), `
		INSERT INTO extension_deliveries (
			extension_id, project_id, user_id, task_id, event_type, event_id, url_host,
			status, http_code, error, attempts, coalesce_key, payload, next_attempt_at, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14, NOW(), NOW())
		RETURNING id`,
		d.ExtensionID, d.ProjectID, d.UserID, d.TaskID, d.EventType, d.EventID, d.URLHost,
		d.Status, d.HTTPCode, d.Error, d.Attempts, d.CoalesceKey, d.Payload, d.NextAttemptAt,
	).Scan(&id)
	return id, err
}

// UpdateExtensionDelivery records the result of an attempt.
func UpdateExtensionDelivery(id int64, status string, httpCode int, errText string, attempts int, nextAttempt time.Time) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE extension_deliveries
		SET status = $2, http_code = $3, error = $4, attempts = $5, next_attempt_at = $6, updated_at = NOW()
		WHERE id = $1`, id, status, httpCode, errText, attempts, nextAttempt)
	return err
}

// FindPendingCoalesceDelivery returns an unsent row for this coalesce key created recently.
func FindPendingCoalesceDelivery(coalesceKey string, window time.Duration) (int64, error) {
	coalesceKey = strings.TrimSpace(coalesceKey)
	if coalesceKey == "" {
		return 0, nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var id int64
	secs := int(window.Seconds())
	if secs < 1 {
		secs = 1
	}
	err = pool.QueryRow(context.Background(), `
		SELECT id FROM extension_deliveries
		WHERE coalesce_key = $1 AND status IN ($2, $3) AND created_at > NOW() - make_interval(secs => $4)
		ORDER BY id DESC LIMIT 1`,
		coalesceKey, DeliveryStatusPending, DeliveryStatusDigest, secs).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return id, nil
}

// ReplaceDeliveryPayload updates a queued delivery with a newer payload.
func ReplaceDeliveryPayload(id int64, eventID, payload string) error {
	if len(payload) > maxDeliveryPayload {
		payload = payload[:maxDeliveryPayload]
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		UPDATE extension_deliveries SET event_id = $2, payload = $3, updated_at = NOW() WHERE id = $1`,
		id, eventID, payload)
	return err
}

// ListRetryableDeliveries returns pending/failed rows due for another attempt.
func ListRetryableDeliveries(limit int) ([]ExtensionDelivery, error) {
	if limit <= 0 {
		limit = 50
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT id, extension_id, project_id, user_id, task_id, event_type, event_id, url_host,
		       status, http_code, error, attempts, coalesce_key, payload, next_attempt_at, created_at, updated_at
		FROM extension_deliveries
		WHERE status IN ($1, $2) AND next_attempt_at <= NOW() AND attempts < 8
		ORDER BY next_attempt_at, id
		LIMIT $3`, DeliveryStatusPending, DeliveryStatusFailed, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExtensionDelivery
	for rows.Next() {
		var d ExtensionDelivery
		if err := rows.Scan(
			&d.ID, &d.ExtensionID, &d.ProjectID, &d.UserID, &d.TaskID, &d.EventType, &d.EventID, &d.URLHost,
			&d.Status, &d.HTTPCode, &d.Error, &d.Attempts, &d.CoalesceKey, &d.Payload, &d.NextAttemptAt, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListRecentDeliveries returns the latest deliveries for a destination.
func ListRecentDeliveries(extensionID string, projectID, userID, limit int) ([]ExtensionDelivery, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT id, extension_id, project_id, user_id, task_id, event_type, event_id, url_host,
		       status, http_code, error, attempts, coalesce_key, payload, next_attempt_at, created_at, updated_at
		FROM extension_deliveries
		WHERE extension_id = $1 AND project_id = $2 AND user_id = $3
		ORDER BY id DESC
		LIMIT $4`, extensionID, projectID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExtensionDelivery
	for rows.Next() {
		var d ExtensionDelivery
		if err := rows.Scan(
			&d.ID, &d.ExtensionID, &d.ProjectID, &d.UserID, &d.TaskID, &d.EventType, &d.EventID, &d.URLHost,
			&d.Status, &d.HTTPCode, &d.Error, &d.Attempts, &d.CoalesceKey, &d.Payload, &d.NextAttemptAt, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListDigestDeliveries returns queued digest rows for a destination.
func ListDigestDeliveries(extensionID string, projectID, userID int) ([]ExtensionDelivery, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT id, extension_id, project_id, user_id, task_id, event_type, event_id, url_host,
		       status, http_code, error, attempts, coalesce_key, payload, next_attempt_at, created_at, updated_at
		FROM extension_deliveries
		WHERE extension_id = $1 AND project_id = $2 AND user_id = $3 AND status = $4
		ORDER BY id`, extensionID, projectID, userID, DeliveryStatusDigest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExtensionDelivery
	for rows.Next() {
		var d ExtensionDelivery
		if err := rows.Scan(
			&d.ID, &d.ExtensionID, &d.ProjectID, &d.UserID, &d.TaskID, &d.EventType, &d.EventID, &d.URLHost,
			&d.Status, &d.HTTPCode, &d.Error, &d.Attempts, &d.CoalesceKey, &d.Payload, &d.NextAttemptAt, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ListDistinctDigestDestinations returns destinations that have queued digest rows due now.
func ListDistinctDigestDestinations() ([]ExtensionDelivery, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT DISTINCT ON (extension_id, project_id, user_id)
			id, extension_id, project_id, user_id, task_id, event_type, event_id, url_host,
			status, http_code, error, attempts, coalesce_key, payload, next_attempt_at, created_at, updated_at
		FROM extension_deliveries
		WHERE status = $1 AND next_attempt_at <= NOW()
		ORDER BY extension_id, project_id, user_id, id`, DeliveryStatusDigest)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ExtensionDelivery
	for rows.Next() {
		var d ExtensionDelivery
		if err := rows.Scan(
			&d.ID, &d.ExtensionID, &d.ProjectID, &d.UserID, &d.TaskID, &d.EventType, &d.EventID, &d.URLHost,
			&d.Status, &d.HTTPCode, &d.Error, &d.Attempts, &d.CoalesceKey, &d.Payload, &d.NextAttemptAt, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// MarkDeliveriesStatus updates many rows.
func MarkDeliveriesStatus(ids []int64, status string) error {
	if len(ids) == 0 {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`UPDATE extension_deliveries SET status = $1, updated_at = NOW() WHERE id = ANY($2)`,
		status, ids)
	return err
}

// ProjectInboundWebhook is the generic inbound receiver config for a project.
type ProjectInboundWebhook struct {
	ProjectID      int
	OwnerUserID    int
	Secret         string
	Enabled        bool
	AllowCreate    bool
	AllowComment   bool
	LastError      string
	LastDeliveryAt *time.Time
	SecretSet      bool
}

// ProjectOwnerID returns the project owner, or 0 if unknown.
func (p *ProjectInboundWebhook) ProjectOwnerID() int {
	if p == nil {
		return 0
	}
	return p.OwnerUserID
}

// GetProjectInboundWebhook loads inbound settings. Secret is decrypted when present.
func GetProjectInboundWebhook(projectID int) (*ProjectInboundWebhook, error) {
	if projectID <= 0 {
		return nil, fmt.Errorf("project required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	var enc string
	var last sql.NullTime
	out := &ProjectInboundWebhook{ProjectID: projectID, AllowCreate: true, AllowComment: true}
	err = pool.QueryRow(context.Background(), `
		SELECT secret_enc, enabled, allow_create, allow_comment, last_error, last_delivery_at
		FROM project_inbound_webhooks WHERE project_id = $1`, projectID).Scan(
		&enc, &out.Enabled, &out.AllowCreate, &out.AllowComment, &out.LastError, &last)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return out, nil
		}
		return nil, err
	}
	if last.Valid {
		t := last.Time
		out.LastDeliveryAt = &t
	}
	if strings.TrimSpace(enc) != "" {
		plain, err := secret.Decrypt(enc)
		if err != nil {
			return nil, err
		}
		out.Secret = plain
		out.SecretSet = plain != ""
	}
	_ = pool.QueryRow(context.Background(), `SELECT user_id FROM projects WHERE id = $1`, projectID).Scan(&out.OwnerUserID)
	return out, nil
}

// ListEnabledProjectInboundWebhooks returns enabled inbound configs with decrypted secrets.
func ListEnabledProjectInboundWebhooks() ([]*ProjectInboundWebhook, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT w.project_id, w.secret_enc, w.enabled, w.allow_create, w.allow_comment, w.last_error, w.last_delivery_at, p.user_id
		FROM project_inbound_webhooks w
		JOIN projects p ON p.id = w.project_id
		WHERE w.enabled = true AND COALESCE(w.secret_enc, '') <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ProjectInboundWebhook
	for rows.Next() {
		var enc string
		var last sql.NullTime
		cfg := &ProjectInboundWebhook{}
		if err := rows.Scan(&cfg.ProjectID, &enc, &cfg.Enabled, &cfg.AllowCreate, &cfg.AllowComment, &cfg.LastError, &last, &cfg.OwnerUserID); err != nil {
			return nil, err
		}
		if last.Valid {
			t := last.Time
			cfg.LastDeliveryAt = &t
		}
		plain, err := secret.Decrypt(enc)
		if err != nil || strings.TrimSpace(plain) == "" {
			continue
		}
		cfg.Secret = plain
		cfg.SecretSet = true
		out = append(out, cfg)
	}
	return out, rows.Err()
}

// UpsertProjectInboundWebhook writes inbound settings. Empty secret keeps the current value.
func UpsertProjectInboundWebhook(projectID int, enabled, allowCreate, allowComment bool, newSecret string) (*ProjectInboundWebhook, error) {
	cur, err := GetProjectInboundWebhook(projectID)
	if err != nil {
		return nil, err
	}
	secretPlain := strings.TrimSpace(newSecret)
	if secretPlain == "" {
		secretPlain = cur.Secret
	}
	if secretPlain == "" {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		secretPlain = hex.EncodeToString(b)
	}
	enc, err := secret.Encrypt(secretPlain)
	if err != nil {
		return nil, err
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO project_inbound_webhooks (project_id, secret_enc, enabled, allow_create, allow_comment, last_error, last_delivery_at)
		VALUES ($1, $2, $3, $4, $5, COALESCE((SELECT last_error FROM project_inbound_webhooks WHERE project_id = $1), ''),
		        (SELECT last_delivery_at FROM project_inbound_webhooks WHERE project_id = $1))
		ON CONFLICT (project_id) DO UPDATE SET
			secret_enc = EXCLUDED.secret_enc,
			enabled = EXCLUDED.enabled,
			allow_create = EXCLUDED.allow_create,
			allow_comment = EXCLUDED.allow_comment`,
		projectID, enc, enabled, allowCreate, allowComment)
	if err != nil {
		return nil, err
	}
	return GetProjectInboundWebhook(projectID)
}

// RecordProjectInboundDelivery stores last inbound attempt.
func RecordProjectInboundDelivery(projectID int, lastErr string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO project_inbound_webhooks (project_id, secret_enc, last_error, last_delivery_at)
		VALUES ($1, '', $2, NOW())
		ON CONFLICT (project_id) DO UPDATE SET last_error = EXCLUDED.last_error, last_delivery_at = NOW()`,
		projectID, lastErr)
	return err
}

// GetHookTaskMessageID returns a stored provider message id for threading.
func GetHookTaskMessageID(extensionID string, projectID, taskID int) (string, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return "", err
	}
	defer CloseDatabase(pool)
	var id string
	err = pool.QueryRow(context.Background(),
		`SELECT message_id FROM hook_task_messages WHERE extension_id = $1 AND project_id = $2 AND task_id = $3`,
		extensionID, projectID, taskID).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return id, nil
}

// SetHookTaskMessageID stores a provider message id for later thread replies.
func SetHookTaskMessageID(extensionID string, projectID, taskID int, messageID string) error {
	messageID = strings.TrimSpace(messageID)
	if extensionID == "" || projectID <= 0 || taskID <= 0 || messageID == "" {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO hook_task_messages (extension_id, project_id, task_id, message_id, updated_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (extension_id, project_id, task_id) DO UPDATE SET message_id = EXCLUDED.message_id, updated_at = NOW()`,
		extensionID, projectID, taskID, messageID)
	return err
}

// UserTimezone returns the user's timezone or UTC.
func UserTimezone(userID int) string {
	if userID <= 0 {
		return "UTC"
	}
	p, err := GetUserProfileByID(userID)
	if err != nil || p == nil {
		return "UTC"
	}
	tz := strings.TrimSpace(p.Timezone)
	if tz == "" {
		return "UTC"
	}
	return tz
}

// SprintHookRow is a sprint due for a lifecycle hook today.
type SprintHookRow struct {
	SprintID  int
	ProjectID int
	Name      string
}

// ListSprintLifecycleHooks returns dated sprints whose start or end is today and not yet notified.
func ListSprintLifecycleHooks(eventType string, limit int) ([]SprintHookRow, error) {
	col := ""
	switch eventType {
	case "sprint.started":
		col = "start_date"
	case "sprint.ended":
		col = "end_date"
	default:
		return nil, fmt.Errorf("unknown sprint event")
	}
	if limit <= 0 {
		limit = 200
	}
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	q := fmt.Sprintf(`
		SELECT s.id, s.project_id, s.name
		FROM project_sprints s
		WHERE s.%s IS NOT NULL AND s.%s = CURRENT_DATE
		  AND NOT EXISTS (
		    SELECT 1 FROM hook_sprint_sent x WHERE x.sprint_id = s.id AND x.event_type = $1
		  )
		ORDER BY s.id
		LIMIT $2`, col, col)
	rows, err := pool.Query(context.Background(), q, eventType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SprintHookRow
	for rows.Next() {
		var r SprintHookRow
		if err := rows.Scan(&r.SprintID, &r.ProjectID, &r.Name); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TryMarkSprintHookSent records a sprint lifecycle send. Returns true when this caller owns the send.
func TryMarkSprintHookSent(sprintID int, eventType string) (bool, error) {
	if sprintID <= 0 || strings.TrimSpace(eventType) == "" {
		return false, fmt.Errorf("sprint and event required")
	}
	pool, err := OpenDatabase()
	if err != nil {
		return false, err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(), `
		INSERT INTO hook_sprint_sent (sprint_id, event_type, sent_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (sprint_id, event_type) DO NOTHING`, sprintID, eventType)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
