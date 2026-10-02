package storage

import (
	"sync"
	"time"
)

// Task description and comment caps are counted in characters (runes) and
// configured per site in site_settings. The underlying columns are TEXT, so
// the hard ceiling only guards against abusive payloads.
const (
	DefaultTaskTextLength = 20000
	MinTaskTextLength     = 500
	MaxTaskTextLengthCap  = 100000

	taskTextLimitsTTL = 30 * time.Second
)

// TaskTextLimits holds the active character limits for task text.
type TaskTextLimits struct {
	Description int
	Comment     int
}

var (
	taskTextLimitsMu      sync.RWMutex
	taskTextLimitsCached  *TaskTextLimits
	taskTextLimitsExpires time.Time
)

// ClampTaskTextLength returns a safe limit. Unset values (<= 0) use the
// default; others are bounded to [MinTaskTextLength, MaxTaskTextLengthCap].
func ClampTaskTextLength(n int) int {
	if n <= 0 {
		return DefaultTaskTextLength
	}
	if n < MinTaskTextLength {
		return MinTaskTextLength
	}
	if n > MaxTaskTextLengthCap {
		return MaxTaskTextLengthCap
	}
	return n
}

// DefaultTaskTextLimits returns the limits used before an admin configures them.
func DefaultTaskTextLimits() TaskTextLimits {
	return TaskTextLimits{Description: DefaultTaskTextLength, Comment: DefaultTaskTextLength}
}

// GetTaskTextLimits returns the configured limits, cached briefly so request
// validation does not hit site_settings every time. Falls back to defaults
// when settings cannot be loaded.
func GetTaskTextLimits() TaskTextLimits {
	taskTextLimitsMu.RLock()
	if taskTextLimitsCached != nil && time.Now().Before(taskTextLimitsExpires) {
		l := *taskTextLimitsCached
		taskTextLimitsMu.RUnlock()
		return l
	}
	taskTextLimitsMu.RUnlock()

	s, err := GetSiteSettings() // refreshes the cache on success
	if err != nil || s == nil {
		return DefaultTaskTextLimits()
	}
	return s.TaskTextLimits()
}

// TaskTextLimits returns the clamped limits stored in these settings.
func (s *SiteSettings) TaskTextLimits() TaskTextLimits {
	if s == nil {
		return DefaultTaskTextLimits()
	}
	return TaskTextLimits{
		Description: ClampTaskTextLength(s.MaxDescriptionLength),
		Comment:     ClampTaskTextLength(s.MaxCommentLength),
	}
}

func cacheTaskTextLimits(l TaskTextLimits) {
	taskTextLimitsMu.Lock()
	taskTextLimitsCached = &l
	taskTextLimitsExpires = time.Now().Add(taskTextLimitsTTL)
	taskTextLimitsMu.Unlock()
}
