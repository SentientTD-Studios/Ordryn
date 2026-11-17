package domain

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/storage"
)

// MaxRequestSubmissionsPerHour limits how many requests one person files per hour.
const MaxRequestSubmissionsPerHour = 20

// ErrRequestFormClosed is returned when a form is turned off or can't create tasks.
var ErrRequestFormClosed = fmt.Errorf("%w: this form isn't accepting requests", ErrValidation)

// RequestFormInput is what a manager edits.
type RequestFormInput struct {
	Title    string
	Intro    string
	Fields   []storage.RequestFormField
	Priority int
	TagIDs   []int
	Enabled  bool
}

var formFieldIDPattern = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func validFormFieldType(t string) bool {
	switch t {
	case storage.FormFieldText, storage.FormFieldTextarea, storage.FormFieldSelect, storage.FormFieldDate, storage.FormFieldNumber:
		return true
	}
	return false
}

// normalizeRequestForm validates and cleans manager input.
func normalizeRequestForm(projectID int, in RequestFormInput) (RequestFormInput, error) {
	out := in
	out.Title = strings.TrimSpace(in.Title)
	if out.Title == "" {
		return out, fmt.Errorf("%w: give the form a title", ErrValidation)
	}
	if runeLen(out.Title) > storage.MaxRequestFormTitleLen {
		return out, fmt.Errorf("%w: title is too long (max %d)", ErrValidation, storage.MaxRequestFormTitleLen)
	}
	out.Intro = strings.TrimSpace(in.Intro)
	if runeLen(out.Intro) > storage.MaxRequestFormIntroLen {
		return out, fmt.Errorf("%w: intro is too long (max %d)", ErrValidation, storage.MaxRequestFormIntroLen)
	}
	if in.Priority < 0 || in.Priority > 3 {
		return out, fmt.Errorf("%w: priority must be 0-3", ErrValidation)
	}
	if len(in.Fields) > storage.MaxRequestFormFields {
		return out, fmt.Errorf("%w: at most %d questions", ErrValidation, storage.MaxRequestFormFields)
	}
	seen := map[string]bool{}
	out.Fields = make([]storage.RequestFormField, 0, len(in.Fields))
	for i, f := range in.Fields {
		f.ID = strings.TrimSpace(f.ID)
		if f.ID == "" {
			f.ID = "q" + strconv.Itoa(i+1)
		}
		if !formFieldIDPattern.MatchString(f.ID) || f.ID == "summary" {
			return out, fmt.Errorf("%w: question id %q must be lowercase letters, digits or _", ErrValidation, f.ID)
		}
		if seen[f.ID] {
			return out, fmt.Errorf("%w: duplicate question id %q", ErrValidation, f.ID)
		}
		seen[f.ID] = true
		f.Label = strings.TrimSpace(f.Label)
		if f.Label == "" || runeLen(f.Label) > storage.MaxRequestFormLabelLen {
			return out, fmt.Errorf("%w: each question needs a label of at most %d characters", ErrValidation, storage.MaxRequestFormLabelLen)
		}
		f.Help = strings.TrimSpace(f.Help)
		if runeLen(f.Help) > storage.MaxRequestFormLabelLen*2 {
			return out, fmt.Errorf("%w: help text for %q is too long", ErrValidation, f.Label)
		}
		if !validFormFieldType(f.Type) {
			return out, fmt.Errorf("%w: unknown question type %q", ErrValidation, f.Type)
		}
		if f.Type == storage.FormFieldSelect {
			opts := make([]string, 0, len(f.Options))
			optSeen := map[string]bool{}
			for _, o := range f.Options {
				o = strings.TrimSpace(o)
				if o == "" || optSeen[o] {
					continue
				}
				if runeLen(o) > storage.MaxRequestFormLabelLen {
					return out, fmt.Errorf("%w: an option for %q is too long", ErrValidation, f.Label)
				}
				optSeen[o] = true
				opts = append(opts, o)
			}
			if len(opts) == 0 || len(opts) > storage.MaxRequestFormOptions {
				return out, fmt.Errorf("%w: %q needs 1-%d options", ErrValidation, f.Label, storage.MaxRequestFormOptions)
			}
			f.Options = opts
		} else {
			f.Options = nil
		}
		out.Fields = append(out.Fields, f)
	}
	tags := []int{}
	if len(in.TagIDs) > 0 {
		projectTags, err := storage.GetProjectTags(projectID)
		if err != nil {
			return out, err
		}
		valid := map[int]bool{}
		for _, t := range projectTags {
			valid[t.ID] = true
		}
		for _, id := range in.TagIDs {
			if !valid[id] {
				return out, fmt.Errorf("%w: tag %d isn't a tag of this project", ErrValidation, id)
			}
			tags = append(tags, id)
		}
	}
	out.TagIDs = tags
	return out, nil
}

