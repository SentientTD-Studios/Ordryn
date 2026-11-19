package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"GoTodo/internal/domain"
	"GoTodo/internal/mailer"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// Ordryn accounts for people who sign up on a chat server.
//
//	POST /api/v2/chat/accounts         {chat_user_ref, email, user_name, display_name} → request
//	GET  /api/v2/chat/accounts?ids=1,2 → {accounts: [request…]}
//
// The chat server authenticates with HTTP Basic (client ID : client secret)
// and needs "Account creation" allowed under Admin → Chat servers.
//
// A new account can't be used until its owner chooses a password from the
// welcome email, which names the chat server and chat username. Only that
// link turns the request "ready"; the chat server then links the chat account
// to user_id. If the email already has an Ordryn account, nothing is created:
// the request is "email_in_use", its owner gets an email saying so, and the
// person links their existing account from chat themselves.

type apiChatAccountJSON struct {
	ID          int    `json:"id"`
	ChatUserRef string `json:"chat_user_ref"`
	// Status: pending, ready, email_in_use, expired or gone.
	Status string `json:"status"`
	// UserID is set once the request is ready.
	UserID int `json:"user_id,omitempty"`
}

func chatAccountJSON(r *storage.ChatAccountRequest) apiChatAccountJSON {
	out := apiChatAccountJSON{ID: r.ID, ChatUserRef: r.ChatUserRef, Status: r.Status(domain.SetPasswordLinkTTL)}
	if out.Status == storage.ChatAccountStatusReady && r.UserID != nil {
		out.UserID = *r.UserID
	}
	return out
}

// chatServerForAccounts authenticates the chat server and requires account creation.
func chatServerForAccounts(w http.ResponseWriter, r *http.Request) (*storage.ChatServer, bool) {
	clientID, secret, ok := r.BasicAuth()
	if !ok {
		w.Header().Set("WWW-Authenticate", `Basic realm="Ordryn Chat"`)
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_client", "Send the chat server's client ID and secret (HTTP Basic).")
		return nil, false
	}
	cs, err := storage.AuthenticateChatServer(clientID, secret)
	if errors.Is(err, storage.ErrChatClientRejected) {
		utils.APIJSONError(w, http.StatusUnauthorized, "invalid_client",
			"This chat server isn't allowed to use Ordryn. An Ordryn admin can add it under Admin → Chat servers.")
		return nil, false
	}
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to check the chat server.")
		return nil, false
	}
	if !cs.AccountCreate {
		utils.APIJSONError(w, http.StatusForbidden, "account_create_disabled",
			"Account creation isn't allowed for this chat server. An Ordryn admin can turn it on under Admin → Chat servers.")
		return nil, false
	}
	return cs, true
}

// APIV2ChatAccounts serves /api/v2/chat/accounts.
func APIV2ChatAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		apiV2ChatCreateAccount(w, r)
	case http.MethodGet:
		apiV2ChatListAccounts(w, r)
	default:
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
	}
}

type apiChatCreateAccountRequest struct {
	ChatUserRef string `json:"chat_user_ref"`
	Email       string `json:"email"`
	UserName    string `json:"user_name"`
	DisplayName string `json:"display_name"`
}

