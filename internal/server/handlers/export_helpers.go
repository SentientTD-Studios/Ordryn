package handlers

import (
	"GoTodo/internal/tasks"
	"strconv"
	"strings"
)

type exportTaskJSON struct {
	ID          int             `json:"id"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Completed   bool            `json:"completed"`
	DueDate     string          `json:"due_date"`
	Project     string          `json:"project"`
	Priority    int             `json:"priority"`
	Favorite    bool            `json:"favorite"`
	Position    int             `json:"position"`
	Tags        []exportTagJSON `json:"tags"`
	CreatedAt   string          `json:"created_at"`
	ModifiedAt  string          `json:"modified_at"`
}

type exportTagJSON struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

func taskToExportJSON(t tasks.Task) exportTaskJSON {
	tags := make([]exportTagJSON, 0, len(t.Tags))
	for _, tg := range t.Tags {
		tags = append(tags, exportTagJSON{ID: tg.ID, Name: tg.Name, Color: tg.Color})
	}
	return exportTaskJSON{
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Completed:   t.Completed,
		DueDate:     t.DueDate,
		Project:     t.ProjectName,
		Priority:    t.Priority,
		Favorite:    t.IsFavorite,
		Position:    t.Position,
		Tags:        tags,
		CreatedAt:   t.DateCreated,
		ModifiedAt:  t.DateModified,
	}
}

func taskToCSVRow(t tasks.Task) []string {
	tagNames := make([]string, 0, len(t.Tags))
	for _, tg := range t.Tags {
		tagNames = append(tagNames, tg.Name)
	}
	return []string{
		strconv.Itoa(t.ID),
		t.Title,
		t.Description,
		strconv.FormatBool(t.Completed),
		t.DueDate,
		t.ProjectName,
		strconv.Itoa(t.Priority),
		strconv.FormatBool(t.IsFavorite),
		strconv.Itoa(t.Position),
		strings.Join(tagNames, ";"),
		t.DateCreated,
		t.DateModified,
	}
}
