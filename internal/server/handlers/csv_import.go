package handlers

import (
	"GoTodo/internal/domain"
	"GoTodo/internal/live"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const (
	importMaxFileBytes = 5 * 1024 * 1024
	importMaxRows      = 5000
)

type importColumnMap struct {
	title       int
	description int
	completed   int
	dueDate     int
	project     int
	priority    int
	favorite    int
	tags        int
}

type stagedImportCols struct {
	Title       int `json:"title"`
	Description int `json:"description"`
	Completed   int `json:"completed"`
	DueDate     int `json:"due_date"`
	Project     int `json:"project"`
	Priority    int `json:"priority"`
	Favorite    int `json:"favorite"`
	Tags        int `json:"tags"`
}

type stagedImport struct {
	Cols stagedImportCols `json:"cols"`
	Rows [][]string       `json:"rows"`
}

type importPreviewRow struct {
	Title   string
	Project string
	DueDate string
	Tags    string
}

const importStagingSessionKey = "import_staging"

// APIV1ImportRouter handles /api/v2/import preview/confirm/cancel.
func APIV1ImportRouter(w http.ResponseWriter, r *http.Request) {
	sub := utils.ParseAPIV1Subpath(r, "import")
	switch {
	case sub == "preview" && r.Method == http.MethodPost:
		apiV1ImportPreview(w, r)
	case sub == "confirm" && r.Method == http.MethodPost:
		apiV1ImportConfirm(w, r)
	case sub == "cancel" && r.Method == http.MethodPost:
		apiV1ImportCancel(w, r)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

func apiV1ImportPreview(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	cols, rows, err := readImportUpload(r)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	wouldImport, wouldSkip, err := dryRunImportCounts(userID, cols, rows)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	if err := saveImportStaging(r, w, cols, rows); err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to stage import.")
		return
	}

	previewRows := buildImportPreviewRows(cols, rows, 10)
	out := make([]map[string]string, 0, len(previewRows))
	for _, row := range previewRows {
		out = append(out, map[string]string{
			"title":    row.Title,
			"project":  row.Project,
			"due_date": row.DueDate,
			"tags":     row.Tags,
		})
	}

	payload := map[string]interface{}{
		"preview":      out,
		"would_import": wouldImport,
		"would_skip":   wouldSkip,
		"total_rows":   len(rows),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

func apiV1ImportConfirm(w http.ResponseWriter, r *http.Request) {
	userID, ok := utils.GetAPIUserID(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}

	staging, err := loadImportStaging(r)
	if err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	cols := stagingColsToMap(staging.Cols)
	imported, skipped, importErr := importTasksFromCSV(userID, cols, staging.Rows)
	_ = clearImportStaging(r, w)
	if errors.Is(importErr, errImportProjectCreateForbidden) || errors.Is(importErr, errImportProjectReadOnly) {
		utils.APIJSONError(w, http.StatusForbidden, "forbidden", importErr.Error())
		return
	}
	if errors.Is(importErr, errImportProjectRequired) {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", importErr.Error())
		return
	}
	if importErr != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", importErr.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(struct {
		Imported int `json:"imported"`
		Skipped  int `json:"skipped"`
	}{
		Imported: imported,
		Skipped:  skipped,
	})
}

func apiV1ImportCancel(w http.ResponseWriter, r *http.Request) {
	if _, ok := utils.GetAPIUserID(r); !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	_ = clearImportStaging(r, w)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func readImportUpload(r *http.Request) (importColumnMap, [][]string, error) {
	if err := r.ParseMultipartForm(importMaxFileBytes); err != nil {
		return importColumnMap{}, nil, fmt.Errorf("file too large (max 5MB)")
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		return importColumnMap{}, nil, fmt.Errorf("missing file upload")
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, importMaxFileBytes+1))
	if err != nil {
		return importColumnMap{}, nil, fmt.Errorf("failed to read file")
	}
	if len(data) > importMaxFileBytes {
		return importColumnMap{}, nil, fmt.Errorf("file too large (max 5MB)")
	}

	cols, rows, err := parseImportCSV(data)
	if err != nil {
		return importColumnMap{}, nil, err
	}
	if len(rows) == 0 {
		return importColumnMap{}, nil, fmt.Errorf("no data rows found in CSV")
	}
	if len(rows) > importMaxRows {
		return importColumnMap{}, nil, fmt.Errorf("too many rows (max %d)", importMaxRows)
	}
	return cols, rows, nil
}

func stagingColsFromMap(cols importColumnMap) stagedImportCols {
	return stagedImportCols{
		Title:       cols.title,
		Description: cols.description,
		Completed:   cols.completed,
		DueDate:     cols.dueDate,
		Project:     cols.project,
		Priority:    cols.priority,
		Favorite:    cols.favorite,
		Tags:        cols.tags,
	}
}

func stagingColsToMap(cols stagedImportCols) importColumnMap {
	return importColumnMap{
		title:       cols.Title,
		description: cols.Description,
		completed:   cols.Completed,
		dueDate:     cols.DueDate,
		project:     cols.Project,
		priority:    cols.Priority,
		favorite:    cols.Favorite,
		tags:        cols.Tags,
	}
}

func saveImportStaging(r *http.Request, w http.ResponseWriter, cols importColumnMap, rows [][]string) error {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return err
	}
	payload, err := json.Marshal(stagedImport{
		Cols: stagingColsFromMap(cols),
		Rows: rows,
	})
	if err != nil {
		return err
	}
	sess.Values[importStagingSessionKey] = string(payload)
	return sess.Save(r, w)
}

func loadImportStaging(r *http.Request) (*stagedImport, error) {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return nil, err
	}
	raw, ok := sess.Values[importStagingSessionKey]
	if !ok || raw == nil {
		return nil, fmt.Errorf("no import preview to confirm")
	}
	var staging stagedImport
	switch v := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &staging); err != nil {
			return nil, fmt.Errorf("invalid import staging")
		}
	default:
		return nil, fmt.Errorf("invalid import staging")
	}
	if len(staging.Rows) == 0 {
		return nil, fmt.Errorf("no import preview to confirm")
	}
	return &staging, nil
}

