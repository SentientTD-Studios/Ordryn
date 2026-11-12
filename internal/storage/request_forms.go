package storage

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Request form limits.
const (
	MaxRequestFormTitleLen  = 120
	MaxRequestFormIntroLen  = 2000
	MaxRequestFormFields    = 20
	MaxRequestFormLabelLen  = 120
	MaxRequestFormOptions   = 30
	MaxRequestFormAnswerLen = 4000
	MaxRequestFormsPerProj  = 25
)

// Request form field types.
const (
	FormFieldText     = "text"
	FormFieldTextarea = "textarea"
	FormFieldSelect   = "select"
	FormFieldDate     = "date"
	FormFieldNumber   = "number"
)

// ErrRequestFormNotFound is returned when a form does not exist.
var ErrRequestFormNotFound = errors.New("request form not found")

// RequestFormField is one question on a form.
type RequestFormField struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	Help     string   `json:"help,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// RequestForm turns submissions into tasks in one project. OwnerID is the
// manager whose access creates the tasks (whoever saved the form last).
type RequestForm struct {
	ID          int
	ProjectID   int
	Token       string
	Title       string
	Intro       string
	Fields      []RequestFormField
	Priority    int
	TagIDs      []int
	Enabled     bool
	OwnerID     int
	Submissions int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateRequestFormTables creates request_forms and request_form_submissions.
func CreateRequestFormTables() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS request_forms (
			id SERIAL PRIMARY KEY,
			project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			token VARCHAR(64) NOT NULL UNIQUE,
			title VARCHAR(120) NOT NULL,
			intro TEXT NOT NULL DEFAULT '',
			fields JSONB NOT NULL DEFAULT '[]',
			priority INTEGER NOT NULL DEFAULT 0,
			tag_ids INTEGER[] NOT NULL DEFAULT '{}',
			enabled BOOLEAN NOT NULL DEFAULT TRUE,
			owner_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_forms_project ON request_forms(project_id)`,
		`CREATE TABLE IF NOT EXISTS request_form_submissions (
			id SERIAL PRIMARY KEY,
			form_id INTEGER NOT NULL REFERENCES request_forms(id) ON DELETE CASCADE,
			user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_request_form_submissions_form ON request_form_submissions(form_id, created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_request_form_submissions_user ON request_form_submissions(user_id, created_at)`,
	} {
		if _, err := pool.Exec(context.Background(), stmt); err != nil {
			return fmt.Errorf("request forms: %v", err)
		}
	}
	return nil
}

// NewRequestFormToken returns a random URL-safe token for a form link.
func NewRequestFormToken() (string, error) {
	b := make([]byte, 18)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

const requestFormSelect = `
	SELECT f.id, f.project_id, f.token, f.title, f.intro, f.fields, f.priority, f.tag_ids, f.enabled,
	       COALESCE(f.owner_id, 0),
	       (SELECT COUNT(*) FROM request_form_submissions s WHERE s.form_id = f.id),
	       f.created_at, f.updated_at
	FROM request_forms f`

func scanRequestForm(row pgx.Row) (*RequestForm, error) {
	var f RequestForm
	var fields []byte
	if err := row.Scan(&f.ID, &f.ProjectID, &f.Token, &f.Title, &f.Intro, &fields, &f.Priority, &f.TagIDs,
		&f.Enabled, &f.OwnerID, &f.Submissions, &f.CreatedAt, &f.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRequestFormNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(fields, &f.Fields); err != nil {
		return nil, err
	}
	if f.Fields == nil {
		f.Fields = []RequestFormField{}
	}
	if f.TagIDs == nil {
		f.TagIDs = []int{}
	}
	return &f, nil
}

// ListRequestForms returns a project's forms, oldest first.
func ListRequestForms(projectID int) ([]RequestForm, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), requestFormSelect+` WHERE f.project_id = $1 ORDER BY f.id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RequestForm{}
	for rows.Next() {
		f, err := scanRequestForm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetRequestForm loads a form by id within a project.
func GetRequestForm(projectID, formID int) (*RequestForm, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanRequestForm(pool.QueryRow(context.Background(),
		requestFormSelect+` WHERE f.project_id = $1 AND f.id = $2`, projectID, formID))
}

// GetRequestFormByToken loads a form by its link token.
func GetRequestFormByToken(token string) (*RequestForm, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, err
	}
	defer CloseDatabase(pool)
	return scanRequestForm(pool.QueryRow(context.Background(), requestFormSelect+` WHERE f.token = $1`, token))
}

// CountRequestForms returns how many forms a project has.
func CountRequestForms(projectID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM request_forms WHERE project_id = $1`, projectID).Scan(&n)
	return n, err
}

// SaveRequestForm inserts (ID == 0) or updates a form. Token is only set on insert.
func SaveRequestForm(f *RequestForm) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	fields, err := json.Marshal(f.Fields)
	if err != nil {
		return err
	}
	tags := f.TagIDs
	if tags == nil {
		tags = []int{}
	}
	var owner any
	if f.OwnerID > 0 {
		owner = f.OwnerID
	}
	ctx := context.Background()
	if f.ID == 0 {
		return pool.QueryRow(ctx, `
			INSERT INTO request_forms (project_id, token, title, intro, fields, priority, tag_ids, enabled, owner_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id, created_at, updated_at`,
			f.ProjectID, f.Token, f.Title, f.Intro, fields, f.Priority, tags, f.Enabled, owner).
			Scan(&f.ID, &f.CreatedAt, &f.UpdatedAt)
	}
	tag, err := pool.Exec(ctx, `
		UPDATE request_forms SET title = $3, intro = $4, fields = $5, priority = $6, tag_ids = $7,
		       enabled = $8, owner_id = $9, updated_at = NOW()
		WHERE project_id = $1 AND id = $2`,
		f.ProjectID, f.ID, f.Title, f.Intro, fields, f.Priority, tags, f.Enabled, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRequestFormNotFound
	}
	return nil
}

// RotateRequestFormToken gives a form a new link; the old one stops working.
func RotateRequestFormToken(projectID, formID int, token string) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`UPDATE request_forms SET token = $3, updated_at = NOW() WHERE project_id = $1 AND id = $2`,
		projectID, formID, token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRequestFormNotFound
	}
	return nil
}

// DeleteRequestForm removes a form. Tasks it created stay.
func DeleteRequestForm(projectID, formID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	tag, err := pool.Exec(context.Background(),
		`DELETE FROM request_forms WHERE project_id = $1 AND id = $2`, projectID, formID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRequestFormNotFound
	}
	return nil
}

// RecordRequestFormSubmission logs that userID submitted formID, creating taskID.
func RecordRequestFormSubmission(formID, userID, taskID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO request_form_submissions (form_id, user_id, task_id) VALUES ($1, $2, $3)`,
		formID, userID, taskID)
	return err
}

// CountRecentRequestFormSubmissions counts a user's submissions to any form since a time.
func CountRecentRequestFormSubmissions(userID int, since time.Time) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)
	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM request_form_submissions WHERE user_id = $1 AND created_at >= $2`,
		userID, since).Scan(&n)
	return n, err
}
