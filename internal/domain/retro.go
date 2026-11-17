package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// Retro templates. Action items are collected in the Act phase, not as a column.
const (
	RetroTemplateWentWell          = "went_well"
	RetroTemplateStartStopContinue = "start_stop_continue"
	RetroTemplateFourLs            = "four_ls"
	RetroTemplateMadSadGlad        = "mad_sad_glad"
)

// RetroColumn is one column of cards.
type RetroColumn struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

var retroTemplates = map[string][]RetroColumn{
	RetroTemplateWentWell:          {{"went_well", "Went well"}, {"to_improve", "To improve"}, {"ideas", "Ideas"}},
	RetroTemplateStartStopContinue: {{"start", "Start"}, {"stop", "Stop"}, {"continue", "Continue"}},
	RetroTemplateFourLs:            {{"liked", "Liked"}, {"learned", "Learned"}, {"lacked", "Lacked"}, {"longed_for", "Longed for"}},
	RetroTemplateMadSadGlad:        {{"mad", "Mad"}, {"sad", "Sad"}, {"glad", "Glad"}},
}

// RetroTemplateIDs lists templates in display order.
var RetroTemplateIDs = []string{RetroTemplateWentWell, RetroTemplateStartStopContinue, RetroTemplateFourLs, RetroTemplateMadSadGlad}

// RetroTemplateColumns returns a template's columns (nil when unknown).
func RetroTemplateColumns(id string) []RetroColumn { return retroTemplates[id] }

// Retro phases, in order. The facilitator may move to any phase.
const (
	RetroPhaseCollect = "collect"
	RetroPhaseGroup   = "group"
	RetroPhaseVote    = "vote"
	RetroPhaseAct     = "act"
)

var retroPhases = []string{RetroPhaseCollect, RetroPhaseGroup, RetroPhaseVote, RetroPhaseAct}

const maxRetroCardLen = 500

// RetroState is the revisioned session state; cards and votes live in their own rows.
type RetroState struct {
	Phase     string        `json:"phase"`
	Template  string        `json:"template"`
	Columns   []RetroColumn `json:"columns"`
	Anonymous bool          `json:"anonymous"`
	MaxVotes  int           `json:"max_votes"`
}

func decodeRetroState(raw json.RawMessage) RetroState {
	st := RetroState{Phase: RetroPhaseCollect, Template: RetroTemplateWentWell}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &st)
	}
	if len(st.Columns) == 0 {
		st.Columns = retroTemplates[RetroTemplateWentWell]
	}
	return st
}

func (s RetroState) hasColumn(key string) bool {
	for _, c := range s.Columns {
		if c.Key == key {
			return true
		}
	}
	return false
}

// RetroCard is one card as the viewer may see it. Hidden cards (others' cards while collecting) carry
// only their id and column so the board can show how many there are.
type RetroCard struct {
	ID         int
	Column     string
	Body       string
	AuthorID   int
	AuthorName string
	Mine       bool
	Hidden     bool
	ParentID   int
	Votes      int // shown in the Act phase only
	VotedByMe  bool
}

// RetroAction is an action item from the Act phase.
type RetroAction struct {
	ID         int
	Body       string
	AuthorID   int
	AuthorName string
	CardID     int
	TaskID     int
	TaskTitle  string
	Mine       bool
}

// RetroSession is what a viewer sees of a retrospective.
type RetroSession struct {
	storage.CeremonySession
	State         RetroState
	Cards         []RetroCard
	Actions       []RetroAction
	VotesUsed     int
	CanFacilitate bool
	IsFacilitator bool
	CanCreateTask bool
}

type retroActionData struct {
	CardID int `json:"card_id,omitempty"`
	TaskID int `json:"task_id,omitempty"`
}

