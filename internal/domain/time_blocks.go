package domain

import (
	"context"
	"errors"
	"fmt"
	"time"

	"GoTodo/internal/storage"
)

// MaxTimeBlockRangeDays caps the calendar range one block listing covers.
const MaxTimeBlockRangeDays = 42

// TimeBlockSpan is where a block sits on the calendar.
type TimeBlockSpan struct {
	StartAt time.Time
	EndAt   time.Time
}

func (s TimeBlockSpan) validate() error {
	step := time.Duration(storage.TimeBlockStepMinutes) * time.Minute
	if s.StartAt.IsZero() || s.EndAt.IsZero() {
		return fmt.Errorf("%w: start_at and end_at are required", ErrValidation)
	}
	if !s.EndAt.After(s.StartAt) {
		return fmt.Errorf("%w: end_at must be after start_at", ErrValidation)
	}
	if s.EndAt.Sub(s.StartAt) > time.Duration(storage.MaxTimeBlockMinutes)*time.Minute {
		return fmt.Errorf("%w: a time block can be at most %d hours", ErrValidation, storage.MaxTimeBlockMinutes/60)
	}
	if s.StartAt.Truncate(step) != s.StartAt || s.EndAt.Truncate(step) != s.EndAt {
		return fmt.Errorf("%w: time blocks snap to %d minutes", ErrValidation, storage.TimeBlockStepMinutes)
	}
	return nil
}

// canReadTask reports whether the user can still open the task.
func canReadTask(userID, taskID int) (bool, error) {
	canRead, _, _, err := storage.CanUserAccessTask(taskID, userID)
	return canRead, err
}

// filterReadableBlocks drops blocks on tasks the user can no longer open (lost
// project access, for example). The blocks stay and come back with access.
func filterReadableBlocks(userID int, blocks []storage.TimeBlock) ([]storage.TimeBlock, error) {
	readable := map[int]bool{}
	out := blocks[:0]
	for _, b := range blocks {
		ok, seen := readable[b.TaskID]
		if !seen {
			var err error
			if ok, err = canReadTask(userID, b.TaskID); err != nil {
				return nil, err
			}
			readable[b.TaskID] = ok
		}
		if ok {
			out = append(out, b)
		}
	}
	return out, nil
}

// ListTimeBlocksForUser returns the user's own blocks overlapping [from, to).
func ListTimeBlocksForUser(ctx context.Context, userID int, from, to time.Time) ([]storage.TimeBlock, error) {
	_ = ctx
	if from.IsZero() || to.IsZero() || !to.After(from) {
		return nil, fmt.Errorf("%w: from must be before to", ErrValidation)
	}
	if to.Sub(from) > MaxTimeBlockRangeDays*24*time.Hour {
		return nil, fmt.Errorf("%w: choose a range of at most %d days", ErrValidation, MaxTimeBlockRangeDays)
	}
	blocks, err := storage.ListUserTimeBlocks(userID, from, to)
	if err != nil {
		return nil, err
	}
	return filterReadableBlocks(userID, blocks)
}

// ListTaskTimeBlocksForUser returns the user's own blocks on a task they can read.
func ListTaskTimeBlocksForUser(ctx context.Context, userID, taskID int) ([]storage.TimeBlock, error) {
	_ = ctx
	ok, err := canReadTask(userID, taskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return storage.ListUserTaskTimeBlocks(userID, taskID)
}

// CreateTimeBlockForUser sets aside time on the user's calendar for a task
// they can read. Blocks are private to the user.
func CreateTimeBlockForUser(ctx context.Context, userID, taskID int, span TimeBlockSpan) (*storage.TimeBlock, error) {
	_ = ctx
	ok, err := canReadTask(userID, taskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	if err := span.validate(); err != nil {
		return nil, err
	}
	return storage.CreateTimeBlock(userID, taskID, span.StartAt, span.EndAt)
}

// ownTimeBlock loads a block the user owns on a task they can still read.
func ownTimeBlock(userID, blockID int) (*storage.TimeBlock, error) {
	b, err := storage.GetTimeBlock(blockID)
	if errors.Is(err, storage.ErrTimeBlockNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if b.UserID != userID {
		return nil, ErrNotFound
	}
	ok, err := canReadTask(userID, b.TaskID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	return b, nil
}

// MoveTimeBlockForUser moves or resizes one of the user's blocks. Logged
// blocks are fixed: their time entry already records the span.
func MoveTimeBlockForUser(ctx context.Context, userID, blockID int, span TimeBlockSpan) (*storage.TimeBlock, error) {
	_ = ctx
	if _, err := ownTimeBlock(userID, blockID); err != nil {
		return nil, err
	}
	if err := span.validate(); err != nil {
		return nil, err
	}
	b, err := storage.MoveTimeBlock(blockID, userID, span.StartAt, span.EndAt)
	switch {
	case errors.Is(err, storage.ErrTimeBlockNotFound):
		return nil, ErrNotFound
	case errors.Is(err, storage.ErrTimeBlockLogged):
		return nil, fmt.Errorf("%w: this block's time is logged; delete the time entry to move it", ErrConflict)
	}
	return b, err
}

// DeleteTimeBlockForUser removes one of the user's blocks. Time already logged
// from it stays on the task.
func DeleteTimeBlockForUser(ctx context.Context, userID, blockID int) error {
	_ = ctx
	if _, err := ownTimeBlock(userID, blockID); err != nil {
		return err
	}
	if err := storage.DeleteTimeBlock(blockID, userID); err != nil {
		if errors.Is(err, storage.ErrTimeBlockNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

// LogTimeBlockForUser records a block that has started as time spent on its
// task: one time entry for the block's length, dated when the block began.
func LogTimeBlockForUser(ctx context.Context, userID, blockID int, note string) (*storage.TimeBlock, *storage.TaskTimeEntry, error) {
	b, err := ownTimeBlock(userID, blockID)
	if err != nil {
		return nil, nil, err
	}
	if b.TimeEntryID != 0 {
		return nil, nil, fmt.Errorf("%w: this block's time is already logged", ErrConflict)
	}
	if b.StartAt.After(time.Now()) {
		return nil, nil, fmt.Errorf("%w: a block can be logged once it has started", ErrValidation)
	}
	start := b.StartAt
	entry, err := AddTimeEntryAtForUser(ctx, userID, b.TaskID, b.Minutes(), note, &start)
	if err != nil {
		return nil, nil, err
	}
	if err := storage.SetTimeBlockEntry(blockID, entry.ID); err != nil {
		// Logged twice at once: keep the first entry only.
		_ = storage.DeleteTaskTimeEntry(entry.ID)
		if errors.Is(err, storage.ErrTimeBlockLogged) {
			return nil, nil, fmt.Errorf("%w: this block's time is already logged", ErrConflict)
		}
		return nil, nil, err
	}
	b.TimeEntryID = entry.ID
	return b, entry, nil
}
