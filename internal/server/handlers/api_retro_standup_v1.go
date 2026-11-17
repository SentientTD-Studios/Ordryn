package handlers

import (
	"net/http"
	"strconv"
	"time"

	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

type apiCeremonySessionSummaryJSON struct {
	apiCeremonySessionMetaJSON
	Counts map[string]int `json:"counts"`
}

func ceremonySummariesJSON(rows []domain.CeremonySessionSummary) []apiCeremonySessionSummaryJSON {
	out := make([]apiCeremonySessionSummaryJSON, 0, len(rows))
	for _, s := range rows {
		out = append(out, apiCeremonySessionSummaryJSON{apiCeremonySessionMetaJSON: ceremonySessionMetaJSON(s.CeremonySession), Counts: s.Counts})
	}
	return out
}

// ---- retro ----

type apiRetroCardJSON struct {
	ID         int    `json:"id"`
	Column     string `json:"column"`
	Body       string `json:"body,omitempty"`
	AuthorID   *int   `json:"author_id"`
	AuthorName string `json:"author_name,omitempty"`
	Mine       bool   `json:"mine"`
	Hidden     bool   `json:"hidden"`
	ParentID   *int   `json:"parent_id"`
	Votes      int    `json:"votes"`
	VotedByMe  bool   `json:"voted_by_me"`
}

type apiRetroActionJSON struct {
	ID         int    `json:"id"`
	Body       string `json:"body"`
	AuthorID   *int   `json:"author_id"`
	AuthorName string `json:"author_name,omitempty"`
	CardID     *int   `json:"card_id"`
	TaskID     *int   `json:"task_id"`
	TaskTitle  string `json:"task_title,omitempty"`
	Mine       bool   `json:"mine"`
}

type apiRetroSessionJSON struct {
	apiCeremonySessionMetaJSON
	Phase         string               `json:"phase"`
	Template      string               `json:"template"`
	Columns       []domain.RetroColumn `json:"columns"`
	Anonymous     bool                 `json:"anonymous"`
	MaxVotes      int                  `json:"max_votes"`
	VotesUsed     int                  `json:"votes_used"`
	Cards         []apiRetroCardJSON   `json:"cards"`
	Actions       []apiRetroActionJSON `json:"actions"`
	CanFacilitate bool                 `json:"can_facilitate"`
	IsFacilitator bool                 `json:"is_facilitator"`
	CanCreateTask bool                 `json:"can_create_task"`
}

func retroSessionJSON(s *domain.RetroSession) apiRetroSessionJSON {
	cards := make([]apiRetroCardJSON, 0, len(s.Cards))
	for _, c := range s.Cards {
		cards = append(cards, apiRetroCardJSON{
			ID: c.ID, Column: c.Column, Body: c.Body, AuthorID: positiveIntPtr(c.AuthorID), AuthorName: c.AuthorName,
			Mine: c.Mine, Hidden: c.Hidden, ParentID: positiveIntPtr(c.ParentID), Votes: c.Votes, VotedByMe: c.VotedByMe,
		})
	}
	actions := make([]apiRetroActionJSON, 0, len(s.Actions))
	for _, a := range s.Actions {
		actions = append(actions, apiRetroActionJSON{
			ID: a.ID, Body: a.Body, AuthorID: positiveIntPtr(a.AuthorID), AuthorName: a.AuthorName,
			CardID: positiveIntPtr(a.CardID), TaskID: positiveIntPtr(a.TaskID), TaskTitle: a.TaskTitle, Mine: a.Mine,
		})
	}
	return apiRetroSessionJSON{
		apiCeremonySessionMetaJSON: ceremonySessionMetaJSON(s.CeremonySession),
		Phase:                      s.State.Phase, Template: s.State.Template, Columns: s.State.Columns,
		Anonymous: s.State.Anonymous, MaxVotes: s.State.MaxVotes, VotesUsed: s.VotesUsed,
		Cards: cards, Actions: actions,
		CanFacilitate: s.CanFacilitate, IsFacilitator: s.IsFacilitator, CanCreateTask: s.CanCreateTask,
	}
}

type apiRetroStartRequest struct {
	Title     string `json:"title"`
	SprintID  int    `json:"sprint_id"`
	Template  string `json:"template"`
	MaxVotes  *int   `json:"max_votes"`
	Anonymous *bool  `json:"anonymous"`
}

type apiRetroItemRequest struct {
	Type   string `json:"type"`
	Column string `json:"column"`
	Body   string `json:"body"`
	CardID int    `json:"card_id"`
}

type apiRetroItemPatch struct {
	Body     *string     `json:"body"`
	Column   *string     `json:"column"`
	ParentID optionalInt `json:"parent_id"`
}

type apiRetroActionRequest struct {
	Action   string `json:"action"`
	Revision int    `json:"revision"`
	Phase    string `json:"phase"`
}

func writeRetro(w http.ResponseWriter, status int, s *domain.RetroSession, err error) {
	if err != nil {
		writeCeremonyError(w, err)
		return
	}
	writeJSON(w, status, retroSessionJSON(s))
}

// apiV1RetroSessions handles /api/v2/projects/{id}/ceremonies/retro/sessions...
func apiV1RetroSessions(w http.ResponseWriter, r *http.Request, userID, projectID int, rest []string) {
	ctx := r.Context()
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			rows, err := domain.ListRetroSessions(ctx, userID, projectID, limit)
			if err != nil {
				writeCeremonyError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, ceremonySummariesJSON(rows))
		case http.MethodPost:
			var req apiRetroStartRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			s, err := domain.StartRetroSession(ctx, userID, projectID, domain.StartRetroInput{
				Title: req.Title, SprintID: req.SprintID, Template: req.Template, MaxVotes: req.MaxVotes, Anonymous: req.Anonymous,
			})
			writeRetro(w, http.StatusCreated, s, err)
		default:
			methodNotAllowed(w)
		}
		return
	}
	if rest[0] == "active" && len(rest) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s, err := domain.GetActiveRetroSession(ctx, userID, projectID)
		if err != nil {
			writeCeremonyError(w, err)
			return
		}
		if s == nil {
			writeJSON(w, http.StatusOK, map[string]any{"session": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": retroSessionJSON(s)})
		return
	}
	sessionID, err := strconv.Atoi(rest[0])
	if err != nil || sessionID <= 0 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	sub := rest[1:]
	switch {
	case len(sub) == 0 && r.Method == http.MethodGet:
		s, err := domain.GetRetroSession(ctx, userID, projectID, sessionID)
		writeRetro(w, http.StatusOK, s, err)
	case len(sub) == 1 && sub[0] == "actions" && r.Method == http.MethodPost:
		var req apiRetroActionRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.ApplyRetroAction(ctx, userID, projectID, sessionID, domain.RetroActionInput{
			Action: req.Action, Revision: req.Revision, Phase: req.Phase,
		})
		writeRetro(w, http.StatusOK, s, err)
	case len(sub) == 1 && sub[0] == "items" && r.Method == http.MethodPost:
		var req apiRetroItemRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.AddRetroItem(ctx, userID, projectID, sessionID, domain.RetroItemInput{
			Type: req.Type, Column: req.Column, Body: req.Body, CardID: req.CardID,
		})
		writeRetro(w, http.StatusCreated, s, err)
	case len(sub) >= 2 && sub[0] == "items":
		itemID, err := strconv.Atoi(sub[1])
		if err != nil || itemID <= 0 {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		apiV1RetroItem(w, r, userID, projectID, sessionID, itemID, sub[2:])
	case len(sub) <= 1:
		methodNotAllowed(w)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

func apiV1RetroItem(w http.ResponseWriter, r *http.Request, userID, projectID, sessionID, itemID int, rest []string) {
	ctx := r.Context()
	switch {
	case len(rest) == 0 && r.Method == http.MethodPatch:
		var req apiRetroItemPatch
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		p := domain.RetroItemPatch{Body: req.Body, Column: req.Column}
		if req.ParentID.Set {
			p.SetParent = true
			if !req.ParentID.Null {
				p.ParentID = req.ParentID.Value
			}
		}
		s, err := domain.UpdateRetroItem(ctx, userID, projectID, sessionID, itemID, p)
		writeRetro(w, http.StatusOK, s, err)
	case len(rest) == 0 && r.Method == http.MethodDelete:
		s, err := domain.DeleteRetroItem(ctx, userID, projectID, sessionID, itemID)
		writeRetro(w, http.StatusOK, s, err)
	case len(rest) == 1 && rest[0] == "vote" && (r.Method == http.MethodPut || r.Method == http.MethodDelete):
		s, err := domain.VoteRetroCard(ctx, userID, projectID, sessionID, itemID, r.Method == http.MethodPut)
		writeRetro(w, http.StatusOK, s, err)
	case len(rest) == 1 && rest[0] == "task" && r.Method == http.MethodPost:
		s, err := domain.CreateTaskFromRetroAction(ctx, userID, projectID, sessionID, itemID)
		writeRetro(w, http.StatusCreated, s, err)
	case len(rest) <= 1:
		methodNotAllowed(w)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

// ---- standup ----

type apiStandupEntryJSON struct {
	Done           string `json:"done"`
	Today          string `json:"today"`
	Blockers       string `json:"blockers"`
	BlockerTaskIDs []int  `json:"blocker_task_ids"`
}

type apiStandupParticipantJSON struct {
	UserID       int                     `json:"user_id"`
	Name         string                  `json:"name"`
	Entry        *apiStandupEntryJSON    `json:"entry"`
	UpdatedAt    *string                 `json:"updated_at"`
	BlockerTasks []apiRefinementTaskJSON `json:"blocker_tasks"`
	Claimed      []apiRefinementTaskJSON `json:"claimed"`
}

type apiStandupSessionJSON struct {
	apiCeremonySessionMetaJSON
	Current        int                         `json:"current"`
	CurrentUserID  *int                        `json:"current_user_id"`
	SpeakerSince   string                      `json:"speaker_since"`
	SpeakerSeconds int                         `json:"speaker_seconds"`
	ServerTime     string                      `json:"server_time"`
	Participants   []apiStandupParticipantJSON `json:"participants"`
	CanFacilitate  bool                        `json:"can_facilitate"`
	IsFacilitator  bool                        `json:"is_facilitator"`
	CanCreateTask  bool                        `json:"can_create_task"`
	Joined         bool                        `json:"joined"`
}

func refinementTasksJSON(in []storage.RefinementTask) []apiRefinementTaskJSON {
	out := make([]apiRefinementTaskJSON, 0, len(in))
	for _, t := range in {
		out = append(out, apiRefinementTaskJSON{
			ID: t.ID, ParentID: positiveIntPtr(t.ParentID), ParentTitle: t.ParentTitle, Title: t.Title,
			Priority: t.Priority, Completed: t.Completed, StatusName: t.StatusName,
			SprintID: positiveIntPtr(t.SprintID), SprintName: t.SprintName, ClaimedByName: t.ClaimedByName,
			EstimateHours: t.EstimateHours, Refinement: t.Refinement, Deleted: t.Trashed,
		})
	}
	return out
}

func standupSessionJSON(s *domain.StandupSession) apiStandupSessionJSON {
	ps := make([]apiStandupParticipantJSON, 0, len(s.Participants))
	for _, p := range s.Participants {
		row := apiStandupParticipantJSON{
			UserID: p.UserID, Name: p.Name,
			BlockerTasks: refinementTasksJSON(p.BlockerTasks), Claimed: refinementTasksJSON(p.Claimed),
		}
		if p.Entry != nil {
			ids := p.Entry.BlockerTaskIDs
			if ids == nil {
				ids = []int{}
			}
			row.Entry = &apiStandupEntryJSON{Done: p.Entry.Done, Today: p.Entry.Today, Blockers: p.Entry.Blockers, BlockerTaskIDs: ids}
		}
		if p.UpdatedAt != nil {
			v := p.UpdatedAt.UTC().Format(time.RFC3339)
			row.UpdatedAt = &v
		}
		ps = append(ps, row)
	}
	return apiStandupSessionJSON{
		apiCeremonySessionMetaJSON: ceremonySessionMetaJSON(s.CeremonySession),
		Current:                    s.State.Current,
		CurrentUserID:              positiveIntPtr(s.State.CurrentSpeaker()),
		SpeakerSince:               s.State.SpeakerSince.UTC().Format(time.RFC3339),
		SpeakerSeconds:             s.State.SpeakerSeconds,
		ServerTime:                 s.ServerTime.UTC().Format(time.RFC3339),
		Participants:               ps,
		CanFacilitate:              s.CanFacilitate, IsFacilitator: s.IsFacilitator,
		CanCreateTask: s.CanCreateTask, Joined: s.Joined,
	}
}

type apiStandupStartRequest struct {
	Title          string `json:"title"`
	SpeakerSeconds *int   `json:"speaker_seconds"`
	Shuffle        *bool  `json:"shuffle"`
}

type apiStandupActionRequest struct {
	Action   string `json:"action"`
	Revision int    `json:"revision"`
	Index    int    `json:"index"`
	UserID   int    `json:"user_id"`
}

func writeStandup(w http.ResponseWriter, status int, s *domain.StandupSession, err error) {
	if err != nil {
		writeCeremonyError(w, err)
		return
	}
	writeJSON(w, status, standupSessionJSON(s))
}

// apiV1StandupSessions handles /api/v2/projects/{id}/ceremonies/standup/sessions...
func apiV1StandupSessions(w http.ResponseWriter, r *http.Request, userID, projectID int, rest []string) {
	ctx := r.Context()
	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			rows, err := domain.ListStandupSessions(ctx, userID, projectID, limit)
			if err != nil {
				writeCeremonyError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, ceremonySummariesJSON(rows))
		case http.MethodPost:
			var req apiStandupStartRequest
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			s, err := domain.StartStandupSession(ctx, userID, projectID, domain.StartStandupInput{
				Title: req.Title, SpeakerSeconds: req.SpeakerSeconds, Shuffle: req.Shuffle,
			})
			writeStandup(w, http.StatusCreated, s, err)
		default:
			methodNotAllowed(w)
		}
		return
	}
	if rest[0] == "active" && len(rest) == 1 {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		s, err := domain.GetActiveStandupSession(ctx, userID, projectID)
		if err != nil {
			writeCeremonyError(w, err)
			return
		}
		if s == nil {
			writeJSON(w, http.StatusOK, map[string]any{"session": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": standupSessionJSON(s)})
		return
	}
	sessionID, err := strconv.Atoi(rest[0])
	if err != nil || sessionID <= 0 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	sub := rest[1:]
	switch {
	case len(sub) == 0 && r.Method == http.MethodGet:
		s, err := domain.GetStandupSession(ctx, userID, projectID, sessionID)
		writeStandup(w, http.StatusOK, s, err)
	case len(sub) == 1 && sub[0] == "actions" && r.Method == http.MethodPost:
		var req apiStandupActionRequest
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.ApplyStandupAction(ctx, userID, projectID, sessionID, domain.StandupActionInput{
			Action: req.Action, Revision: req.Revision, Index: req.Index, UserID: req.UserID,
		})
		writeStandup(w, http.StatusOK, s, err)
	case len(sub) == 1 && sub[0] == "entry" && r.Method == http.MethodPut:
		var req apiStandupEntryJSON
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.SaveStandupEntry(ctx, userID, projectID, sessionID, domain.StandupEntryData{
			Done: req.Done, Today: req.Today, Blockers: req.Blockers, BlockerTaskIDs: req.BlockerTaskIDs,
		})
		writeStandup(w, http.StatusOK, s, err)
	case len(sub) == 1 && sub[0] == "blockers" && r.Method == http.MethodPost:
		var req struct {
			Title string `json:"title"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		s, err := domain.CreateStandupBlockerTask(ctx, userID, projectID, sessionID, req.Title)
		writeStandup(w, http.StatusCreated, s, err)
	case len(sub) <= 1:
		methodNotAllowed(w)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}