func (a *ceremonyAccess) buildRetroSession(userID int, s *storage.CeremonySession) (*RetroSession, error) {
	st := decodeRetroState(s.State)
	items, err := storage.ListCeremonyItems(s.ID)
	if err != nil {
		return nil, err
	}
	counts, mine, err := storage.CeremonyVoteCounts(s.ID, userID)
	if err != nil {
		return nil, err
	}
	out := &RetroSession{
		CeremonySession: *s,
		State:           st,
		Cards:           []RetroCard{},
		Actions:         []RetroAction{},
		VotesUsed:       len(mine),
		CanFacilitate:   a.canFacilitate,
		IsFacilitator:   s.FacilitatorID == userID,
		CanCreateTask:   storage.HasProjectPerm(s.ProjectID, a.project.Role, storage.PermTasksCreate),
	}
	ended := s.Status != storage.CeremonySessionActive
	var taskIDs []int
	for _, it := range items {
		isMine := it.AuthorID == userID
		switch it.Type {
		case storage.CeremonyItemCard:
			c := RetroCard{ID: it.ID, Column: it.Column, Mine: isMine, ParentID: it.ParentID, VotedByMe: mine[it.ID]}
			if !ended && st.Phase == RetroPhaseCollect && !isMine {
				c.Hidden = true
			} else {
				c.Body = it.Body
				if !st.Anonymous || isMine {
					c.AuthorID, c.AuthorName = it.AuthorID, it.AuthorName
				}
			}
			if ended || st.Phase == RetroPhaseAct {
				c.Votes = counts[it.ID]
			}
			out.Cards = append(out.Cards, c)
		case storage.CeremonyItemAction:
			var d retroActionData
			_ = json.Unmarshal(it.Data, &d)
			out.Actions = append(out.Actions, RetroAction{
				ID: it.ID, Body: it.Body, AuthorID: it.AuthorID, AuthorName: it.AuthorName,
				CardID: d.CardID, TaskID: d.TaskID, Mine: isMine,
			})
			if d.TaskID > 0 {
				taskIDs = append(taskIDs, d.TaskID)
			}
		}
	}
	if len(taskIDs) > 0 {
		if byID, err := storage.ListRefinementTasks(s.ProjectID, taskIDs); err == nil {
			for i := range out.Actions {
				if t, ok := byID[out.Actions[i].TaskID]; ok {
					out.Actions[i].TaskTitle = t.Title
				}
			}
		}
	}
	return out, nil
}

// GetActiveRetroSession returns the running retro, or (nil, nil) when none is running.
func GetActiveRetroSession(_ context.Context, userID, projectID int) (*RetroSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRetro)
	if err != nil {
		return nil, err
	}
	s, err := storage.GetActiveCeremonySession(projectID, storage.CeremonyRetro)
	if err != nil {
		if mapCeremonyStoreErr(err) == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return acc.buildRetroSession(userID, s)
}

// GetRetroSession returns one retro (running or ended).
func GetRetroSession(_ context.Context, userID, projectID, sessionID int) (*RetroSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRetro)
	if err != nil {
		return nil, err
	}
	s, err := loadCeremonySession(projectID, sessionID, storage.CeremonyRetro)
	if err != nil {
		return nil, err
	}
	return acc.buildRetroSession(userID, s)
}

// ListRetroSessions returns recent retros with card and action counts.
func ListRetroSessions(_ context.Context, userID, projectID, limit int) ([]CeremonySessionSummary, error) {
	return listCeremonySummaries(userID, projectID, storage.CeremonyRetro, limit)
}

func listCeremonySummaries(userID, projectID int, kind string, limit int) ([]CeremonySessionSummary, error) {
	if _, err := requireCeremony(projectID, userID, kind); err != nil {
		return nil, err
	}
	rows, err := storage.ListCeremonySessions(projectID, kind, limit)
	if err != nil {
		return nil, err
	}
	out := make([]CeremonySessionSummary, 0, len(rows))
	for _, s := range rows {
		counts := map[string]int{}
		if items, err := storage.ListCeremonyItems(s.ID); err == nil {
			for _, it := range items {
				counts[it.Type]++
				if it.Type == storage.CeremonyItemEntry && entryHasBlockers(it.Data) {
					counts["blocked"]++
				}
			}
		}
		out = append(out, CeremonySessionSummary{CeremonySession: s, Counts: counts})
	}
	return out, nil
}

// StartRetroInput starts a retro; zero values fall back to the project's defaults.
type StartRetroInput struct {
	Title     string
	SprintID  int
	Template  string
	MaxVotes  *int
	Anonymous *bool
}