func apiV2ChatCreateAccount(w http.ResponseWriter, r *http.Request) {
	cs, ok := chatServerForAccounts(w, r)
	if !ok {
		return
	}
	var req apiChatCreateAccountRequest
	if err := decodeJSONBody(r, &req); err != nil {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
		return
	}
	// Every outcome is told by email, so don't start without working email.
	settings, err := storage.GetSiteSettings()
	if err != nil || settings == nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load site settings.")
		return
	}
	mailCfg := storage.SiteEmailConfig(settings)
	if !mailer.Configured(mailCfg) {
		utils.APIJSONError(w, http.StatusServiceUnavailable, "email_unavailable",
			"Ordryn can't send email, so it can't create accounts for chat sign-ups. An Ordryn admin needs to set up email.")
		return
	}

	res, err := domain.CreateChatAccount(domain.ChatAccountInput{
		ChatServerID: cs.ID, ChatUserRef: req.ChatUserRef, Email: req.Email,
		UserName: req.UserName, DisplayName: req.DisplayName,
	})
	switch {
	case errors.Is(err, domain.ErrChatAccountRateLimited):
		utils.APIJSONError(w, http.StatusTooManyRequests, "rate_limited", err.Error())
		return
	case errors.Is(err, domain.ErrValidation):
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", sharingClientMessage(err, "Invalid request."))
		return
	case err != nil:
		fmt.Printf("APIV2ChatAccounts: %v\n", err)
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to create the account.")
		return
	}

	if res.New {
		siteName := strings.TrimSpace(settings.SiteName)
		if siteName == "" {
			siteName = "Ordryn"
		}
		chatUser := chatEmailText(req.UserName, 64)
		switch res.Request.Outcome {
		case storage.ChatAccountCreated:
			uid := 0
			if res.Request.UserID != nil {
				uid = *res.Request.UserID
			}
			logAdminEvent(r, "user_created", "user", int64(uid), storage.UserAuditLabel(uid),
				map[string]interface{}{"via": "chat_server", "chat_server_id": cs.ID, "chat_server": cs.Name})
			if res.Reset != nil {
				link := utils.AbsoluteURLForRequest(r,
					fmt.Sprintf("/reset-password?token=%s&id=%s", res.Reset.Token, res.Reset.ID))
				subject := fmt.Sprintf("%s - Finish creating your account", siteName)
				body := fmt.Sprintf(`Hello,

Someone, hopefully you, signed up on the chat server %s (%s) as @%s and asked for an account on %s with this email address.

To finish, choose your password here:

%s

Doing so also links your %s account to that chat account.

If you didn't sign up, ignore this email. Nobody can use the account without this link. It expires in %d days.
`, chatEmailText(cs.Name, 60), cs.URL, chatUser, siteName, link, siteName, int(domain.SetPasswordLinkTTL.Hours()/24))
				if err := mailer.SendEmail(mailCfg, mailer.TriggerSiteInvite, subject, body, res.Request.Email); err != nil {
					// The account exists; "Forgot password" still lets its owner in,
					// but only the welcome link links it to chat.
					fmt.Printf("APIV2ChatAccounts: welcome email: %v\n", err)
				}
			}
		case storage.ChatAccountExisting:
			subject := fmt.Sprintf("%s - Someone signed up on a chat server with your email", siteName)
			body := fmt.Sprintf(`Hello,

Someone signed up on the chat server %s (%s) as @%s and asked for an account on %s with this email address. You already have an account, so no new one was made.

If that was you, sign in to the chat server and choose "Link Ordryn account" in your profile to connect the two.

If it wasn't you, you can ignore this email. Your account hasn't changed.
`, chatEmailText(cs.Name, 60), cs.URL, chatUser, siteName)
			if err := mailer.SendEmail(mailCfg, mailer.TriggerSignupExisting, subject, body, res.Request.Email); err != nil {
				fmt.Printf("APIV2ChatAccounts: existing-account email: %v\n", err)
			}
		}
	}

	status := http.StatusOK
	if res.New {
		status = http.StatusCreated
	}
	writeJSONStatus(w, status, chatAccountJSON(res.Request))
}

func apiV2ChatListAccounts(w http.ResponseWriter, r *http.Request) {
	cs, ok := chatServerForAccounts(w, r)
	if !ok {
		return
	}
	var ids []int
	for _, part := range strings.Split(r.URL.Query().Get("ids"), ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		id, err := strconv.Atoi(part)
		if err != nil || id <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "ids must be a comma-separated list of request ids.")
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > 200 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Send 1-200 request ids in ids.")
		return
	}
	reqs, err := storage.ListChatAccountRequests(cs.ID, ids)
	if err != nil {
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load account requests.")
		return
	}
	out := struct {
		Accounts []apiChatAccountJSON `json:"accounts"`
	}{Accounts: make([]apiChatAccountJSON, 0, len(reqs))}
	for i := range reqs {
		out.Accounts = append(out.Accounts, chatAccountJSON(&reqs[i]))
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// chatEmailText makes text from a chat server safe to put in a plain-text
// email: one line, at most n characters.
func chatEmailText(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		s = string(r[:n]) + "…"
	}
	return s
}