func clearImportStaging(r *http.Request, w http.ResponseWriter) error {
	sess, err := sessionstore.Store.Get(r, "session")
	if err != nil {
		return err
	}
	delete(sess.Values, importStagingSessionKey)
	return sess.Save(r, w)
}

func buildImportPreviewRows(cols importColumnMap, rows [][]string, limit int) []importPreviewRow {
	if limit <= 0 || limit > len(rows) {
		limit = len(rows)
	}
	out := make([]importPreviewRow, 0, limit)
	for i := 0; i < limit && i < len(rows); i++ {
		row := rows[i]
		out = append(out, importPreviewRow{
			Title:   cellValue(row, cols.title),
			Project: cellValue(row, cols.project),
			DueDate: cellValue(row, cols.dueDate),
			Tags:    cellValue(row, cols.tags),
		})
	}
	return out
}

// dryRunImportCounts returns how many rows would import vs skip as duplicates.
func dryRunImportCounts(userID int, cols importColumnMap, rows [][]string) (wouldImport, wouldSkip int, err error) {
	candidateRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		title := cellValue(row, cols.title)
		if title == "" {
			wouldSkip++
			continue
		}
		candidateRows = append(candidateRows, row)
	}
	if len(candidateRows) == 0 {
		return wouldImport, wouldSkip, nil
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		return 0, 0, err
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	for _, row := range candidateRows {
		title := cellValue(row, cols.title)
		dueDate := cellValue(row, cols.dueDate)
		exists, err := importRowIsDuplicate(ctx, pool, userID, title, dueDate)
		if err != nil {
			return wouldImport, wouldSkip, err
		}
		if exists {
			wouldSkip++
		} else {
			wouldImport++
		}
	}
	return wouldImport, wouldSkip, nil
}

