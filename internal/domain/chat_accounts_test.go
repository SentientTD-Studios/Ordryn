package domain

import (
	"context"
	"testing"

	"GoTodo/internal/storage"
)

// The domain and storage behind POST/GET /api/v2/chat/accounts.
func TestChatAccounts(t *testing.T) {
	useFreshDatabase(t, "gotodo_chat_accounts_test")
	t.Cleanup(clearSetupToken)
	token, err := IssueSetupToken()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := CompleteSetup(context.Background(), SetupInput{
		Token: token, Email: "owner@example.com", UserName: "owner", Password: "password1",
		ConfirmPassword: "password1", Timezone: "UTC", SiteMode: storage.SiteModeMulti,
	}, true)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	cs, _, err := storage.CreateChatServer("Accounts chat", "https://chat-accounts.example.com", false, false, false, true, owner)
	if err != nil {
		t.Fatal(err)
	}
	if !cs.AccountCreate {
		t.Fatal("account_create not stored")
	}

	res, err := CreateChatAccount(ChatAccountInput{
		ChatServerID: cs.ID, ChatUserRef: "41", Email: "New.Chatter@example.com", UserName: "new.chatter", DisplayName: "New Chatter",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.New || res.Reset == nil || res.Request.UserID == nil || res.Request.Outcome != storage.ChatAccountCreated {
		t.Fatalf("first request: %+v", res)
	}
	if got := res.Request.Status(SetPasswordLinkTTL); got != storage.ChatAccountStatusPending {
		t.Fatalf("status = %s, want pending", got)
	}
	uid := *res.Request.UserID
	if p, err := storage.GetUserProfileByID(uid); err != nil || p.UserName != "new_chatter" {
		t.Fatalf("username: %+v %v", p, err)
	}

	// A retry returns the same request without making anything or emailing again.
	again, err := CreateChatAccount(ChatAccountInput{ChatServerID: cs.ID, ChatUserRef: "41", Email: "other@example.com", UserName: "x"})
	if err != nil || again.New || again.Request.ID != res.Request.ID {
		t.Fatalf("retry: %+v %v", again, err)
	}

	// Another reset link (e.g. Forgot password) doesn't confirm the request...
	other, err := storage.GenerateResetTokenTTL(uid, "new.chatter@example.com", SetPasswordLinkTTL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ResetPasswordWithToken(other.ID, other.Token, "hash"); err != nil {
		t.Fatal(err)
	}
	reqs, err := storage.ListChatAccountRequests(cs.ID, []int{res.Request.ID})
	if err != nil || len(reqs) != 1 {
		t.Fatalf("list: %v %v", reqs, err)
	}
	if got := reqs[0].Status(SetPasswordLinkTTL); got != storage.ChatAccountStatusPending {
		t.Fatalf("after another reset: %s, want pending", got)
	}

	// ...only the welcome link does. (The other reset replaced it, so make the
	// request point at a fresh one, as if that had been the welcome link.)
	welcome, err := storage.GenerateResetTokenTTL(uid, "new.chatter@example.com", SetPasswordLinkTTL)
	if err != nil {
		t.Fatal(err)
	}
	if err := storage.SetChatAccountRequestUser(res.Request.ID, uid, welcome.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := storage.ResetPasswordWithToken(welcome.ID, welcome.Token, "hash2"); err != nil {
		t.Fatal(err)
	}
	reqs, _ = storage.ListChatAccountRequests(cs.ID, []int{res.Request.ID})
	if got := reqs[0].Status(SetPasswordLinkTTL); got != storage.ChatAccountStatusReady {
		t.Fatalf("after welcome link: %s, want ready", got)
	}

	// An email that already has an account creates nothing.
	dup, err := CreateChatAccount(ChatAccountInput{ChatServerID: cs.ID, ChatUserRef: "42", Email: "NEW.CHATTER@example.com", UserName: "someone"})
	if err != nil {
		t.Fatal(err)
	}
	if !dup.New || dup.Reset != nil || dup.Request.UserID != nil ||
		dup.Request.Status(SetPasswordLinkTTL) != storage.ChatAccountStatusEmailInUse {
		t.Fatalf("existing email: %+v", dup.Request)
	}

	// Another chat server can't see these requests.
	cs2, _, err := storage.CreateChatServer("Other accounts chat", "https://chat-accounts2.example.com", false, false, false, true, owner)
	if err != nil {
		t.Fatal(err)
	}
	if reqs, err := storage.ListChatAccountRequests(cs2.ID, []int{res.Request.ID, dup.Request.ID}); err != nil || len(reqs) != 0 {
		t.Fatalf("other server sees %v %v", reqs, err)
	}

	if _, err := CreateChatAccount(ChatAccountInput{ChatServerID: cs.ID, ChatUserRef: "43", Email: "not-an-email"}); err == nil {
		t.Fatal("bad email accepted")
	}
}