// ListRequestFormsForManager lists a project's forms (project:manage).
func ListRequestFormsForManager(ctx context.Context, userID, projectID int) ([]storage.RequestForm, error) {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	return storage.ListRequestForms(projectID)
}

// CreateRequestForm adds a form; the caller becomes its owner.
func CreateRequestForm(ctx context.Context, userID, projectID int, in RequestFormInput) (*storage.RequestForm, error) {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	n, err := storage.CountRequestForms(projectID)
	if err != nil {
		return nil, err
	}
	if n >= storage.MaxRequestFormsPerProj {
		return nil, fmt.Errorf("%w: a project can have at most %d forms", ErrValidation, storage.MaxRequestFormsPerProj)
	}
	clean, err := normalizeRequestForm(projectID, in)
	if err != nil {
		return nil, err
	}
	token, err := storage.NewRequestFormToken()
	if err != nil {
		return nil, err
	}
	f := &storage.RequestForm{
		ProjectID: projectID, Token: token, Title: clean.Title, Intro: clean.Intro, Fields: clean.Fields,
		Priority: clean.Priority, TagIDs: clean.TagIDs, Enabled: clean.Enabled, OwnerID: userID,
	}
	if err := storage.SaveRequestForm(f); err != nil {
		return nil, err
	}
	_ = storage.LogProjectEvent(projectID, userID, "request_form_created", map[string]interface{}{"form_id": f.ID, "title": f.Title})
	return storage.GetRequestForm(projectID, f.ID)
}

// UpdateRequestForm replaces a form's settings; the caller becomes its owner.
func UpdateRequestForm(ctx context.Context, userID, projectID, formID int, in RequestFormInput) (*storage.RequestForm, error) {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	f, err := storage.GetRequestForm(projectID, formID)
	if err != nil {
		return nil, requestFormErr(err)
	}
	clean, err := normalizeRequestForm(projectID, in)
	if err != nil {
		return nil, err
	}
	f.Title, f.Intro, f.Fields = clean.Title, clean.Intro, clean.Fields
	f.Priority, f.TagIDs, f.Enabled, f.OwnerID = clean.Priority, clean.TagIDs, clean.Enabled, userID
	if err := storage.SaveRequestForm(f); err != nil {
		return nil, requestFormErr(err)
	}
	return storage.GetRequestForm(projectID, formID)
}

// RotateRequestFormLink gives a form a new URL; the old link stops working.
func RotateRequestFormLink(ctx context.Context, userID, projectID, formID int) (*storage.RequestForm, error) {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	token, err := storage.NewRequestFormToken()
	if err != nil {
		return nil, err
	}
	if err := storage.RotateRequestFormToken(projectID, formID, token); err != nil {
		return nil, requestFormErr(err)
	}
	return storage.GetRequestForm(projectID, formID)
}

// DeleteRequestFormForUser removes a form; tasks it created stay.
func DeleteRequestFormForUser(ctx context.Context, userID, projectID, formID int) error {
	_ = ctx
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return err
	}
	if err := storage.DeleteRequestForm(projectID, formID); err != nil {
		return requestFormErr(err)
	}
	_ = storage.LogProjectEvent(projectID, userID, "request_form_deleted", map[string]interface{}{"form_id": formID})
	return nil
}

func requestFormErr(err error) error {
	if errors.Is(err, storage.ErrRequestFormNotFound) {
		return ErrNotFound
	}
	return err
}

// PublicRequestForm is what a submitter sees.
type PublicRequestForm struct {
	Token       string
	Title       string
	Intro       string
	ProjectName string
	Fields      []storage.RequestFormField
}

// GetRequestFormForSubmitter loads an open form by link for a signed-in user.
func GetRequestFormForSubmitter(ctx context.Context, userID int, token string) (*PublicRequestForm, error) {
	_ = ctx
	f, err := openRequestForm(token)
	if err != nil {
		return nil, err
	}
	name := ""
	if p, err := storage.GetAccessibleProjectByID(f.ProjectID, f.OwnerID); err == nil && p != nil {
		name = p.Name
	}
	return &PublicRequestForm{Token: f.Token, Title: f.Title, Intro: f.Intro, ProjectName: name, Fields: f.Fields}, nil
}

