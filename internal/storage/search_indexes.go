package storage

import (
	"context"
	"fmt"
	"log"
)

// EnsureSearchIndexes adds trigram GIN indexes for task search (titles,
// descriptions, tag names, comments), so `ILIKE '%term%'` uses an index instead
// of scanning every row. They need the pg_trgm extension; when the database role may not
// create it, search keeps working without the indexes and this logs why.
func EnsureSearchIndexes() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	var available bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'pg_trgm')`).Scan(&available); err != nil {
		return err
	}
	if !available {
		if _, err := pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm`); err != nil {
			log.Printf("search: pg_trgm is not installed and could not be created (%v); task search will not use trigram indexes. Run CREATE EXTENSION pg_trgm as a superuser to enable them.", err)
			return nil
		}
	}

	stmts := []string{
		`CREATE INDEX IF NOT EXISTS idx_tasks_title_trgm ON tasks USING gin (title gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_tasks_description_trgm ON tasks USING gin (description gin_trgm_ops)`,
		`CREATE INDEX IF NOT EXISTS idx_tags_name_trgm ON tags USING gin (name gin_trgm_ops)`,
	}
	// task_comments is missing only in trimmed-down test schemas.
	var hasComments bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('task_comments') IS NOT NULL`).Scan(&hasComments); err != nil {
		return err
	}
	if hasComments {
		stmts = append(stmts, `CREATE INDEX IF NOT EXISTS idx_task_comments_body_trgm ON task_comments USING gin (body gin_trgm_ops)`)
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s); err != nil {
			return fmt.Errorf("create search index: %w", err)
		}
	}
	return nil
}
