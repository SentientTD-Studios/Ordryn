package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"
	"unicode/utf8"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

const (
	maxStandupFieldLen    = 2000
	maxStandupBlockerLink = 10
	maxStandupClaimed     = 15
)

// StandupState is the revisioned session state: who speaks, in what order, and since when.
type StandupState struct {
	Participants   []int     `json:"participants"`
	Current        int       `json:"current"` // index; len(Participants) means everyone has spoken
	SpeakerSince   time.Time `json:"speaker_since"`
	SpeakerSeconds int       `json:"speaker_seconds"`
}

func decodeStandupState(raw json.RawMessage) StandupState {
	var st StandupState
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &st)
	}
	if st.Participants == nil {
		st.Participants = []int{}
	}
	if st.Current < 0 {
		st.Current = 0
	}
	if st.Current > len(st.Participants) {
		st.Current = len(st.Participants)
	}
	return st
}

func (s StandupState) indexOf(userID int) int {
	for i, id := range s.Participants {
		if id == userID {
			return i
		}
	}
	return -1
}

// CurrentSpeaker is the person talking now, or 0 when everyone has spoken.
func (s StandupState) CurrentSpeaker() int {
	if s.Current >= 0 && s.Current < len(s.Participants) {
		return s.Participants[s.Current]
	}
	return 0
}

// StandupEntryData is a person's check-in.
type StandupEntryData struct {
	Done           string `json:"done"`
	Today          string `json:"today"`
	Blockers       string `json:"blockers"`
	BlockerTaskIDs []int  `json:"blocker_task_ids"`
}

func entryHasBlockers(raw json.RawMessage) bool {
	var d StandupEntryData
	if json.Unmarshal(raw, &d) != nil {
		return false
	}
	return strings.TrimSpace(d.Blockers) != "" || len(d.BlockerTaskIDs) > 0
}

// StandupParticipant is one person's row in the standup.
type StandupParticipant struct {
	UserID       int
	Name         string
	Entry        *StandupEntryData
	UpdatedAt    *time.Time
	BlockerTasks []storage.RefinementTask
	Claimed      []storage.RefinementTask
}

// StandupSession is what a viewer sees of a standup.
type StandupSession struct {
	storage.CeremonySession
	State         StandupState
	Participants  []StandupParticipant
	ServerTime    time.Time
	CanFacilitate bool
	IsFacilitator bool
	CanCreateTask bool
	Joined        bool
}

func (a *ceremonyAccess) buildStandupSession(userID int, s *storage.CeremonySession) (*StandupSession, error) {
	st := decodeStandupState(s.State)
	items, err := storage.ListCeremonyItems(s.ID)
	if err != nil {
		return nil, err
	}
	entries := map[int]storage.CeremonyItem{}
	var blockerIDs []int
	for _, it := range items {
		if it.Type != storage.CeremonyItemEntry {
			continue
		}
		entries[it.AuthorID] = it
		var d StandupEntryData
		if json.Unmarshal(it.Data, &d) == nil {
			blockerIDs = append(blockerIDs, d.BlockerTaskIDs...)
		}
	}
	blockerTasks, err := storage.ListRefinementTasks(s.ProjectID, blockerIDs)
	if err != nil {
		return nil, err
	}
	names, err := storage.UserDisplayNames(st.Participants)
	if err != nil {
		return nil, err
	}
	ended := s.Status != storage.CeremonySessionActive
	out := &StandupSession{
		CeremonySession: *s,
		State:           st,
		Participants:    make([]StandupParticipant, 0, len(st.Participants)),
		ServerTime:      time.Now().UTC(),
		CanFacilitate:   a.canFacilitate,
		IsFacilitator:   s.FacilitatorID == userID,
		CanCreateTask:   storage.HasProjectPerm(s.ProjectID, a.project.Role, storage.PermTasksCreate),
		Joined:          st.indexOf(userID) >= 0,
	}
	for _, uid := range st.Participants {
		p := StandupParticipant{UserID: uid, Name: names[uid], BlockerTasks: []storage.RefinementTask{}, Claimed: []storage.RefinementTask{}}
		if it, ok := entries[uid]; ok {
			var d StandupEntryData
			_ = json.Unmarshal(it.Data, &d)
			p.Entry = &d
			t := it.UpdatedAt
			p.UpdatedAt = &t
			for _, id := range d.BlockerTaskIDs {
				if bt, ok := blockerTasks[id]; ok {
					p.BlockerTasks = append(p.BlockerTasks, bt)
				}
			}
		}
		// Ended standups are a record of what people said; live work is only listed while running.
		if !ended {
			if claimed, err := storage.ClaimedOpenTasks(s.ProjectID, uid, s.SprintID, maxStandupClaimed); err == nil {
				p.Claimed = claimed
			}
		}
		out.Participants = append(out.Participants, p)
	}
	return out, nil
}

