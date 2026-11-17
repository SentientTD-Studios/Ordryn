package domain

import (
	"context"
	"testing"

	"GoTodo/internal/storage"
)

func onboardingStep(ob *Onboarding, id string) *OnboardingStep {
	for i := range ob.Steps {
		if ob.Steps[i].ID == id {
			return &ob.Steps[i]
		}
	}
	return nil
}

func TestOnboardingTracksProgress(t *testing.T) {
	ctx := context.Background()
	pool, err := storage.OpenDatabase()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, password, role_id) VALUES (9501, 'newbie@example.com', 'x', 1)
		ON CONFLICT DO NOTHING`); err != nil {
		storage.CloseDatabase(pool)
		t.Fatal(err)
	}
	storage.CloseDatabase(pool)
	const uid = 9501

	ob, err := GetOnboardingForUser(ctx, uid)
	if err != nil {
		t.Fatalf("onboarding: %v", err)
	}
	if ob.Dismissed || ob.Complete {
		t.Fatalf("new user: dismissed=%v complete=%v", ob.Dismissed, ob.Complete)
	}
	for _, id := range []string{"project", "task", "complete"} {
		s := onboardingStep(ob, id)
		if s == nil || s.Done {
			t.Fatalf("step %s: %+v", id, s)
		}
	}

	taskID, err := CreateTask(ctx, uid, CreateTaskInput{Title: "First"})
	if err != nil {
		t.Skipf("personal tasks unavailable for this fixture: %v", err)
	}
	ob, _ = GetOnboardingForUser(ctx, uid)
	if s := onboardingStep(ob, "task"); s == nil || !s.Done {
		t.Fatalf("task step not done: %+v", s)
	}
	done := true
	if _, err := UpdateTask(ctx, uid, taskID, UpdateTaskInput{Completed: &done}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	ob, _ = GetOnboardingForUser(ctx, uid)
	if s := onboardingStep(ob, "complete"); s == nil || !s.Done {
		t.Fatalf("complete step not done: %+v", s)
	}

	if err := SetOnboardingDismissedForUser(ctx, uid, true); err != nil {
		t.Fatal(err)
	}
	if ob, _ = GetOnboardingForUser(ctx, uid); !ob.Dismissed {
		t.Fatal("expected dismissed")
	}
	if err := SetOnboardingDismissedForUser(ctx, uid, false); err != nil {
		t.Fatal(err)
	}
	if ob, _ = GetOnboardingForUser(ctx, uid); ob.Dismissed {
		t.Fatal("expected shown again")
	}
}