func importRowIsDuplicate(ctx context.Context, pool interface {
	QueryRow(context.Context, string, ...interface{}) pgx.Row
}, userID int, title, dueDate string) (bool, error) {
	var exists bool
	dupArgs := []interface{}{userID, title}
	dupQuery := "SELECT EXISTS(SELECT 1 FROM tasks WHERE user_id = $1 AND title = $2 AND deleted_at IS NULL"
	if dueDate != "" {
		dupQuery += " AND due_date = $3::date"
		dupArgs = append(dupArgs, dueDate)
	} else {
		dupQuery += " AND due_date IS NULL"
	}
	dupQuery += ")"
	if err := pool.QueryRow(ctx, dupQuery, dupArgs...).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

// APIImportTasks is kept for tests referencing the legacy name.
func APIImportTasks(w http.ResponseWriter, r *http.Request) {
	apiV1ImportConfirm(w, r)
}

func parseImportCSV(data []byte) (importColumnMap, [][]string, error) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	if !utf8.Valid(data) {
		return importColumnMap{}, nil, fmt.Errorf("file must be UTF-8 encoded")
	}

	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return importColumnMap{}, nil, fmt.Errorf("invalid CSV: %v", err)
	}
	if len(records) < 2 {
		return importColumnMap{}, nil, fmt.Errorf("CSV must include a header row and at least one data row")
	}

	header := normalizeCSVHeader(records[0])
	cols := importColumnMap{
		title:       indexOfHeader(header, "title"),
		description: indexOfHeader(header, "description"),
		completed:   indexOfHeader(header, "completed"),
		dueDate:     indexOfHeader(header, "due_date"),
		project:     indexOfHeader(header, "project"),
		priority:    indexOfHeader(header, "priority"),
		favorite:    indexOfHeader(header, "favorite"),
		tags:        indexOfHeader(header, "tags"),
	}
	if cols.title < 0 {
		return importColumnMap{}, nil, fmt.Errorf("CSV must include a title column")
	}

	rows := make([][]string, 0, len(records)-1)
	for _, row := range records[1:] {
		if isEmptyCSVRow(row) {
			continue
		}
		rows = append(rows, row)
	}
	return cols, rows, nil
}

func normalizeCSVHeader(row []string) []string {
	out := make([]string, len(row))
	for i, col := range row {
		out[i] = strings.ToLower(strings.TrimSpace(col))
	}
	return out
}

func indexOfHeader(header []string, name string) int {
	for i, col := range header {
		if col == name {
			return i
		}
	}
	return -1
}

func isEmptyCSVRow(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func cellValue(row []string, idx int) string {
	if idx < 0 || idx >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[idx])
}

func parseBoolCell(val string) bool {
	switch strings.ToLower(val) {
	case "1", "true", "yes", "y":
		return true
	default:
		return false
	}
}

// errImportProjectRequired stops a company-mode import with a row that names no project.
var errImportProjectRequired = errors.New("every task on this site needs a project; map a project column and fill it in for every row")

// errImportProjectReadOnly stops a company-mode import naming a project the user can
// open but can't add tasks to. Creating a same-named project instead would only confuse.
var errImportProjectReadOnly = errors.New("you can't add tasks to a project the import names")

