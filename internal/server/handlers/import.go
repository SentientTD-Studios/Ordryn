package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/sessionstore"
	"GoTodo/internal/storage"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"net/http"
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

// ImportPageHandler renders the CSV import page.
func ImportPageHandler(w http.ResponseWriter, r *http.Request) {
	email, _, permissions, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		http.Redirect(w, r, utils.GetBasePath()+"/", http.StatusSeeOther)
		return
	}

	ctx := map[string]interface{}{
		"LoggedIn":    true,
		"UserEmail":   email,
		"Permissions": permissions,
		"Title":       "Import Tasks",
	}
	utils.RenderTemplate(w, r, "import.html", ctx)
}

// APIImportTasks handles CSV file upload and imports tasks for the user.
func APIImportTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	email, _, _, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if isBanned, err := storage.IsUserBanned(email); err == nil && isBanned {
		sessionstore.ClearSessionCookie(w, r)
		http.Redirect(w, r, utils.GetBasePath()+"/", http.StatusSeeOther)
		return
	}

	userID := utils.GetSessionUserID(r)
	if userID == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	if err := r.ParseMultipartForm(importMaxFileBytes); err != nil {
		http.Error(w, "File too large (max 5MB)", http.StatusBadRequest)
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "Missing file upload", http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, importMaxFileBytes+1))
	if err != nil {
		http.Error(w, "Failed to read file", http.StatusBadRequest)
		return
	}
	if len(data) > importMaxFileBytes {
		http.Error(w, "File too large (max 5MB)", http.StatusBadRequest)
		return
	}

	cols, rows, err := parseImportCSV(data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(rows) == 0 {
		http.Error(w, "No data rows found in CSV", http.StatusBadRequest)
		return
	}
	if len(rows) > importMaxRows {
		http.Error(w, fmt.Sprintf("Too many rows (max %d)", importMaxRows), http.StatusBadRequest)
		return
	}

	imported, skipped, importErr := importTasksFromCSV(*userID, cols, rows)
	if importErr != nil {
		http.Error(w, importErr.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("HX-Trigger", "import-complete")
	fmt.Fprintf(w, `<div class="alert alert-success" role="alert"><i class="bi bi-check-circle"></i> Imported %d tasks, %d skipped (duplicate title + due date).</div>`, imported, skipped)
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
	if err := tx.QueryRow(ctx, "SELECT COALESCE(MAX(position),0) + 1 FROM tasks WHERE user_id = $1 AND (is_favorite IS NULL OR is_favorite = false)", userID).Scan(&nextPos); err != nil {
		return 0, 0, err
	}

	projectCache := make(map[string]int)

	for _, row := range rows {
		title := cellValue(row, cols.title)
		if title == "" {
			skipped++
			continue
		}

		description := cellValue(row, cols.description)
		if len(description) > MaxDescriptionLength {
			description = description[:MaxDescriptionLength]
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
		isFavorite := parseBoolCell(cellValue(row, cols.favorite))

		var exists bool
		dupArgs := []interface{}{userID, title}
		dupQuery := "SELECT EXISTS(SELECT 1 FROM tasks WHERE user_id = $1 AND title = $2"
		if dueDate != "" {
			dupQuery += " AND due_date = $3::date"
			dupArgs = append(dupArgs, dueDate)
		} else {
			dupQuery += " AND due_date IS NULL"
		}
		dupQuery += ")"
		if err := tx.QueryRow(ctx, dupQuery, dupArgs...).Scan(&exists); err != nil {
			return imported, skipped, err
		}
		if exists {
			skipped++
			continue
		}

		var projectID *int
		projectName := cellValue(row, cols.project)
		if projectName != "" {
			pid, ok := projectCache[projectName]
			if !ok {
				var foundID int
				err := tx.QueryRow(ctx, "SELECT id FROM projects WHERE user_id = $1 AND LOWER(name) = LOWER($2)", userID, projectName).Scan(&foundID)
				if err != nil {
					err = tx.QueryRow(ctx, "INSERT INTO projects (user_id, name) VALUES ($1, $2) RETURNING id", userID, projectName).Scan(&foundID)
					if err != nil {
						return imported, skipped, err
					}
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
		if tagCSV != "" {
			tagIDs, tagErr := resolveImportTagIDs(userID, tagCSV)
			if tagErr != nil {
				return imported, skipped, tagErr
			}
			if len(tagIDs) > 0 {
				if err := setTaskTagsInTx(ctx, tx, taskID, userID, tagIDs); err != nil {
					return imported, skipped, err
				}
			}
		}

		imported++
	}

	if err := tx.Commit(ctx); err != nil {
		return imported, skipped, err
	}
	return imported, skipped, nil
}

func resolveImportTagIDs(userID int, tagsCSV string) ([]int, error) {
	parts := strings.Split(tagsCSV, ";")
	if len(parts) == 1 {
		parts = strings.Split(tagsCSV, ",")
	}
	var ids []int
	seen := make(map[int]bool)
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		t, err := storage.GetOrCreateTagByName(userID, name)
		if err != nil {
			return nil, err
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

func setTaskTagsInTx(ctx context.Context, tx pgx.Tx, taskID, userID int, tagIDs []int) error {
	if _, err := tx.Exec(ctx, "DELETE FROM task_tags WHERE task_id = $1", taskID); err != nil {
		return err
	}
	for _, tagID := range tagIDs {
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tags WHERE id = $1 AND user_id = $2)", tagID, userID).Scan(&exists); err != nil || !exists {
			return fmt.Errorf("invalid tag")
		}
		if _, err := tx.Exec(ctx, "INSERT INTO task_tags (task_id, tag_id) VALUES ($1, $2)", taskID, tagID); err != nil {
			return err
		}
	}
	return nil
}