// openRequestForm loads a form that is enabled and whose owner can still create
// tasks in its project. Anything else looks like a missing or closed form.
func openRequestForm(token string) (*storage.RequestForm, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, ErrNotFound
	}
	f, err := storage.GetRequestFormByToken(token)
	if err != nil {
		return nil, requestFormErr(err)
	}
	if !f.Enabled || f.OwnerID <= 0 {
		return nil, ErrRequestFormClosed
	}
	p, err := storage.GetAccessibleProjectByID(f.ProjectID, f.OwnerID)
	if err != nil || p == nil || p.Archived || !storage.HasProjectPerm(f.ProjectID, p.Role, storage.PermTasksCreate) {
		return nil, ErrRequestFormClosed
	}
	return f, nil
}

// SubmitRequestForm files a request: it creates a task in the form's project
// (as the form's owner) titled with the summary, with the answers in the
// description, and records who submitted it. Returns the new task id.
func SubmitRequestForm(ctx context.Context, userID int, token, summary string, answers map[string]string) (int, error) {
	f, err := openRequestForm(token)
	if err != nil {
		return 0, err
	}
	since := time.Now().Add(-time.Hour)
	if n, err := storage.CountRecentRequestFormSubmissions(userID, since); err == nil && n >= MaxRequestSubmissionsPerHour {
		return 0, fmt.Errorf("%w: you've sent a lot of requests in the last hour; try again later", ErrValidation)
	}
	summary = strings.TrimSpace(summary)
	if summary == "" {
		return 0, fmt.Errorf("%w: add a short summary", ErrValidation)
	}
	if runeLen(summary) > storage.MaxFeedbackPostTitleLen {
		return 0, fmt.Errorf("%w: summary is too long (max %d)", ErrValidation, storage.MaxFeedbackPostTitleLen)
	}
	body, err := composeRequestBody(f, answers)
	if err != nil {
		return 0, err
	}
	who := requestSubmitterLabel(userID)
	body += "\n\n---\n_Requested by " + who + " via the **" + escapeMarkdownInline(f.Title) + "** form._"

	pid := f.ProjectID
	taskID, err := CreateTask(ctx, f.OwnerID, CreateTaskInput{
		Title:       summary,
		Description: body,
		ProjectID:   &pid,
		Priority:    f.Priority,
		TagIDs:      f.TagIDs,
	})
	if err != nil {
		return 0, err
	}
	_ = storage.RecordRequestFormSubmission(f.ID, userID, taskID)
	_ = storage.LogTaskEvent(taskID, userID, "submitted_via_form", map[string]interface{}{"form_id": f.ID, "form": f.Title})
	return taskID, nil
}

// composeRequestBody checks answers against the form and renders them as markdown.
func composeRequestBody(f *storage.RequestForm, answers map[string]string) (string, error) {
	var b strings.Builder
	for _, q := range f.Fields {
		v := strings.TrimSpace(answers[q.ID])
		if v == "" {
			if q.Required {
				return "", fmt.Errorf("%w: %q is required", ErrValidation, q.Label)
			}
			continue
		}
		if runeLen(v) > storage.MaxRequestFormAnswerLen {
			return "", fmt.Errorf("%w: the answer to %q is too long", ErrValidation, q.Label)
		}
		switch q.Type {
		case storage.FormFieldSelect:
			ok := false
			for _, o := range q.Options {
				if o == v {
					ok = true
					break
				}
			}
			if !ok {
				return "", fmt.Errorf("%w: pick one of the options for %q", ErrValidation, q.Label)
			}
		case storage.FormFieldDate:
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return "", fmt.Errorf("%w: %q must be a date", ErrValidation, q.Label)
			}
		case storage.FormFieldNumber:
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				return "", fmt.Errorf("%w: %q must be a number", ErrValidation, q.Label)
			}
		case storage.FormFieldText:
			v = strings.Join(strings.Fields(v), " ")
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("**" + escapeMarkdownInline(q.Label) + "**\n")
		b.WriteString(v)
	}
	return b.String(), nil
}

func requestSubmitterLabel(userID int) string {
	// Usernames only: never put a submitter's email into the task.
	if name, _ := storage.UserNameAndEmail(userID); strings.TrimSpace(name) != "" {
		return "@" + strings.TrimSpace(name)
	}
	return "user #" + strconv.Itoa(userID)
}

// escapeMarkdownInline keeps user text from opening bold/italic/links in a label.
func escapeMarkdownInline(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `_`, `\_`, "`", "\\`", `[`, `\[`, `]`, `\]`)
	return r.Replace(s)
}
