package handlers

import (
	"context"
	"errors"

	"GoTodo/internal/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrReorderValidation is returned when task IDs fail ownership/favorite/project checks.
var ErrReorderValidation = domain.ErrValidation

// applyRelativeReorder delegates to domain.ApplyRelativeReorder (kept for existing tests).
func applyRelativeReorder(allIDs []int, orderedIDs []int) ([]int, error) {
	return domain.ApplyRelativeReorder(allIDs, orderedIDs)
}

// reorderTaskPositions validates IDs and renumbers position within a favorite/project group.
// page and pageSize are retained for call-site compatibility but unused.
func reorderTaskPositions(
	ctx context.Context,
	db *pgxpool.Pool,
	userID int,
	ids []int,
	isFav bool,
	page, pageSize int,
	projectFilter *int,
) error {
	_ = db
	_ = page
	_ = pageSize
	err := domain.ReorderTasks(ctx, userID, ids, isFav, projectFilter)
	if err != nil && errors.Is(err, domain.ErrValidation) {
		return err
	}
	return err
}