// GetActiveStandupSession returns the running standup, or (nil, nil) when none is running.
func GetActiveStandupSession(_ context.Context, userID, projectID int) (*StandupSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyStandup)
	if err != nil {
		return nil, err
	}
	s, err := storage.GetActiveCeremonySession(projectID, storage.CeremonyStandup)
	if err != nil {
		if mapCeremonyStoreErr(err) == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return acc.buildStandupSession(userID, s)
}

// GetStandupSession returns one standup (running or ended).
func GetStandupSession(_ context.Context, userID, projectID, sessionID int) (*StandupSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyStandup)
	if err != nil {
		return nil, err
	}
	s, err := loadCeremonySession(projectID, sessionID, storage.CeremonyStandup)
	if err != nil {
		return nil, err
	}
	return acc.buildStandupSession(userID, s)
}

// ListStandupSessions returns recent standups with entry and blocker counts.
func ListStandupSessions(_ context.Context, userID, projectID, limit int) ([]CeremonySessionSummary, error) {
	return listCeremonySummaries(userID, projectID, storage.CeremonyStandup, limit)
}

// StartStandupInput starts a standup; nil fields fall back to the project defaults.
type StartStandupInput struct {
	Title          string
	SpeakerSeconds *int
	Shuffle        *bool
}

// StartStandupSession opens a standup with the project's human members as participants, scoped to
// the sprint running today (if any) for each person's claimed work.
func StartStandupSession(_ context.Context, userID, projectID int, in StartStandupInput) (*StandupSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyStandup)
	if err != nil {
		return nil, err
	}
	if !acc.canFacilitate {
		return nil, fmt.Errorf("%w: your role cannot run standups", ErrForbidden)
	}
	cfg := loadStandupConfig(projectID)
	if in.SpeakerSeconds != nil {
		cfg.SpeakerSeconds = *in.SpeakerSeconds
	}
	if in.Shuffle != nil {
		cfg.Shuffle = *in.Shuffle
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	title, err := normalizeCeremonyTitle(in.Title, "Standup "+time.Now().UTC().Format("Mon Jan 2"))
	if err != nil {
		return nil, err
	}
	participants, err := standupParticipants(projectID, userID)
	if err != nil {
		return nil, err
	}
	if cfg.Shuffle {
		rand.Shuffle(len(participants), func(i, j int) { participants[i], participants[j] = participants[j], participants[i] })
	}
	sprintID := activeSprintID(projectID)
	raw, err := json.Marshal(StandupState{
		Participants: participants, SpeakerSince: time.Now().UTC(), SpeakerSeconds: cfg.SpeakerSeconds,
	})
	if err != nil {
		return nil, err
	}
	s, err := storage.CreateCeremonySession(projectID, storage.CeremonyStandup, title, userID, sprintID, raw)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	live.AfterCeremonyChange(userID, projectID, storage.CeremonyStandup, s.ID)
	return acc.buildStandupSession(userID, s)
}

// standupParticipants is the project's human members, plus the facilitator if they're not one
// (company-mode access has no member rows).
func standupParticipants(projectID, facilitatorID int) ([]int, error) {
	members, err := storage.ListProjectMembers(projectID)
	if err != nil {
		return nil, err
	}
	out := []int{}
	seen := map[int]bool{}
	for _, m := range members {
		if m.IsAgent || seen[m.UserID] {
			continue
		}
		seen[m.UserID] = true
		out = append(out, m.UserID)
	}
	if !seen[facilitatorID] {
		out = append(out, facilitatorID)
	}
	return out, nil
}

func activeSprintID(projectID int) int {
	sprints, err := storage.ListProjectSprints(projectID)
	if err != nil {
		return 0
	}
	now := time.Now()
	for _, sp := range sprints {
		if storage.SprintIsActive(sp.StartDate, sp.EndDate, now) {
			return sp.ID
		}
	}
	return 0
}

