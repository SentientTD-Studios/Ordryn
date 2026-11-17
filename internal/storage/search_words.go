package storage

import (
	"context"
	"fmt"
)

// Search tolerates typos by looking words up in a dictionary of every word that
// has appeared in a task title. Each word is stored with its
// "variants": itself and each way of dropping one letter. Two words are one
// edit apart (an inserted, missing, wrong or swapped letter) only if their
// variant sets overlap, so a search term finds its close spellings with a
// btree lookup of its own variants (tasks.CloseSpellings). The cost depends on
// the size of the vocabulary, not on how many tasks there are.
//
// Words are only ever added; a word whose tasks are gone just expands a search
// to a pattern that matches nothing. Search results still go through the
// caller's visibility rules, so the dictionary never reveals a task.

const searchWordsSchema = `
CREATE TABLE IF NOT EXISTS search_words (
	word TEXT PRIMARY KEY
);
CREATE TABLE IF NOT EXISTS search_word_variants (
	variant TEXT NOT NULL,
	word TEXT NOT NULL REFERENCES search_words(word) ON DELETE CASCADE,
	PRIMARY KEY (variant, word)
)`

// gotodo_index_search_words adds the words of a text (3 to 32 letters or
// digits, lowercased) to the dictionary. Words are taken in sorted order so
// concurrent writers lock them in the same order.
const indexSearchWordsFunc = `
CREATE OR REPLACE FUNCTION gotodo_index_search_words(p_text TEXT) RETURNS void AS $$
DECLARE
	w TEXT;
	i INT;
BEGIN
	FOR w IN
		SELECT DISTINCT x FROM regexp_split_to_table(lower(COALESCE(p_text, '')), '[^[:alnum:]]+') AS x
		 WHERE char_length(x) BETWEEN 3 AND 32
		 ORDER BY x
	LOOP
		INSERT INTO search_words (word) VALUES (w) ON CONFLICT DO NOTHING;
		IF FOUND THEN
			INSERT INTO search_word_variants (variant, word)
			SELECT DISTINCT v, w FROM (
				SELECT w AS v
				UNION ALL
				SELECT substr(w, 1, n - 1) || substr(w, n + 1) FROM generate_series(1, char_length(w)) AS n
			) vs
			ON CONFLICT DO NOTHING;
		END IF;
	END LOOP;
END;
$$ LANGUAGE plpgsql`

const taskSearchWordsFunc = `
CREATE OR REPLACE FUNCTION gotodo_task_search_words() RETURNS trigger AS $$
BEGIN
	IF TG_OP = 'INSERT' OR NEW.title IS DISTINCT FROM OLD.title THEN
		PERFORM gotodo_index_search_words(NEW.title);
	END IF;
	RETURN NULL;
END;
$$ LANGUAGE plpgsql`

// MigrateSearchWords creates the typo dictionary and the triggers that keep it
// current. The first run fills it from existing titles. Safe to
// run on every start.
func MigrateSearchWords() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)
	ctx := context.Background()

	for _, stmt := range []string{
		searchWordsSchema, indexSearchWordsFunc, taskSearchWordsFunc,
		// Tag names used to be indexed too; search only looks at titles now.
		`DROP TRIGGER IF EXISTS gotodo_tag_search_words ON tags`,
		`DROP FUNCTION IF EXISTS gotodo_tag_search_words()`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("search words: %w", err)
		}
	}

	triggers := []struct{ name, table, column, fn string }{
		{"gotodo_task_search_words", "tasks", "title", "gotodo_task_search_words"},
	}
	// Install the triggers and backfill in one transaction, so no row written
	// meanwhile is missed.
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	backfilled := false
	for _, tr := range triggers {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = $1 AND tgrelid = $2::regclass)`,
			tr.name, tr.table).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		stmts := []string{
			fmt.Sprintf(`CREATE TRIGGER %s AFTER INSERT OR UPDATE OF %s ON %s FOR EACH ROW EXECUTE FUNCTION %s()`,
				tr.name, tr.column, tr.table, tr.fn),
			fmt.Sprintf(`INSERT INTO search_words (word)
				SELECT DISTINCT x FROM %s, regexp_split_to_table(lower(COALESCE(%s, '')), '[^[:alnum:]]+') AS x
				 WHERE char_length(x) BETWEEN 3 AND 32
				ON CONFLICT DO NOTHING`, tr.table, tr.column),
		}
		for _, stmt := range stmts {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("search words %s: %w", tr.table, err)
			}
		}
		backfilled = true
	}
	if backfilled {
		if _, err := tx.Exec(ctx, `
			INSERT INTO search_word_variants (variant, word)
			SELECT DISTINCT v, word FROM (
				SELECT word AS v, word FROM search_words
				UNION ALL
				SELECT substr(word, 1, n - 1) || substr(word, n + 1), word
				  FROM search_words, generate_series(1, char_length(word)) AS n
			) vs
			ON CONFLICT DO NOTHING`); err != nil {
			return fmt.Errorf("search word variants: %w", err)
		}
	}
	return tx.Commit(ctx)
}