// StartRetroSession opens the project's retrospective with the caller facilitating.
func StartRetroSession(_ context.Context, userID, projectID int, in StartRetroInput) (*RetroSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRetro)
	if err != nil {
		return nil, err
	}
	if !acc.canFacilitate {
		return nil, fmt.Errorf("%w: your role cannot run retrospectives", ErrForbidden)
	}
	cfg := loadRetroConfig(projectID)
	if in.Template != "" {
		cfg.Template = in.Template
	}
	if in.MaxVotes != nil {
		cfg.MaxVotes = *in.MaxVotes
	}
	if in.Anonymous != nil {
		cfg.Anonymous = *in.Anonymous
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	fallback := "Retrospective " + time.Now().UTC().Format("Jan 2")
	if in.SprintID > 0 {
		sp, err := storage.GetProjectSprint(projectID, in.SprintID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid sprint_id", ErrValidation)
		}
		fallback = sp.Name + " retrospective"
	}
	title, err := normalizeCeremonyTitle(in.Title, fallback)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(RetroState{
		Phase: RetroPhaseCollect, Template: cfg.Template, Columns: retroTemplates[cfg.Template],
		Anonymous: cfg.Anonymous, MaxVotes: cfg.MaxVotes,
	})
	if err != nil {
		return nil, err
	}
	s, err := storage.CreateCeremonySession(projectID, storage.CeremonyRetro, title, userID, in.SprintID, raw)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	live.AfterCeremonyChange(userID, projectID, storage.CeremonyRetro, s.ID)
	return acc.buildRetroSession(userID, s)
}

// Retro facilitator actions.
const (
	RetroActionPhase    = "phase"
	RetroActionEnd      = "end"
	RetroActionTakeOver = "take_over"
)

// RetroActionInput is one facilitator step.
type RetroActionInput struct {
	Action   string
	Revision int
	Phase    string
}

// ApplyRetroAction moves the retro between phases, ends it, or hands it to a new facilitator.
func ApplyRetroAction(_ context.Context, userID, projectID, sessionID int, in RetroActionInput) (*RetroSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRetro)
	if err != nil {
		return nil, err
	}
	s, err := loadCeremonySession(projectID, sessionID, storage.CeremonyRetro)
	if err != nil {
		return nil, err
	}
	if err := ceremonyStep(acc, s, userID, in.Revision, in.Action == RetroActionTakeOver); err != nil {
		return nil, err
	}
	st := decodeRetroState(s.State)
	newFacilitator, end := 0, false
	switch in.Action {
	case RetroActionTakeOver:
		newFacilitator = userID
	case RetroActionPhase:
		if !containsString(retroPhases, in.Phase) {
			return nil, fmt.Errorf("%w: unknown phase %q", ErrValidation, in.Phase)
		}
		st.Phase = in.Phase
	case RetroActionEnd:
		end = true
	default:
		return nil, fmt.Errorf("%w: unknown action %q", ErrValidation, in.Action)
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	updated, err := storage.UpdateCeremonySession(projectID, sessionID, s.Revision, raw, newFacilitator, end)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	live.AfterCeremonyChange(userID, projectID, storage.CeremonyRetro, sessionID)
	return acc.buildRetroSession(userID, updated)
}

// RetroItemInput adds or edits a card or action item.
type RetroItemInput struct {
	Type   string // storage.CeremonyItemCard or storage.CeremonyItemAction
	Column string
	Body   string
	CardID int // action items: the card that prompted it (optional)
}

func normalizeRetroBody(body string) (string, error) {
	b := strings.TrimSpace(body)
	if b == "" {
		return "", fmt.Errorf("%w: text is required", ErrValidation)
	}
	if utf8.RuneCountInString(b) > maxRetroCardLen {
		return "", fmt.Errorf("%w: text must be %d characters or less", ErrValidation, maxRetroCardLen)
	}
	return b, nil
}

// retroContext loads what every participant change needs.
func retroContext(userID, projectID, sessionID int) (*ceremonyAccess, *storage.CeremonySession, RetroState, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRetro)
	if err != nil {
		return nil, nil, RetroState{}, err
	}
	s, err := loadCeremonySession(projectID, sessionID, storage.CeremonyRetro)
	if err != nil {
		return nil, nil, RetroState{}, err
	}
	if err := requireActiveSession(s); err != nil {
		return nil, nil, RetroState{}, err
	}
	return acc, s, decodeRetroState(s.State), nil
}

func (a *ceremonyAccess) retroChanged(userID int, s *storage.CeremonySession) (*RetroSession, error) {
	live.AfterCeremonyChange(userID, s.ProjectID, storage.CeremonyRetro, s.ID)
	fresh, err := storage.GetCeremonySession(s.ProjectID, s.ID)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	return a.buildRetroSession(userID, fresh)
}

