package tasks

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"

	"GoTodo/internal/query"
	"GoTodo/internal/storage"

	"github.com/jackc/pgx/v5"
)

// Query result limits.
const (
	QueryDefaultPageSize = 50
	QueryMaxPageSize     = 200
	// QueryExportLimit caps rows in one export.
	QueryExportLimit = 5000
	// queryMaxTreeDepth bounds how many subtask levels a tree result nests.
	queryMaxTreeDepth = 5
	// queryStatementTimeout keeps one heavy query from tying up the database.
	queryStatementTimeout = "8s"
)

// QueryResult is one page of a query run. In tree mode Tasks are the top-level
// rows (matches whose parent does not match) with matching subtasks nested in
// Children, and Total counts top-level rows.
type QueryResult struct {
	Tasks []Task
	Total int
}

// RunQuery runs a prepared definition (see query.Loader.Prepare) for one page.
// pageSize <= 0 runs a count only.
func RunQuery(d query.Definition, ctx query.Context, page, pageSize int) (QueryResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize > QueryMaxPageSize {
		pageSize = QueryMaxPageSize
	}
	return runQuery(d, ctx, page, pageSize)
}

// RunQueryExport returns up to QueryExportLimit matching tasks as a flat list.
func RunQueryExport(d query.Definition, ctx query.Context) ([]Task, error) {
	d.Mode = query.ModeFlat
	res, err := runQuery(d, ctx, 1, QueryExportLimit)
	return res.Tasks, err
}

