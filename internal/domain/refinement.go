package domain

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// Refinement outcomes recorded per task in a session.
const (
	RefinementOutcomeRefined = "refined"
	RefinementOutcomeSkipped = "skipped"
)

// Refinement session actions. Only the facilitator may send them, except take_over.
const (
	RefinementActionNext     = "next"
	RefinementActionPrev     = "prev"
	RefinementActionGoto     = "goto"
	RefinementActionRefine   = "refine"
	RefinementActionSkip     = "skip"
	RefinementActionAdd      = "add"
	RefinementActionRemove   = "remove"
	RefinementActionEnd      = "end"
	RefinementActionTakeOver = "take_over"
)

// RefinementState is the JSON stored on a refinement session.
type RefinementState struct {
	Queue []int `json:"queue"`
	// Current indexes Queue; len(Queue) means the facilitator walked past the last task.
	Current  int            `json:"current"`
	Outcomes map[int]string `json:"outcomes"`
}

func (s *RefinementState) normalize() {
	if s.Queue == nil {
		s.Queue = []int{}
	}
	if s.Outcomes == nil {
		s.Outcomes = map[int]string{}
	}
	if s.Current < 0 {
		s.Current = 0
	}
	if s.Current > len(s.Queue) {
		s.Current = len(s.Queue)
	}
}

// CurrentTaskID is the task under discussion, or 0 past the end of the queue.
func (s RefinementState) CurrentTaskID() int {
	if s.Current >= 0 && s.Current < len(s.Queue) {
		return s.Queue[s.Current]
	}
	return 0
}

func (s RefinementState) indexOf(taskID int) int {
	for i, id := range s.Queue {
		if id == taskID {
			return i
		}
	}
	return -1
}

// RefinementSession is a session with its decoded state and queued task summaries.
type RefinementSession struct {
	storage.CeremonySession
	State         RefinementState
	Tasks         []storage.RefinementTask // queue order; deleted tasks are dropped
	CanFacilitate bool
	IsFacilitator bool
}

// RefinementSessionSummary is one row of session history.
type RefinementSessionSummary struct {
	storage.CeremonySession
	Queued  int
	Refined int
	Skipped int
}

// CreateRefinementSessionInput starts a session. TaskIDs, when set, replace the automatic queue.
type CreateRefinementSessionInput struct {
	Title            string
	SprintID         *int
	IncludeUnflagged bool
	TaskIDs          []int
}

// RefinementActionInput is one facilitator step. Revision must match the session's current revision.
type RefinementActionInput struct {
	Action   string
	Revision int
	TaskID   int
	TaskIDs  []int
	Index    int
}

func decodeRefinementState(raw json.RawMessage) RefinementState {
	var st RefinementState
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &st)
	}
	st.normalize()
	return st
}

func (a *ceremonyAccess) buildRefinementSession(userID int, s *storage.CeremonySession) (*RefinementSession, error) {
	st := decodeRefinementState(s.State)
	byID, err := storage.ListRefinementTasks(s.ProjectID, st.Queue)
	if err != nil {
		return nil, err
	}
	tasks := make([]storage.RefinementTask, 0, len(st.Queue))
	for _, id := range st.Queue {
		if t, ok := byID[id]; ok {
			tasks = append(tasks, t)
		}
	}
	return &RefinementSession{
		CeremonySession: *s,
		State:           st,
		Tasks:           tasks,
		CanFacilitate:   a.canFacilitate,
		IsFacilitator:   s.FacilitatorID == userID,
	}, nil
}

// GetActiveRefinementSession returns the running session, or (nil, nil) when none is running.
func GetActiveRefinementSession(_ context.Context, userID, projectID int) (*RefinementSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRefinement)
	if err != nil {
		return nil, err
	}
	s, err := storage.GetActiveCeremonySession(projectID, storage.CeremonyRefinement)
	if err != nil {
		if mapped := mapCeremonyStoreErr(err); mapped == ErrNotFound {
			return nil, nil
		}
		return nil, err
	}
	return acc.buildRefinementSession(userID, s)
}

// GetRefinementSession returns one session (active or ended).
func GetRefinementSession(_ context.Context, userID, projectID, sessionID int) (*RefinementSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRefinement)
	if err != nil {
		return nil, err
	}
	s, err := storage.GetCeremonySession(projectID, sessionID)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	if s.Kind != storage.CeremonyRefinement {
		return nil, ErrNotFound
	}
	return acc.buildRefinementSession(userID, s)
}

