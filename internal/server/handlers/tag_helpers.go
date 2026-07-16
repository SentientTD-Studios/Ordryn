package handlers

import (
	"GoTodo/internal/storage"
	"net/http"
	"strconv"
)

func resolveTaskTagIDsFromRequest(r *http.Request, userID int) ([]int, error) {
	return storage.ResolveTaskTagIDs(userID, r.Form["tag_ids"], r.FormValue("new_tags"))
}

func assignTaskTagsFromRequest(r *http.Request, taskID, userID int) error {
	tagIDs, err := resolveTaskTagIDsFromRequest(r, userID)
	if err != nil {
		return err
	}
	return storage.SetTaskTags(taskID, userID, tagIDs)
}

func buildTagFormOptions(userID int, selectedIDs map[int]bool) []map[string]interface{} {
	tags, err := storage.GetTagsForUser(userID)
	if err != nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(tags))
	for _, tg := range tags {
		out = append(out, map[string]interface{}{
			"ID":       tg.ID,
			"Name":     tg.Name,
			"Color":    tg.Color,
			"Selected": selectedIDs[tg.ID],
		})
	}
	return out
}

func selectedTagIDMap(tags []storage.Tag) map[int]bool {
	m := make(map[int]bool, len(tags))
	for _, t := range tags {
		m[t.ID] = true
	}
	return m
}

func tagsListForFilter(userID int, tagFilter string) []map[string]interface{} {
	tags, err := storage.GetTagsForUser(userID)
	if err != nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(tags))
	for _, tg := range tags {
		out = append(out, map[string]interface{}{
			"ID":       tg.ID,
			"Name":     tg.Name,
			"Color":    tg.Color,
			"Selected": tagFilter == strconv.Itoa(tg.ID),
		})
	}
	return out
}
