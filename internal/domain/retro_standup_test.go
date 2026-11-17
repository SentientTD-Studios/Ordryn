package domain

import (
	"context"
	"errors"
	"testing"

	"GoTodo/internal/storage"
)

// ceremonyBoard is a kanban project owned by user 1 with kind on, plus an editor member who
// cannot facilitate. It returns the project and editor ids.
func ceremonyBoard(t *testing.T, name, kind string) (projectID, editorID int) {
	t.Helper()
	ctx := context.Background()
	proj, err := CreateProject(ctx, 1, name, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := SetProjectWorkflowMode(ctx, 1, proj.ID, storage.WorkflowKanban); err != nil {
		t.Fatalf("kanban: %v", err)
	}
	if err := SetSiteCeremonies(map[string]bool{kind: true}); err != nil {
		t.Fatalf("site on: %v", err)
	}
	t.Cleanup(func() { _ = SetSiteCeremonies(map[string]bool{kind: false}) })
	if _, err := UpdateCeremonySettings(ctx, 1, proj.ID, UpdateCeremonySettingsInput{Ceremonies: map[string]bool{kind: true}}); err != nil {
		t.Fatalf("project opt-in: %v", err)
	}
	ensureTestProjectRole(t, proj.ID, testRoleEditor)
	editorID = insertUserWithRole(t, name+"-editor@example.com", storage.MemberRoleName)
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer storage.CloseDatabase(pool)
	if _, err := pool.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, $3)`,
		proj.ID, editorID, testRoleEditor); err != nil {
		t.Fatalf("add editor: %v", err)
	}
	return proj.ID, editorID
}

func cardByBody(s *RetroSession, body string) *RetroCard {
	for i := range s.Cards {
		if s.Cards[i].Body == body {
			return &s.Cards[i]
		}
	}
	return nil
}

func TestRetroSettingsValidation(t *testing.T) {
	ctx := context.Background()
	pid, _ := ceremonyBoard(t, "Retro settings", storage.CeremonyRetro)
	if _, err := UpdateCeremonySettings(ctx, 1, pid, UpdateCeremonySettingsInput{Retro: &RetroConfig{Template: "nope"}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad template err=%v", err)
	}
	if _, err := UpdateCeremonySettings(ctx, 1, pid, UpdateCeremonySettingsInput{Retro: &RetroConfig{Template: RetroTemplateFourLs, MaxVotes: 99}}); !errors.Is(err, ErrValidation) {
		t.Fatalf("too many votes err=%v", err)
	}
	got, err := UpdateCeremonySettings(ctx, 1, pid, UpdateCeremonySettingsInput{
		Retro:   &RetroConfig{Template: RetroTemplateFourLs, MaxVotes: 2, Anonymous: true},
		Standup: &StandupConfig{SpeakerSeconds: 90, Shuffle: true},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if got.Retro.Template != RetroTemplateFourLs || got.Retro.MaxVotes != 2 || !got.Retro.Anonymous || got.Standup.SpeakerSeconds != 90 || !got.Standup.Shuffle {
		t.Fatalf("settings=%+v / %+v", got.Retro, got.Standup)
	}
	// Opt-in survives a settings write and vice versa.
	if !got.Project[storage.CeremonyRetro] {
		t.Fatal("retro opt-in lost after saving settings")
	}
}

func TestRetroSessionFlow(t *testing.T) {
	ctx := context.Background()
	pid, editor := ceremonyBoard(t, "Retro flow", storage.CeremonyRetro)
	two, anon := 2, true

	if _, err := StartRetroSession(ctx, editor, pid, StartRetroInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor start err=%v", err)
	}
	s, err := StartRetroSession(ctx, 1, pid, StartRetroInput{Template: RetroTemplateStartStopContinue, MaxVotes: &two, Anonymous: &anon})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if s.State.Phase != RetroPhaseCollect || len(s.State.Columns) != 3 || s.State.Columns[0].Key != "start" {
		t.Fatalf("state=%+v", s.State)
	}
	card := func(user int, col, body string) {
		t.Helper()
		if _, err := AddRetroItem(ctx, user, pid, s.ID, RetroItemInput{Type: storage.CeremonyItemCard, Column: col, Body: body}); err != nil {
			t.Fatalf("add %q: %v", body, err)
		}
	}
	card(1, "start", "Pairing")
	card(editor, "stop", "Late deploys")
	card(editor, "stop", "Friday releases")
	if _, err := AddRetroItem(ctx, 1, pid, s.ID, RetroItemInput{Type: storage.CeremonyItemCard, Column: "nope", Body: "x"}); !errors.Is(err, ErrValidation) {
		t.Fatalf("bad column err=%v", err)
	}

	// While collecting, others' cards are hidden; your own are not.
	mine, err := GetActiveRetroSession(ctx, 1, pid)
	if err != nil {
		t.Fatal(err)
	}
	hidden := 0
	for _, c := range mine.Cards {
		if c.Hidden {
			hidden++
			if c.Body != "" || c.AuthorName != "" {
				t.Fatalf("hidden card leaked %+v", c)
			}
		}
	}
	if hidden != 2 || cardByBody(mine, "Pairing") == nil {
		t.Fatalf("owner sees hidden=%d cards=%+v", hidden, mine.Cards)
	}

	s, err = ApplyRetroAction(ctx, 1, pid, s.ID, RetroActionInput{Action: RetroActionPhase, Phase: RetroPhaseGroup, Revision: mine.Revision})
	if err != nil {
		t.Fatalf("group phase: %v", err)
	}
	late, friday := cardByBody(s, "Late deploys"), cardByBody(s, "Friday releases")
	if late == nil || friday == nil || late.AuthorName != "" {
		t.Fatalf("revealed anonymous cards: late=%+v friday=%+v", late, friday)
	}
	// Only the author edits text.
	txt := "Edited"
	if _, err := UpdateRetroItem(ctx, 1, pid, s.ID, late.ID, RetroItemPatch{Body: &txt}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("non-author edit err=%v", err)
	}
	// Anyone can group.
	if s, err = UpdateRetroItem(ctx, 1, pid, s.ID, friday.ID, RetroItemPatch{SetParent: true, ParentID: late.ID}); err != nil {
		t.Fatalf("group: %v", err)
	}
	if c := cardByBody(s, "Friday releases"); c.ParentID != late.ID {
		t.Fatalf("grouped card parent=%d", c.ParentID)
	}
	pairing := cardByBody(s, "Pairing")
	// Stacking a group's top card under another merges the groups.
	if s, err = UpdateRetroItem(ctx, editor, pid, s.ID, late.ID, RetroItemPatch{SetParent: true, ParentID: pairing.ID}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if c := cardByBody(s, "Friday releases"); c.ParentID != pairing.ID || c.Column != "start" {
		t.Fatalf("merged child=%+v", c)
	}

	s, err = ApplyRetroAction(ctx, 1, pid, s.ID, RetroActionInput{Action: RetroActionPhase, Phase: RetroPhaseVote, Revision: s.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VoteRetroCard(ctx, editor, pid, s.ID, late.ID, true); !errors.Is(err, ErrValidation) {
		t.Fatalf("vote on grouped card err=%v", err)
	}
	if _, err := VoteRetroCard(ctx, editor, pid, s.ID, pairing.ID, true); err != nil {
		t.Fatalf("vote: %v", err)
	}
	if _, err := AddRetroItem(ctx, 1, pid, s.ID, RetroItemInput{Type: storage.CeremonyItemCard, Column: "continue", Body: "Too late"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("card during vote err=%v", err)
	}

	// Vote limit: the editor has 2 votes; a re-vote on the same card does not count twice.
	v, err := VoteRetroCard(ctx, editor, pid, s.ID, pairing.ID, true)
	if err != nil || v.VotesUsed != 1 {
		t.Fatalf("re-vote used=%v err=%v", v, err)
	}
	s, err = ApplyRetroAction(ctx, 1, pid, s.ID, RetroActionInput{Action: RetroActionPhase, Phase: RetroPhaseGroup, Revision: s.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if s, err = UpdateRetroItem(ctx, 1, pid, s.ID, late.ID, RetroItemPatch{SetParent: true, ParentID: 0}); err != nil {
		t.Fatalf("ungroup: %v", err)
	}
	card(1, "continue", "Demo days") // cards can still be added while grouping
	s, err = ApplyRetroAction(ctx, 1, pid, s.ID, RetroActionInput{Action: RetroActionPhase, Phase: RetroPhaseVote, Revision: s.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VoteRetroCard(ctx, editor, pid, s.ID, late.ID, true); err != nil {
		t.Fatalf("second vote: %v", err)
	}
	// A third vote, on another top-level card, exceeds the limit of 2; taking one back frees it.
	demo := cardByBody(s, "Demo days")
	if _, err := VoteRetroCard(ctx, editor, pid, s.ID, demo.ID, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("third vote err=%v want conflict", err)
	}
	if _, err := VoteRetroCard(ctx, editor, pid, s.ID, late.ID, false); err != nil {
		t.Fatalf("unvote: %v", err)
	}
	if _, err := VoteRetroCard(ctx, editor, pid, s.ID, demo.ID, true); err != nil {
		t.Fatalf("vote after unvote: %v", err)
	}

	s, err = ApplyRetroAction(ctx, 1, pid, s.ID, RetroActionInput{Action: RetroActionPhase, Phase: RetroPhaseAct, Revision: s.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if c := cardByBody(s, "Pairing"); c.Votes != 1 {
		t.Fatalf("act phase votes=%d", c.Votes)
	}
	if s, err = AddRetroItem(ctx, editor, pid, s.ID, RetroItemInput{Type: storage.CeremonyItemAction, Body: "Freeze deploys after 3pm", CardID: late.ID}); err != nil {
		t.Fatalf("action: %v", err)
	}
	actionID := s.Actions[0].ID
	if s, err = CreateTaskFromRetroAction(ctx, 1, pid, s.ID, actionID); err != nil {
		t.Fatalf("task from action: %v", err)
	}
	if s.Actions[0].TaskID == 0 || s.Actions[0].TaskTitle != "Freeze deploys after 3pm" {
		t.Fatalf("action=%+v", s.Actions[0])
	}
	if _, err := CreateTaskFromRetroAction(ctx, 1, pid, s.ID, actionID); !errors.Is(err, ErrConflict) {
		t.Fatalf("second task err=%v", err)
	}

	s, err = ApplyRetroAction(ctx, 1, pid, s.ID, RetroActionInput{Action: RetroActionEnd, Revision: s.Revision})
	if err != nil || s.Status != storage.CeremonySessionEnded {
		t.Fatalf("end: %v", err)
	}
	if _, err := AddRetroItem(ctx, 1, pid, s.ID, RetroItemInput{Type: storage.CeremonyItemAction, Body: "late"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("add after end err=%v", err)
	}
	hist, err := ListRetroSessions(ctx, 1, pid, 5)
	if err != nil || len(hist) != 1 || hist[0].Counts[storage.CeremonyItemCard] != 4 || hist[0].Counts[storage.CeremonyItemAction] != 1 {
		t.Fatalf("history=%+v err=%v", hist, err)
	}
}

func TestStandupSessionFlow(t *testing.T) {
	ctx := context.Background()
	pid, editor := ceremonyBoard(t, "Standup flow", storage.CeremonyStandup)
	outsider := insertUserWithRole(t, "standup-outsider@example.com", storage.MemberRoleName)

	if _, err := StartStandupSession(ctx, editor, pid, StartStandupInput{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor start err=%v", err)
	}
	zero := 0
	s, err := StartStandupSession(ctx, 1, pid, StartStandupInput{SpeakerSeconds: &zero})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	if len(s.State.Participants) != 2 || s.State.SpeakerSeconds != 0 || !s.Joined {
		t.Fatalf("participants=%v secs=%d joined=%v", s.State.Participants, s.State.SpeakerSeconds, s.Joined)
	}

	// Claimed open work shows on a person's row.
	claimed := mustCreateTask(t, 1, CreateTaskInput{Title: "Ship standups", ProjectID: &pid})
	if err := ClaimTaskForUser(ctx, editor, claimed); err != nil {
		t.Fatalf("claim: %v", err)
	}

	if _, err := SaveStandupEntry(ctx, outsider, pid, s.ID, StandupEntryData{Done: "x"}); err == nil {
		t.Fatal("outsider wrote an entry")
	}
	blocker := mustCreateTask(t, 1, CreateTaskInput{Title: "Waiting on API keys", ProjectID: &pid})
	s, err = SaveStandupEntry(ctx, editor, pid, s.ID, StandupEntryData{
		Done: " Wired the board ", Today: "Tests", Blockers: "API keys", BlockerTaskIDs: []int{blocker, blocker, 999999},
	})
	if err != nil {
		t.Fatalf("entry: %v", err)
	}
	var row *StandupParticipant
	for i := range s.Participants {
		if s.Participants[i].UserID == editor {
			row = &s.Participants[i]
		}
	}
	if row == nil || row.Entry == nil || row.Entry.Done != "Wired the board" || len(row.BlockerTasks) != 1 || len(row.Claimed) != 1 {
		t.Fatalf("editor row=%+v", row)
	}
	if s, err = CreateStandupBlockerTask(ctx, editor, pid, s.ID, "Ask ops for keys"); err != nil {
		t.Fatalf("blocker task: %v", err)
	}
	for _, p := range s.Participants {
		if p.UserID == editor && len(p.BlockerTasks) != 2 {
			t.Fatalf("blocker tasks=%d", len(p.BlockerTasks))
		}
	}

	if _, err := ApplyStandupAction(ctx, editor, pid, s.ID, StandupActionInput{Action: StandupActionNext, Revision: s.Revision}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("editor next err=%v", err)
	}
	first := s.State.CurrentSpeaker()
	s, err = ApplyStandupAction(ctx, 1, pid, s.ID, StandupActionInput{Action: StandupActionNext, Revision: s.Revision})
	if err != nil || s.State.CurrentSpeaker() == first || s.State.Current != 1 {
		t.Fatalf("next: current=%d err=%v", s.State.Current, err)
	}
	if _, err := ApplyStandupAction(ctx, 1, pid, s.ID, StandupActionInput{Action: StandupActionAdd, UserID: outsider, Revision: s.Revision}); !errors.Is(err, ErrValidation) {
		t.Fatalf("add non-member err=%v", err)
	}
	s, err = ApplyStandupAction(ctx, 1, pid, s.ID, StandupActionInput{Action: StandupActionNext, Revision: s.Revision})
	if err != nil || s.State.CurrentSpeaker() != 0 {
		t.Fatalf("past the end: %+v err=%v", s.State, err)
	}
	s, err = ApplyStandupAction(ctx, 1, pid, s.ID, StandupActionInput{Action: StandupActionRemove, UserID: editor, Revision: s.Revision})
	if err != nil || len(s.State.Participants) != 1 || s.State.Current != 1 {
		t.Fatalf("remove: %+v err=%v", s.State, err)
	}
	// The editor can rejoin by themselves.
	if s, err = ApplyStandupAction(ctx, editor, pid, s.ID, StandupActionInput{Action: StandupActionJoin}); err != nil || !s.Joined {
		t.Fatalf("join: %v", err)
	}
	if s.State.CurrentSpeaker() != editor {
		t.Fatalf("joining after everyone spoke should make them the speaker, got %d", s.State.CurrentSpeaker())
	}

	s, err = ApplyStandupAction(ctx, 1, pid, s.ID, StandupActionInput{Action: StandupActionEnd, Revision: s.Revision})
	if err != nil || s.Status != storage.CeremonySessionEnded {
		t.Fatalf("end: %v", err)
	}
	hist, err := ListStandupSessions(ctx, 1, pid, 5)
	if err != nil || len(hist) != 1 || hist[0].Counts[storage.CeremonyItemEntry] != 1 || hist[0].Counts["blocked"] != 1 {
		t.Fatalf("history=%+v err=%v", hist, err)
	}
}