// Standup actions. join is open to any member; the rest are the facilitator's.
const (
	StandupActionNext     = "next"
	StandupActionPrev     = "prev"
	StandupActionGoto     = "goto"
	StandupActionAdd      = "add_participant"
	StandupActionRemove   = "remove_participant"
	StandupActionShuffle  = "shuffle"
	StandupActionEnd      = "end"
	StandupActionTakeOver = "take_over"
	StandupActionJoin     = "join"
)

// StandupActionInput is one step. Index is for goto; UserID for add/remove.
type StandupActionInput struct {
	Action   string
	Revision int
	Index    int
	UserID   int
}

// ApplyStandupAction runs one standup step and broadcasts the new state.
func ApplyStandupAction(ctx context.Context, userID, projectID, sessionID int, in StandupActionInput) (*StandupSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyStandup)
	if err != nil {
		return nil, err
	}
	if in.Action == StandupActionJoin {
		return joinStandup(acc, userID, projectID, sessionID)
	}
	s, err := loadCeremonySession(projectID, sessionID, storage.CeremonyStandup)
	if err != nil {
		return nil, err
	}
	if err := ceremonyStep(acc, s, userID, in.Revision, in.Action == StandupActionTakeOver); err != nil {
		return nil, err
	}
	st := decodeStandupState(s.State)
	speaker := st.CurrentSpeaker()
	newFacilitator, end := 0, false
	switch in.Action {
	case StandupActionTakeOver:
		newFacilitator = userID
	case StandupActionNext:
		st.Current++
	case StandupActionPrev:
		st.Current--
	case StandupActionGoto:
		if in.Index < 0 || in.Index > len(st.Participants) {
			return nil, fmt.Errorf("%w: index out of range", ErrValidation)
		}
		st.Current = in.Index
	case StandupActionAdd:
		if in.UserID <= 0 {
			return nil, fmt.Errorf("%w: user_id is required", ErrValidation)
		}
		if _, err := storage.GetAccessibleProjectByID(projectID, in.UserID); err != nil {
			return nil, fmt.Errorf("%w: that person cannot open this project", ErrValidation)
		}
		if st.indexOf(in.UserID) < 0 {
			st.Participants = append(st.Participants, in.UserID)
		}
	case StandupActionRemove:
		idx := st.indexOf(in.UserID)
		if idx < 0 {
			return nil, fmt.Errorf("%w: not a participant", ErrValidation)
		}
		st.Participants = append(st.Participants[:idx], st.Participants[idx+1:]...)
		if idx < st.Current {
			st.Current--
		}
	case StandupActionShuffle:
		// Shuffle only who hasn't spoken yet; the order so far stays as it happened.
		rest := st.Participants[st.Current:]
		rand.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
	case StandupActionEnd:
		end = true
	default:
		return nil, fmt.Errorf("%w: unknown action %q", ErrValidation, in.Action)
	}
	st = decodeStandupState(mustJSON(st))
	if st.CurrentSpeaker() != speaker {
		st.SpeakerSince = time.Now().UTC()
	}
	updated, err := storage.UpdateCeremonySession(projectID, sessionID, s.Revision, mustJSON(st), newFacilitator, end)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	live.AfterCeremonyChange(userID, projectID, storage.CeremonyStandup, sessionID)
	return acc.buildStandupSession(userID, updated)
}

// joinStandup adds the caller to the end of the speaking order, retrying if the session moves meanwhile.
func joinStandup(acc *ceremonyAccess, userID, projectID, sessionID int) (*StandupSession, error) {
	for attempt := 0; attempt < 5; attempt++ {
		s, err := loadCeremonySession(projectID, sessionID, storage.CeremonyStandup)
		if err != nil {
			return nil, err
		}
		if err := requireActiveSession(s); err != nil {
			return nil, err
		}
		st := decodeStandupState(s.State)
		if st.indexOf(userID) >= 0 {
			return acc.buildStandupSession(userID, s)
		}
		wasDone := st.CurrentSpeaker() == 0
		st.Participants = append(st.Participants, userID)
		if wasDone {
			st.SpeakerSince = time.Now().UTC()
		}
		updated, err := storage.UpdateCeremonySession(projectID, sessionID, s.Revision, mustJSON(st), 0, false)
		if err == storage.ErrCeremonyRevisionConflict {
			continue
		}
		if err != nil {
			return nil, mapCeremonyStoreErr(err)
		}
		live.AfterCeremonyChange(userID, projectID, storage.CeremonyStandup, sessionID)
		return acc.buildStandupSession(userID, updated)
	}
	return nil, mapCeremonyStoreErr(storage.ErrCeremonyRevisionConflict)
}

