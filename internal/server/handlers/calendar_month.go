package handlers

import (
	"context"
	"time"

	"GoTodo/internal/storage"
)

type calendarTaskJSON struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Due         string `json:"due"`
	Priority    int    `json:"priority"`
	ProjectName string `json:"project_name"`
	Completed   bool   `json:"completed"`
}

type calendarCellJSON struct {
	Date    string             `json:"date"`
	Day     int                `json:"day"`
	InMonth bool               `json:"in_month"`
	IsToday bool               `json:"is_today"`
	Tasks   []calendarTaskJSON `json:"tasks"`
}

type calendarMonthJSON struct {
	YearMonth  string              `json:"year_month"`
	MonthLabel string              `json:"month_label"`
	PrevMonth  string              `json:"prev_month"`
	NextMonth  string              `json:"next_month"`
	TodayMonth string              `json:"today_month"`
	Year       int                 `json:"year"`
	Weeks      [][]calendarCellJSON `json:"weeks"`
}

func loadLocation(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

func isValidYearMonth(s string) bool {
	if len(s) != 7 || s[4] != '-' {
		return false
	}
	_, err := time.Parse("2006-01", s)
	return err == nil
}

func currentYearMonth(timezone string) string {
	return time.Now().In(loadLocation(timezone)).Format("2006-01")
}

func buildCalendarMonthJSON(userID int, timezone, yearMonth string) calendarMonthJSON {
	loc := loadLocation(timezone)
	monthStart, err := time.ParseInLocation("2006-01", yearMonth, loc)
	if err != nil {
		return calendarMonthJSON{YearMonth: yearMonth}
	}

	tasksByDate := map[string][]calendarTaskJSON{}
	if userID > 0 {
		tasksByDate = fetchCalendarTasksByDate(userID, timezone, yearMonth)
	}
	today := time.Now().In(loc).Format("2006-01-02")

	gridStart := monthStart
	for gridStart.Weekday() != time.Sunday {
		gridStart = gridStart.AddDate(0, 0, -1)
	}

	weeks := make([][]calendarCellJSON, 0, 6)
	cursor := gridStart
	for w := 0; w < 6; w++ {
		week := make([]calendarCellJSON, 7)
		for d := 0; d < 7; d++ {
			dateStr := cursor.Format("2006-01-02")
			tasks := tasksByDate[dateStr]
			if tasks == nil {
				tasks = []calendarTaskJSON{}
			}
			week[d] = calendarCellJSON{
				Date:    dateStr,
				Day:     cursor.Day(),
				InMonth: cursor.Month() == monthStart.Month() && cursor.Year() == monthStart.Year(),
				IsToday: dateStr == today,
				Tasks:   tasks,
			}
			cursor = cursor.AddDate(0, 0, 1)
		}
		weeks = append(weeks, week)
	}

	return calendarMonthJSON{
		YearMonth:  yearMonth,
		MonthLabel: monthStart.Format("January 2006"),
		PrevMonth:  monthStart.AddDate(0, -1, 0).Format("2006-01"),
		NextMonth:  monthStart.AddDate(0, 1, 0).Format("2006-01"),
		TodayMonth: time.Now().In(loc).Format("2006-01"),
		Year:       monthStart.Year(),
		Weeks:      weeks,
	}
}

func fetchCalendarTasksByDate(userID int, timezone, yearMonth string) map[string][]calendarTaskJSON {
	pool, err := storage.OpenDatabase()
	if err != nil {
		return nil
	}
	defer storage.CloseDatabase(pool)

	loc := loadLocation(timezone)
	monthStart, err := time.ParseInLocation("2006-01", yearMonth, loc)
	if err != nil {
		return nil
	}
	start := monthStart.Format("2006-01-02")
	end := monthStart.AddDate(0, 1, 0).Format("2006-01-02")

	rows, err := pool.Query(context.Background(), `
		SELECT t.id, t.title, CAST(t.due_date AS TEXT), COALESCE(t.priority, 0), COALESCE(p.name, ''), COALESCE(t.completed, false)
		FROM tasks t
		LEFT JOIN projects p ON t.project_id = p.id
		WHERE t.user_id = $1 AND t.due_date IS NOT NULL
		  AND t.due_date >= $2::date AND t.due_date < $3::date
		ORDER BY t.due_date, t.completed ASC, t.priority DESC, t.id`, userID, start, end)
	if err != nil {
		return nil
	}
	defer rows.Close()

	byDate := map[string][]calendarTaskJSON{}
	for rows.Next() {
		var ct calendarTaskJSON
		if err := rows.Scan(&ct.ID, &ct.Title, &ct.Due, &ct.Priority, &ct.ProjectName, &ct.Completed); err != nil {
			continue
		}
		byDate[ct.Due] = append(byDate[ct.Due], ct)
	}
	return byDate
}