// AddRetroItem adds a card (Collect and Group phases) or an action item (Act phase). Any member may.
func AddRetroItem(_ context.Context, userID, projectID, sessionID int, in RetroItemInput) (*RetroSession, error) {
	acc, s, st, err := retroContext(userID, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	body, err := normalizeRetroBody(in.Body)
	if err != nil {
		return nil, err
	}
	item := storage.CeremonyItem{SessionID: sessionID, AuthorID: userID, Body: body}
	switch in.Type {
	case storage.CeremonyItemCard:
		if st.Phase != RetroPhaseCollect && st.Phase != RetroPhaseGroup {
			return nil, fmt.Errorf("%w: cards can only be added while collecting or grouping", ErrConflict)
		}
		if !st.hasColumn(in.Column) {
			return nil, fmt.Errorf("%w: unknown column %q", ErrValidation, in.Column)
		}
		item.Type, item.Column = storage.CeremonyItemCard, in.Column
	case storage.CeremonyItemAction:
		if st.Phase != RetroPhaseAct {
			return nil, fmt.Errorf("%w: action items are added in the Act phase", ErrConflict)
		}
		d := retroActionData{}
		if in.CardID > 0 {
			card, err := storage.GetCeremonyItem(sessionID, in.CardID)
			if err != nil || card.Type != storage.CeremonyItemCard {
				return nil, fmt.Errorf("%w: card not found", ErrValidation)
			}
			d.CardID = in.CardID
		}
		raw, _ := json.Marshal(d)
		item.Type, item.Data = storage.CeremonyItemAction, raw
	default:
		return nil, fmt.Errorf("%w: unknown item type %q", ErrValidation, in.Type)
	}
	if _, err := storage.CreateCeremonyItem(item); err != nil {
		return nil, err
	}
	return acc.retroChanged(userID, s)
}

// RetroItemPatch edits an item; nil fields are left alone. ParentID 0 with SetParent ungroups.
type RetroItemPatch struct {
	Body      *string
	Column    *string
	SetParent bool
	ParentID  int
}

// UpdateRetroItem edits a card or action item.
//   - Text: the author, while cards are open (Collect/Group) or for action items in Act.
//   - Column: the author or facilitator, while collecting or grouping.
//   - Grouping (stack a card under another top-level card, or ungroup it): anyone, in the Group phase.
func UpdateRetroItem(_ context.Context, userID, projectID, sessionID, itemID int, p RetroItemPatch) (*RetroSession, error) {
	acc, s, st, err := retroContext(userID, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	it, err := storage.GetCeremonyItem(sessionID, itemID)
	if err != nil {
		return nil, ErrNotFound
	}
	isAuthor := it.AuthorID == userID
	isFacilitator := s.FacilitatorID == userID
	cardsOpen := st.Phase == RetroPhaseCollect || st.Phase == RetroPhaseGroup

	if p.Body != nil {
		body, err := normalizeRetroBody(*p.Body)
		if err != nil {
			return nil, err
		}
		if !isAuthor {
			return nil, fmt.Errorf("%w: only the author can edit this text", ErrForbidden)
		}
		if it.Type == storage.CeremonyItemCard && !cardsOpen {
			return nil, fmt.Errorf("%w: cards are locked after grouping", ErrConflict)
		}
		it.Body = body
	}
	if p.Column != nil && *p.Column != it.Column {
		if it.Type != storage.CeremonyItemCard || !cardsOpen {
			return nil, fmt.Errorf("%w: cards can only move while collecting or grouping", ErrConflict)
		}
		if !isAuthor && !isFacilitator {
			return nil, fmt.Errorf("%w: only the author or facilitator can move this card", ErrForbidden)
		}
		if !st.hasColumn(*p.Column) {
			return nil, fmt.Errorf("%w: unknown column %q", ErrValidation, *p.Column)
		}
		if it.ParentID > 0 {
			return nil, fmt.Errorf("%w: ungroup the card before moving it", ErrValidation)
		}
		it.Column = *p.Column
		if err := storage.MoveCeremonyChildren(sessionID, it.ID, it.Column); err != nil {
			return nil, err
		}
	}
	if p.SetParent {
		if it.Type != storage.CeremonyItemCard || st.Phase != RetroPhaseGroup {
			return nil, fmt.Errorf("%w: cards are grouped in the Group phase", ErrConflict)
		}
		if p.ParentID > 0 {
			if p.ParentID == it.ID {
				return nil, fmt.Errorf("%w: a card cannot be grouped under itself", ErrValidation)
			}
			parent, err := storage.GetCeremonyItem(sessionID, p.ParentID)
			if err != nil || parent.Type != storage.CeremonyItemCard {
				return nil, fmt.Errorf("%w: card not found", ErrValidation)
			}
			if parent.ParentID > 0 {
				return nil, fmt.Errorf("%w: group under the top card of a group", ErrValidation)
			}
			// Stacking a group's top card under another card merges the two groups.
			if err := storage.ReparentCeremonyChildren(sessionID, it.ID, parent.ID, parent.Column); err != nil {
				return nil, err
			}
			it.Column = parent.Column
		}
		it.ParentID = p.ParentID
	}
	if _, err := storage.UpdateCeremonyItem(*it); err != nil {
		return nil, mapItemErr(err)
	}
	return acc.retroChanged(userID, s)
}

// DeleteRetroItem removes a card or action item (author or facilitator).
func DeleteRetroItem(_ context.Context, userID, projectID, sessionID, itemID int) (*RetroSession, error) {
	acc, s, _, err := retroContext(userID, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	it, err := storage.GetCeremonyItem(sessionID, itemID)
	if err != nil {
		return nil, ErrNotFound
	}
	if it.AuthorID != userID && s.FacilitatorID != userID {
		return nil, fmt.Errorf("%w: only the author or facilitator can delete this", ErrForbidden)
	}
	if err := storage.UngroupCeremonyChildren(sessionID, itemID, ""); err != nil {
		return nil, err
	}
	if err := storage.DeleteCeremonyItem(sessionID, itemID); err != nil {
		return nil, mapItemErr(err)
	}
	return acc.retroChanged(userID, s)
}

// VoteRetroCard adds or removes the caller's vote on a top-level card, during the Vote phase.
func VoteRetroCard(_ context.Context, userID, projectID, sessionID, itemID int, on bool) (*RetroSession, error) {
	acc, s, st, err := retroContext(userID, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	if st.Phase != RetroPhaseVote {
		return nil, fmt.Errorf("%w: voting is open in the Vote phase only", ErrConflict)
	}
	it, err := storage.GetCeremonyItem(sessionID, itemID)
	if err != nil || it.Type != storage.CeremonyItemCard {
		return nil, ErrNotFound
	}
	if it.ParentID > 0 {
		return nil, fmt.Errorf("%w: vote on the top card of a group", ErrValidation)
	}
	ok, err := storage.SetCeremonyVote(sessionID, itemID, userID, on, st.MaxVotes)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: you have used all %d votes", ErrConflict, st.MaxVotes)
	}
	return acc.retroChanged(userID, s)
}

// CreateTaskFromRetroAction turns an action item into a task in the project (needs tasks:create).
// The task lands in the retro's sprint when it has one and that sprint still accepts tasks.
func CreateTaskFromRetroAction(ctx context.Context, userID, projectID, sessionID, itemID int) (*RetroSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRetro)
	if err != nil {
		return nil, err
	}
	s, err := loadCeremonySession(projectID, sessionID, storage.CeremonyRetro)
	if err != nil {
		return nil, err
	}
	it, err := storage.GetCeremonyItem(sessionID, itemID)
	if err != nil || it.Type != storage.CeremonyItemAction {
		return nil, ErrNotFound
	}
	var d retroActionData
	_ = json.Unmarshal(it.Data, &d)
	if d.TaskID > 0 {
		return nil, fmt.Errorf("%w: this action item already has a task", ErrConflict)
	}
	if !storage.HasProjectPerm(projectID, acc.project.Role, storage.PermTasksCreate) {
		return nil, fmt.Errorf("%w: your role cannot create tasks", ErrForbidden)
	}
	title := it.Body
	if utf8.RuneCountInString(title) > 200 {
		title = string([]rune(title)[:200])
	}
	pid := projectID
	taskID, err := CreateTask(ctx, userID, CreateTaskInput{
		Title:       title,
		Description: "From retrospective: " + s.Title,
		ProjectID:   &pid,
	})
	if err != nil {
		return nil, err
	}
	d.TaskID = taskID
	raw, _ := json.Marshal(d)
	it.Data = raw
	if _, err := storage.UpdateCeremonyItem(*it); err != nil {
		return nil, err
	}
	return acc.retroChanged(userID, s)
}

func mapItemErr(err error) error {
	if err == storage.ErrCeremonyItemNotFound {
		return ErrNotFound
	}
	return err
}

func containsString(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
