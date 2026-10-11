package domain

import (
	"log"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

const (
	// reminderHour is the local hour reminders go out (day before and morning of).
	reminderHour = 8
	// reminderTaskLimit bounds the tasks one pass reads.
	reminderTaskLimit = 5000
)

// StartDueReminderWorker checks for due-date reminders every 15 minutes, so
// each user's 8:00 local reminder lands within a quarter hour of 8:00.
func StartDueReminderWorker() {
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			RunDueReminderPass(time.Now())
			<-ticker.C
		}
	}()
}

// reminderRecipients is who hears about a task: the owner of its project and
// whoever claimed it (so an unclaimed board task still reaches someone), or the
// task's own owner when it has no project.
func reminderRecipients(t storage.ReminderTask) []int {
	var ids []int
	add := func(id int) {
		if id <= 0 {
			return
		}
		for _, existing := range ids {
			if existing == id {
				return
			}
		}
		ids = append(ids, id)
	}
	if t.ProjectID > 0 {
		add(t.ProjectOwnerID)
		add(t.ClaimedBy)
	} else {
		add(t.OwnerID)
	}
	return ids
}

// reminderKindAt returns which reminder (if any) is due for a task at local
// time, given the user's timing preference.
func reminderKindAt(due, local time.Time, timing string) string {
	if local.Hour() < reminderHour {
		return ""
	}
	today := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
	dueDay := time.Date(due.Year(), due.Month(), due.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case dueDay.Equal(today.AddDate(0, 0, 1)) &&
		(timing == storage.ReminderTimingDayBefore || timing == storage.ReminderTimingBoth):
		return storage.ReminderKindDayBefore
	case dueDay.Equal(today) &&
		(timing == storage.ReminderTimingMorning || timing == storage.ReminderTimingBoth):
		return storage.ReminderKindMorning
	}
	return ""
}

// RunDueReminderPass creates the reminder notifications due at now and returns
// how many it created. Each reminder is recorded once per task, user, due date,
// and kind, so overlapping passes or servers never repeat one.
func RunDueReminderPass(now time.Time) int {
	utc := now.UTC()
	// Local "today" is within a day of UTC, so this window covers every
	// timezone's today and tomorrow.
	tasks, err := storage.ListReminderTasks(utc.AddDate(0, 0, -1), utc.AddDate(0, 0, 2), reminderTaskLimit)
	if err != nil {
		log.Printf("reminders: list tasks: %v", err)
		return 0
	}
	if len(tasks) == 0 {
		return 0
	}
	seen := map[int]bool{}
	var userIDs []int
	for _, t := range tasks {
		for _, id := range reminderRecipients(t) {
			if !seen[id] {
				seen[id] = true
				userIDs = append(userIDs, id)
			}
		}
	}
	prefs, err := storage.ListReminderRecipients(userIDs)
	if err != nil {
		log.Printf("reminders: recipients: %v", err)
		return 0
	}

	created := 0
	for _, t := range tasks {
		for _, uid := range reminderRecipients(t) {
			p, ok := prefs[uid]
			if !ok || p.Timing == storage.ReminderTimingOff {
				continue
			}
			kind := reminderKindAt(t.DueDate, now.In(loadLocation(p.Timezone)), p.Timing)
			if kind == "" {
				continue
			}
			if canRead, _, _, err := storage.CanUserAccessTask(t.TaskID, uid); err != nil || !canRead {
				continue
			}
			first, err := storage.TryMarkReminderSent(t.TaskID, uid, t.DueDate, kind)
			if err != nil {
				log.Printf("reminders: mark task=%d user=%d: %v", t.TaskID, uid, err)
				continue
			}
			if !first {
				continue
			}
			title := "Due today: " + t.Title
			if kind == storage.ReminderKindDayBefore {
				title = "Due tomorrow: " + t.Title
			}
			id, err := storage.CreateUserNotification(storage.UserNotification{
				UserID:    uid,
				Type:      storage.NotificationDueReminder,
				ProjectID: t.ProjectID,
				TaskID:    t.TaskID,
				Title:     title,
				Body:      t.ProjectName,
			})
			if err != nil {
				log.Printf("reminders: notify task=%d user=%d: %v", t.TaskID, uid, err)
				continue
			}
			if id > 0 {
				created++
				live.Push(live.Event{Type: live.TypeNotificationCreated, TaskID: t.TaskID}, []int{uid})
			}
		}
	}
	return created
}