// ListRefinementSessions returns recent sessions with outcome counts, newest first.
func ListRefinementSessions(_ context.Context, userID, projectID, limit int) ([]RefinementSessionSummary, error) {
	if _, err := requireCeremony(projectID, userID, storage.CeremonyRefinement); err != nil {
		return nil, err
	}
	rows, err := storage.ListCeremonySessions(projectID, storage.CeremonyRefinement, limit)
	if err != nil {
		return nil, err
	}
	out := make([]RefinementSessionSummary, 0, len(rows))
	for _, s := range rows {
		st := decodeRefinementState(s.State)
		sum := RefinementSessionSummary{CeremonySession: s, Queued: len(st.Queue)}
		for _, o := range st.Outcomes {
			switch o {
			case RefinementOutcomeRefined:
				sum.Refined++
			case RefinementOutcomeSkipped:
				sum.Skipped++
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

// StartRefinementSession creates the project's running session with the caller as facilitator.
func StartRefinementSession(ctx context.Context, userID, projectID int, in CreateRefinementSessionInput) (*RefinementSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRefinement)
	if err != nil {
		return nil, err
	}
	if !acc.canFacilitate {
		return nil, fmt.Errorf("%w: your role cannot run refinement sessions", ErrForbidden)
	}
	title, err := normalizeCeremonyTitle(in.Title, "Refinement "+time.Now().UTC().Format("Jan 2"))
	if err != nil {
		return nil, err
	}
	sprintID := 0
	if in.SprintID != nil && *in.SprintID > 0 {
		if _, err := storage.GetProjectSprint(projectID, *in.SprintID); err != nil {
			return nil, fmt.Errorf("%w: invalid sprint_id", ErrValidation)
		}
		sprintID = *in.SprintID
	}
	var queue []int
	if in.TaskIDs != nil {
		queue, err = storage.FilterProjectTaskIDs(projectID, dedupeInts(in.TaskIDs))
	} else {
		queue, err = storage.ListRefinementCandidates(projectID, storage.RefinementCandidateFilter{
			SprintID: in.SprintID, IncludeUnflagged: in.IncludeUnflagged,
		})
	}
	if err != nil {
		return nil, err
	}
	if len(queue) > storage.MaxRefinementQueue {
		queue = queue[:storage.MaxRefinementQueue]
	}
	st := RefinementState{Queue: queue}
	st.normalize()
	raw, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	s, err := storage.CreateCeremonySession(projectID, storage.CeremonyRefinement, title, userID, sprintID, raw)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	live.AfterCeremonyChange(userID, projectID, storage.CeremonyRefinement, s.ID)
	return acc.buildRefinementSession(userID, s)
}

// ApplyRefinementAction runs one facilitator step and broadcasts the new session state.
func ApplyRefinementAction(ctx context.Context, userID, projectID, sessionID int, in RefinementActionInput) (*RefinementSession, error) {
	acc, err := requireCeremony(projectID, userID, storage.CeremonyRefinement)
	if err != nil {
		return nil, err
	}
	if !acc.canFacilitate {
		return nil, fmt.Errorf("%w: your role cannot run refinement sessions", ErrForbidden)
	}
	s, err := storage.GetCeremonySession(projectID, sessionID)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	if s.Kind != storage.CeremonyRefinement {
		return nil, ErrNotFound
	}
	if s.Status != storage.CeremonySessionActive {
		return nil, fmt.Errorf("%w: this session has ended", ErrConflict)
	}
	if in.Revision != s.Revision {
		return nil, mapCeremonyStoreErr(storage.ErrCeremonyRevisionConflict)
	}
	newFacilitator := 0
	if in.Action == RefinementActionTakeOver {
		newFacilitator = userID
	} else if s.FacilitatorID != userID {
		return nil, fmt.Errorf("%w: only the facilitator can drive this session; take over first", ErrForbidden)
	}

	st := decodeRefinementState(s.State)
	end := false
	var flagTask int
	switch in.Action {
	case RefinementActionTakeOver:
	case RefinementActionNext:
		st.Current++
	case RefinementActionPrev:
		st.Current--
	case RefinementActionGoto:
		if in.Index < 0 || in.Index > len(st.Queue) {
			return nil, fmt.Errorf("%w: index out of range", ErrValidation)
		}
		st.Current = in.Index
	case RefinementActionRefine, RefinementActionSkip:
		id := st.CurrentTaskID()
		if in.TaskID > 0 {
			id = in.TaskID
		}
		idx := st.indexOf(id)
		if id == 0 || idx < 0 {
			return nil, fmt.Errorf("%w: task is not in this session", ErrValidation)
		}
		if in.Action == RefinementActionRefine {
			st.Outcomes[id] = RefinementOutcomeRefined
			flagTask = id
		} else {
			st.Outcomes[id] = RefinementOutcomeSkipped
		}
		if idx == st.Current {
			st.Current++
		}
	case RefinementActionAdd:
		ids, err := storage.FilterProjectTaskIDs(projectID, dedupeInts(in.TaskIDs))
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			if st.indexOf(id) < 0 && len(st.Queue) < storage.MaxRefinementQueue {
				st.Queue = append(st.Queue, id)
			}
		}
	case RefinementActionRemove:
		idx := st.indexOf(in.TaskID)
		if idx < 0 {
			return nil, fmt.Errorf("%w: task is not in this session", ErrValidation)
		}
		st.Queue = append(st.Queue[:idx], st.Queue[idx+1:]...)
		delete(st.Outcomes, in.TaskID)
		if idx < st.Current {
			st.Current--
		}
	case RefinementActionEnd:
		end = true
	default:
		return nil, fmt.Errorf("%w: unknown action %q", ErrValidation, in.Action)
	}
	st.normalize()

	if flagTask > 0 {
		if err := markTaskRefined(userID, flagTask); err != nil {
			return nil, err
		}
	}
	raw, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	updated, err := storage.UpdateCeremonySession(projectID, sessionID, s.Revision, raw, newFacilitator, end)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	live.AfterCeremonyChange(userID, projectID, storage.CeremonyRefinement, sessionID)
	return acc.buildRefinementSession(userID, updated)
}

// markTaskRefined flags a task "Refined" on behalf of the facilitator, with activity and live updates.
func markTaskRefined(userID, taskID int) error {
	prev, err := storage.GetTaskRefinementState(taskID)
	if err != nil {
		return err
	}
	if prev == storage.RefinementRefined {
		return nil
	}
	if err := storage.SetTaskRefinementState(taskID, storage.RefinementRefined); err != nil {
		return err
	}
	_ = storage.LogTaskEvent(taskID, userID, "refinement_changed", map[string]interface{}{"from": prev, "to": storage.RefinementRefined})
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskUpdated)
	return nil
}

func dedupeInts(in []int) []int {
	seen := make(map[int]bool, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if v > 0 && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