func runQuery(d query.Definition, ctx query.Context, page, pageSize int) (QueryResult, error) {
	out := QueryResult{Tasks: []Task{}}
	pool, err := storage.OpenDatabase()
	if err != nil {
		return out, err
	}
	defer storage.CloseDatabase(pool)

	bg := context.Background()
	tx, err := pool.BeginTx(bg, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(bg)
	if _, err := tx.Exec(bg, "SET LOCAL statement_timeout = '"+queryStatementTimeout+"'"); err != nil {
		return out, err
	}

	c := query.Compile(d, ctx, nil)
	where := c.Where
	if d.Mode == query.ModeTree {
		// Top-level rows: matches whose parent is not itself a match.
		where = "(" + c.Where + ") AND (t.parent_id IS NULL OR t.parent_id NOT IN (SELECT t.id FROM tasks t WHERE " + c.Where + "))"
	}

	if err := tx.QueryRow(bg, "SELECT COUNT(*) FROM tasks t WHERE "+where, c.Args...).Scan(&out.Total); err != nil {
		return out, fmt.Errorf("query count: %w", err)
	}
	if pageSize <= 0 || out.Total == 0 {
		return out, tx.Commit(bg)
	}

	args := append([]any(nil), c.Args...)
	args = append(args, pageSize, (page-1)*pageSize)
	limit := " LIMIT $" + strconv.Itoa(len(args)-1) + " OFFSET $" + strconv.Itoa(len(args))
	rows, err := queryTaskRows(bg, tx, where, c.OrderBy, limit, args, ctx.Timezone)
	if err != nil {
		return out, err
	}
	out.Tasks = rows

	if d.Mode == query.ModeTree {
		level := make([]*Task, 0, len(out.Tasks))
		for i := range out.Tasks {
			level = append(level, &out.Tasks[i])
		}
		for depth := 0; depth < queryMaxTreeDepth && len(level) > 0; depth++ {
			ids := make([]int, len(level))
			byID := make(map[int]*Task, len(level))
			for i, t := range level {
				ids[i] = t.ID
				byID[t.ID] = t
			}
			cargs := append([]any(nil), c.Args...)
			cargs = append(cargs, ids)
			children, err := queryTaskRows(bg, tx,
				"("+c.Where+") AND t.parent_id = ANY($"+strconv.Itoa(len(cargs))+")",
				c.OrderBy, "", cargs, ctx.Timezone)
			if err != nil {
				return out, err
			}
			for _, child := range children {
				if parent, ok := byID[child.ParentID]; ok {
					child.ParentTitle = parent.Title
					parent.Children = append(parent.Children, child)
				}
			}
			next := make([]*Task, 0)
			for _, t := range level {
				for j := range t.Children {
					next = append(next, &t.Children[j])
				}
			}
			level = next
		}
	}
	if err := tx.Commit(bg); err != nil {
		return out, err
	}
	if err := decorateQueryTasks(out.Tasks); err != nil {
		return out, err
	}
	return out, nil
}

func queryTaskRows(ctx context.Context, tx pgx.Tx, where, orderBy, limit string, args []any, timezone string) ([]Task, error) {
	if timezone == "" {
		timezone = "UTC"
	}
	args = append(args, timezone)
	tz := "$" + strconv.Itoa(len(args))
	sqlText := `SELECT t.id, t.title, COALESCE(t.description, ''), COALESCE(t.completed, false),
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE ` + tz + `, 'YYYY/MM/DD HH:MI AM'),
		COALESCE(CAST(t.due_date AS TEXT), ''),
		TO_CHAR((t.time_stamp AT TIME ZONE 'UTC') AT TIME ZONE ` + tz + `, 'YYYY/MM/DD HH:MI AM'),
		COALESCE(TO_CHAR((t.date_modified AT TIME ZONE 'UTC') AT TIME ZONE ` + tz + `, 'YYYY/MM/DD HH:MI AM'), ''),
		COALESCE(t.is_favorite, false), COALESCE(t.position, 0), COALESCE(t.priority, 0), t.project_id, COALESCE(p.name, ''), t.parent_id,
		COALESCE(t.user_id, 0), COALESCE(NULLIF(qcr.full_name, ''), NULLIF(qcr.user_name, ''), qcr.email, ''),
		CASE WHEN COALESCE(t.completed, false) THEN TO_CHAR(((COALESCE(
			(SELECT MAX(qte.created_at) FROM task_events qte WHERE qte.task_id = t.id AND qte.event_type = 'completed'),
			t.date_modified, t.time_stamp) AT TIME ZONE 'UTC') AT TIME ZONE ` + tz + `), 'YYYY-MM-DD') ELSE '' END,
		COALESCE(TO_CHAR(((` + query.StatusEnteredSQL + ` AT TIME ZONE 'UTC') AT TIME ZONE ` + tz + `), 'YYYY-MM-DD'), ''),
		((NOW() AT TIME ZONE ` + tz + `)::date - ((` + query.StatusEnteredSQL + ` AT TIME ZONE 'UTC') AT TIME ZONE ` + tz + `)::date)
		FROM tasks t` + query.SelectJoins + ` WHERE ` + where + ` ORDER BY ` + orderBy + limit
	rows, err := tx.Query(ctx, sqlText, args...)
	if err != nil {
		return nil, fmt.Errorf("query rows: %w", err)
	}
	defer rows.Close()
	out := make([]Task, 0)
	for rows.Next() {
		var t Task
		var pid, parentID, days sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Title, &t.Description, &t.Completed,
			&t.DateAdded, &t.DueDate, &t.DateCreated, &t.DateModified,
			&t.IsFavorite, &t.Position, &t.Priority, &pid, &t.ProjectName, &parentID,
			&t.CreatedByID, &t.CreatedByName, &t.CompletedDate, &t.StatusChangedDate, &days); err != nil {
			return nil, err
		}
		if days.Valid {
			d := int(days.Int64)
			t.DaysInStatus = &d
		}
		if pid.Valid {
			t.ProjectID = int(pid.Int64)
		}
		if parentID.Valid {
			t.ParentID = int(parentID.Int64)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// decorateQueryTasks loads tags, workflow fields, custom fields and the rest
// for every row at every level, then subtask counts and parent titles.
func decorateQueryTasks(list []Task) error {
	if len(list) == 0 {
		return nil
	}
	if err := decorateQueryLevel(list); err != nil {
		return err
	}
	if err := attachChildCounts(list); err != nil {
		return err
	}
	if err := attachBacklogNames(list); err != nil {
		return err
	}
	missing := make([]Task, 0)
	for _, t := range list {
		if t.ParentID > 0 && t.ParentTitle == "" {
			missing = append(missing, Task{ID: t.ID, ParentID: t.ParentID})
		}
	}
	if len(missing) == 0 {
		return nil
	}
	if err := attachParentTitles(missing); err != nil {
		return err
	}
	titles := make(map[int]string, len(missing))
	for _, t := range missing {
		titles[t.ID] = t.ParentTitle
	}
	for i := range list {
		if title, ok := titles[list[i].ID]; ok {
			list[i].ParentTitle = title
		}
	}
	return nil
}

// decorateQueryLevel decorates children first, then this level. The list
// helpers also touch one level of children; doing that twice is harmless.
func decorateQueryLevel(list []Task) error {
	for i := range list {
		if len(list[i].Children) > 0 {
			if err := decorateQueryLevel(list[i].Children); err != nil {
				return err
			}
		}
	}
	return attachTagsToTasks(list)
}

// attachBacklogNames labels kanban tasks with no sprint by their project's
// backlog name, so the sprint column reads "Backlog" (or its rename) instead
// of blank. Only query results get this; boards key off an empty SprintName.
func attachBacklogNames(list []Task) error {
	needs := func(t *Task) bool {
		return t.SprintID == 0 && t.ProjectID > 0 && t.ProjectWorkflow == "kanban"
	}
	seen := map[int]bool{}
	ids := make([]int, 0)
	var collect func(ts []Task)
	collect = func(ts []Task) {
		for i := range ts {
			if needs(&ts[i]) && !seen[ts[i].ProjectID] {
				seen[ts[i].ProjectID] = true
				ids = append(ids, ts[i].ProjectID)
			}
			collect(ts[i].Children)
		}
	}
	collect(list)
	if len(ids) == 0 {
		return nil
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)
	rows, err := pool.Query(context.Background(),
		`SELECT id, COALESCE(NULLIF(backlog_name, ''), 'Backlog') FROM projects WHERE id = ANY($1)`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	names := make(map[int]string, len(ids))
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return err
		}
		names[id] = name
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var apply func(ts []Task)
	apply = func(ts []Task) {
		for i := range ts {
			if needs(&ts[i]) {
				ts[i].SprintName = names[ts[i].ProjectID]
			}
			apply(ts[i].Children)
		}
	}
	apply(list)
	return nil
}

// attachChildCounts fills ChildCount and ChildrenCompleted from every live
// subtask, matching or not, so progress reads the same as on the task list.
func attachChildCounts(list []Task) error {
	ids := make([]int, 0, len(list))
	var collect func(ts []Task)
	collect = func(ts []Task) {
		for _, t := range ts {
			ids = append(ids, t.ID)
			collect(t.Children)
		}
	}
	collect(list)
	pool, err := storage.OpenDatabase()
	if err != nil {
		return err
	}
	defer storage.CloseDatabase(pool)
	rows, err := pool.Query(context.Background(), `
		SELECT parent_id, COUNT(*), COUNT(*) FILTER (WHERE COALESCE(completed, false))
		FROM tasks WHERE parent_id = ANY($1) AND deleted_at IS NULL
		  AND NOT `+storage.ArchivedTaskExistsSQL("tasks.id")+`
		GROUP BY parent_id`, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	type counts struct{ all, done int }
	byID := map[int]counts{}
	for rows.Next() {
		var id int
		var c counts
		if err := rows.Scan(&id, &c.all, &c.done); err != nil {
			return err
		}
		byID[id] = c
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var apply func(ts []Task)
	apply = func(ts []Task) {
		for i := range ts {
			c := byID[ts[i].ID]
			ts[i].ChildCount = c.all
			ts[i].ChildrenCompleted = c.done
			apply(ts[i].Children)
		}
	}
	apply(list)
	return nil
}