// findImportProject resolves a project name from an import. On a multi-user site it
// matches the user's own projects. On a company site, where projects are shared rather
// than owned, it matches any project the user can open: one they can add tasks to wins,
// and a match they can only view stops the import.
func findImportProject(ctx context.Context, tx pgx.Tx, userID int, name string, company bool) (int, bool, error) {
	if !company {
		var id int
		err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE user_id = $1 AND LOWER(name) = LOWER($2) AND COALESCE(archived, false) = false ORDER BY id ASC LIMIT 1`, userID, name).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, false, nil
		}
		return id, err == nil, err
	}
	rows, err := tx.Query(ctx, `
		SELECT p.id, pm.role FROM projects p
		JOIN project_members pm ON pm.project_id = p.id AND pm.user_id = $1
		WHERE LOWER(p.name) = LOWER($2) AND COALESCE(p.archived, false) = false
		ORDER BY p.user_id = $1 DESC, pm.implicit_admin, p.id`, userID, name)
	if err != nil {
		return 0, false, err
	}
	type match struct {
		id   int
		role string
	}
	var matches []match
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.id, &m.role); err != nil {
			rows.Close()
			return 0, false, err
		}
		matches = append(matches, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, false, err
	}
	for _, m := range matches {
		if storage.HasProjectPerm(m.id, m.role, storage.PermTasksCreate) {
			return m.id, true, nil
		}
	}
	if len(matches) > 0 {
		return 0, false, fmt.Errorf("%w: %q", errImportProjectReadOnly, name)
	}
	return 0, false, nil
}

// errImportProjectCreateForbidden stops an import that would create a project the user may not create.
var errImportProjectCreateForbidden = errors.New("you don't have permission to create projects; the import names a project you don't have")

func importTasksFromCSV(userID int, cols importColumnMap, rows [][]string) (imported, skipped int, err error) {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return 0, 0, err
	}
	defer storage.CloseDatabase(pool)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	var nextPos int
	if err := tx.QueryRow(ctx, "SELECT COALESCE(MAX(position),0) + 1 FROM tasks WHERE user_id = $1", userID).Scan(&nextPos); err != nil {
		return 0, 0, err
	}

	type importedTask struct {
		id        int
		projectID int
		tagged    bool
	}
	created := make([]importedTask, 0, len(rows))
	newProjects := make([]int, 0)

	projectCache := make(map[string]int)
	canCreateTags := make(map[int]bool)
	maxDescription := domain.MaxDescriptionLength()
	company := storage.IsCompanyMode()

	for _, row := range rows {
		title := cellValue(row, cols.title)
		if title == "" {
			skipped++
			continue
		}

		description := cellValue(row, cols.description)
		if r := []rune(description); len(r) > maxDescription {
			description = string(r[:maxDescription])
		}
		completed := parseBoolCell(cellValue(row, cols.completed))
		dueDate := cellValue(row, cols.dueDate)
		if dueDate == "" {
			dueDate = ""
		}
		priority := 0
		if cols.priority >= 0 {
			if p, err := strconv.Atoi(cellValue(row, cols.priority)); err == nil && p >= 0 && p <= 3 {
				priority = p
			}
		}
		isFavorite := false

		var exists bool
		exists, err = importRowIsDuplicate(ctx, tx, userID, title, dueDate)
		if err != nil {
			return imported, skipped, err
		}
		if exists {
			skipped++
			continue
		}

		var projectID *int
		projectName := cellValue(row, cols.project)
		if projectName == "" && company {
			return imported, skipped, errImportProjectRequired
		}
		if projectName != "" {
			pid, ok := projectCache[projectName]
			if !ok {
				foundID, found, err := findImportProject(ctx, tx, userID, projectName, company)
				if err != nil {
					return imported, skipped, err
				}
				if !found {
					if !storage.UserHasSitePermission(userID, storage.SitePermProjectsCreate) {
						return imported, skipped, fmt.Errorf("%w: %q", errImportProjectCreateForbidden, projectName)
					}
					// In company mode new projects belong to the company, members-only like any other.
					err = tx.QueryRow(ctx, `
						INSERT INTO projects (user_id, name, organization_id, org_import)
						SELECT $1, $2, c.id, CASE WHEN c.id IS NULL THEN NULL ELSE $3 END
						FROM (SELECT (SELECT id FROM organizations WHERE is_company) AS id) c
						RETURNING id`, userID, projectName, storage.OrgImportSelect).Scan(&foundID)
					if err != nil {
						return imported, skipped, err
					}
					newProjects = append(newProjects, foundID)
				}
				pid = foundID
				projectCache[projectName] = pid
			}
			projectID = &pid
		}

		position := nextPos
		nextPos++

		var taskID int
		if projectID != nil && dueDate != "" {
			err = tx.QueryRow(ctx,
				"INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, due_date, is_favorite) VALUES ($1,$2,$3,$4,NOW() AT TIME ZONE 'UTC',$5,$6,$7,$8,$9) RETURNING id",
				title, description, completed, userID, position, priority, *projectID, dueDate, isFavorite).Scan(&taskID)
		} else if projectID != nil {
			err = tx.QueryRow(ctx,
				"INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, project_id, is_favorite) VALUES ($1,$2,$3,$4,NOW() AT TIME ZONE 'UTC',$5,$6,$7,$8) RETURNING id",
				title, description, completed, userID, position, priority, *projectID, isFavorite).Scan(&taskID)
		} else if dueDate != "" {
			err = tx.QueryRow(ctx,
				"INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, due_date, is_favorite) VALUES ($1,$2,$3,$4,NOW() AT TIME ZONE 'UTC',$5,$6,$7,$8) RETURNING id",
				title, description, completed, userID, position, priority, dueDate, isFavorite).Scan(&taskID)
		} else {
			err = tx.QueryRow(ctx,
				"INSERT INTO tasks (title, description, completed, user_id, time_stamp, position, priority, is_favorite) VALUES ($1,$2,$3,$4,NOW() AT TIME ZONE 'UTC',$5,$6,$7) RETURNING id",
				title, description, completed, userID, position, priority, isFavorite).Scan(&taskID)
		}
		if err != nil {
			return imported, skipped, err
		}

		tagCSV := cellValue(row, cols.tags)
		tagged := false
		if tagCSV != "" {
			canCreate, ok := canCreateTags[pidOrZero(projectID)]
			if !ok {
				canCreate = true
				// Projects this import created are the user's own; others need Manage tags.
				if projectID != nil && !slices.Contains(newProjects, *projectID) {
					if canCreate, err = storage.UserCanManageTagNamespace(userID, projectID); err != nil {
						return imported, skipped, err
					}
				}
				canCreateTags[pidOrZero(projectID)] = canCreate
			}
			tagIDs, tagErr := resolveImportTagIDs(ctx, tx, userID, projectID, tagCSV, canCreate)
			if tagErr != nil {
				return imported, skipped, tagErr
			}
			if len(tagIDs) > 0 {
				if err := setTaskTagsInTx(ctx, tx, taskID, tagIDs); err != nil {
					return imported, skipped, err
				}
				tagged = true
			}
		}

		pid := 0
		if projectID != nil {
			pid = *projectID
		}
		created = append(created, importedTask{id: taskID, projectID: pid, tagged: tagged})
		imported++
	}

	if err := tx.Commit(ctx); err != nil {
		return imported, skipped, err
	}
	for _, pid := range newProjects {
		live.AfterProjectChange(userID, pid, live.TypeProjectCreated)
	}
	projectCounts := map[int]int{}
	inboxCount := 0
	for _, row := range created {
		live.AfterTaskChange(userID, row.id, live.TypeTaskCreated)
		if row.tagged {
			live.DispatchHook(userID, row.id, live.TypeTaskTagged, &live.TaskHookMeta{Changed: []string{"tags"}})
		}
		if row.projectID > 0 {
			projectCounts[row.projectID]++
		} else {
			inboxCount++
		}
	}
	for pid, n := range projectCounts {
		live.DispatchProjectHook(userID, pid, live.TypeImportCompleted, &live.TaskHookMeta{Count: n})
	}
	if inboxCount > 0 {
		live.DispatchOwnerHook(userID, userID, live.TypeImportCompleted, &live.TaskHookMeta{Count: inboxCount})
	}
	return imported, skipped, nil
}

// resolveImportTagIDs finds or creates a row's tags inside the import's transaction,
// which can see a project the import just created. canCreate is false where the user
// can add tasks but not manage tags (a project shared with them): existing tags are
// used and new names skipped.
func resolveImportTagIDs(ctx context.Context, tx pgx.Tx, userID int, projectID *int, tagsCSV string, canCreate bool) ([]int, error) {
	parts := strings.Split(tagsCSV, ";")
	if len(parts) == 1 {
		parts = strings.Split(tagsCSV, ",")
	}
	var ids []int
	seen := make(map[int]bool)
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" || storage.IsSystemTagName(name) {
			continue
		}
		t, err := storage.FindOrCreateTagInTx(ctx, tx, userID, projectID, name, canCreate)
		if err != nil {
			return nil, err
		}
		if t == nil {
			continue
		}
		if !seen[t.ID] {
			seen[t.ID] = true
			ids = append(ids, t.ID)
		}
		if len(ids) >= storage.MaxTagsPerTask {
			break
		}
	}
	return ids, nil
}

// setTaskTagsInTx replaces a task's tags. The ids come from resolveImportTagIDs, which
// only returns tags in the task's own namespace.
func setTaskTagsInTx(ctx context.Context, tx pgx.Tx, taskID int, tagIDs []int) error {
	if _, err := tx.Exec(ctx, "DELETE FROM task_tags WHERE task_id = $1", taskID); err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		if _, err := tx.Exec(ctx, "INSERT INTO task_tags (task_id, tag_id) VALUES ($1, $2)", taskID, tagID); err != nil {
			return err
		}
	}
	return nil
}

func pidOrZero(projectID *int) int {
	if projectID == nil {
		return 0
	}
	return *projectID
}