func standupContext(userID, projectID, sessionID int) (*ceremonyAccess, *storage.CeremonySession, StandupState, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyStandup)
	if err != nil {
		return nil, nil, StandupState{}, err
	}
	s, err := loadCeremonySession(projectID, sessionID, storage.CeremonyStandup)
	if err != nil {
		return nil, nil, StandupState{}, err
	}
	if err := requireActiveSession(s); err != nil {
		return nil, nil, StandupState{}, err
	}
	st := decodeStandupState(s.State)
	if st.indexOf(userID) < 0 {
		return nil, nil, StandupState{}, fmt.Errorf("%w: join the standup first", ErrConflict)
	}
	return acc, s, st, nil
}

func loadEntry(sessionID, userID int) StandupEntryData {
	d := StandupEntryData{BlockerTaskIDs: []int{}}
	items, err := storage.ListCeremonyItems(sessionID)
	if err != nil {
		return d
	}
	for _, it := range items {
		if it.Type == storage.CeremonyItemEntry && it.AuthorID == userID {
			_ = json.Unmarshal(it.Data, &d)
		}
	}
	if d.BlockerTaskIDs == nil {
		d.BlockerTaskIDs = []int{}
	}
	return d
}

// SaveStandupEntry writes the caller's own check-in. BlockerTaskIDs must be tasks in the project.
func SaveStandupEntry(_ context.Context, userID, projectID, sessionID int, in StandupEntryData) (*StandupSession, error) {
	acc, _, _, err := standupContext(userID, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	for _, f := range []*string{&in.Done, &in.Today, &in.Blockers} {
		*f = strings.TrimSpace(*f)
		if utf8.RuneCountInString(*f) > maxStandupFieldLen {
			return nil, fmt.Errorf("%w: each field must be %d characters or less", ErrValidation, maxStandupFieldLen)
		}
	}
	ids, err := storage.FilterProjectTaskIDs(projectID, dedupeInts(in.BlockerTaskIDs))
	if err != nil {
		return nil, err
	}
	if len(ids) > maxStandupBlockerLink {
		return nil, fmt.Errorf("%w: link at most %d blocker tasks", ErrValidation, maxStandupBlockerLink)
	}
	in.BlockerTaskIDs = ids
	if _, err := storage.UpsertCeremonyEntry(sessionID, userID, mustJSON(in)); err != nil {
		return nil, err
	}
	live.AfterCeremonyChange(userID, projectID, storage.CeremonyStandup, sessionID)
	fresh, err := storage.GetCeremonySession(projectID, sessionID)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	return acc.buildStandupSession(userID, fresh)
}

// CreateStandupBlockerTask makes a task for a blocker and links it to the caller's check-in.
func CreateStandupBlockerTask(ctx context.Context, userID, projectID, sessionID int, title string) (*StandupSession, error) {
	acc, s, _, err := standupContext(userID, projectID, sessionID)
	if err != nil {
		return nil, err
	}
	if !storage.HasProjectPerm(projectID, acc.project.Role, storage.PermTasksCreate) {
		return nil, fmt.Errorf("%w: your role cannot create tasks", ErrForbidden)
	}
	t := strings.TrimSpace(title)
	if t == "" {
		return nil, fmt.Errorf("%w: title is required", ErrValidation)
	}
	entry := loadEntry(sessionID, userID)
	if len(entry.BlockerTaskIDs) >= maxStandupBlockerLink {
		return nil, fmt.Errorf("%w: link at most %d blocker tasks", ErrValidation, maxStandupBlockerLink)
	}
	pid := projectID
	var sprint *int
	if s.SprintID > 0 {
		sid := s.SprintID
		sprint = &sid
	}
	taskID, err := CreateTask(ctx, userID, CreateTaskInput{
		Title: t, Description: "Blocker raised in " + s.Title, ProjectID: &pid, SprintID: sprint,
	})
	if err != nil && sprint != nil {
		// A locked sprint refuses new tasks; fall back to the backlog rather than losing the blocker.
		taskID, err = CreateTask(ctx, userID, CreateTaskInput{Title: t, Description: "Blocker raised in " + s.Title, ProjectID: &pid})
	}
	if err != nil {
		return nil, err
	}
	entry.BlockerTaskIDs = append(entry.BlockerTaskIDs, taskID)
	return SaveStandupEntry(ctx, userID, projectID, sessionID, entry)
}

func mustJSON(v interface{}) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
