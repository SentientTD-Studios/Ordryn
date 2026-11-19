package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"GoTodo/internal/storage"
	"GoTodo/internal/username"
)

// ChatAccountHourlyLimit caps how many account requests one chat server may
// make per hour, so a misbehaving or compromised chat server can't use Ordryn
// to send a flood of email.
const ChatAccountHourlyLimit = 30

// ErrChatAccountRateLimited means the chat server hit ChatAccountHourlyLimit.
var ErrChatAccountRateLimited = errors.New("too many account requests from this chat server; try again later")

// ChatAccountInput is a chat server's request for an Ordryn account.
type ChatAccountInput struct {
	ChatServerID int
	// ChatUserRef is the chat server's own id for the person. A repeat request
	// with the same ref returns the first one's result and sends no email.
	ChatUserRef string
	Email       string
	// UserName and DisplayName come from the chat account; the username is
	// adjusted to Ordryn's rules and made unique.
	UserName    string
	DisplayName string
}

// ChatAccountResult says what came of a request.
type ChatAccountResult struct {
	Request *storage.ChatAccountRequest
	// New is true when this call made the request, so its email should go out.
	New bool
	// Reset is the welcome (set-password) link for a new account.
	Reset *storage.PasswordReset
}

// CreateChatAccount handles a chat server's account request. If the email is
// free, it creates an account nobody can sign in to until the welcome link is
// used. If the email already has an account, nothing is created; the caller
// tells that address's owner instead, and the chat server learns the person
// should link their existing account.
func CreateChatAccount(in ChatAccountInput) (*ChatAccountResult, error) {
	ref := strings.TrimSpace(in.ChatUserRef)
	if ref == "" || len(ref) > 64 {
		return nil, fmt.Errorf("%w: chat_user_ref must be 1-64 characters", ErrValidation)
	}
	if prev, err := storage.GetChatAccountRequestByRef(in.ChatServerID, ref); err == nil {
		return &ChatAccountResult{Request: prev}, nil
	} else if !errors.Is(err, storage.ErrChatAccountRequestNotFound) {
		return nil, err
	}

	email := strings.TrimSpace(in.Email)
	if len(email) > 254 || !strings.Contains(email, "@") || strings.ContainsAny(email, " \t\r\n") {
		return nil, fmt.Errorf("%w: a valid email address is required", ErrValidation)
	}
	if err := CheckNewAccountEmail(email); err != nil {
		return nil, err
	}

	n, err := storage.CountRecentChatAccountRequests(in.ChatServerID, time.Now().Add(-time.Hour))
	if err != nil {
		return nil, err
	}
	if n >= ChatAccountHourlyLimit {
		return nil, ErrChatAccountRateLimited
	}

	exists, err := storage.UserExistsByEmail(email)
	if err != nil {
		return nil, err
	}
	if exists {
		req, reserved, err := storage.ReserveChatAccountRequest(in.ChatServerID, ref, email, storage.ChatAccountExisting)
		if err != nil {
			return nil, err
		}
		return &ChatAccountResult{Request: req, New: reserved}, nil
	}

	req, reserved, err := storage.ReserveChatAccountRequest(in.ChatServerID, ref, email, storage.ChatAccountCreated)
	if err != nil {
		return nil, err
	}
	if !reserved {
		return &ChatAccountResult{Request: req}, nil // a concurrent retry got there first
	}
	userID, reset, err := createChatAccountUser(email, in.UserName, in.DisplayName)
	if err != nil {
		_ = storage.DeleteChatAccountRequest(req.ID)
		return nil, err
	}
	if err := storage.SetChatAccountRequestUser(req.ID, userID, reset.ID); err != nil {
		return nil, err
	}
	req, err = storage.GetChatAccountRequestByRef(in.ChatServerID, ref)
	if err != nil {
		return nil, err
	}
	return &ChatAccountResult{Request: req, New: true, Reset: reset}, nil
}

func createChatAccountUser(email, rawUserName, displayName string) (int, *storage.PasswordReset, error) {
	name, err := chatAccountUsername(rawUserName)
	if err != nil {
		return 0, nil, err
	}
	// Nobody knows this password; the person chooses theirs from the welcome link.
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return 0, nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(hex.EncodeToString(secret)), bcrypt.DefaultCost)
	if err != nil {
		return 0, nil, err
	}
	roleID, err := storage.GetDefaultRoleID()
	if err != nil {
		return 0, nil, err
	}
	timezone := "America/New_York"
	if settings, err := storage.GetSiteSettings(); err == nil && settings.DefaultTimezone != "" {
		timezone = settings.DefaultTimezone
	}
	// Unverified: it can't sign in until the welcome link confirms the email.
	userID, err := storage.RegisterUnverifiedUser(email, string(hash), timezone, name, roleID)
	if err != nil {
		return 0, nil, err
	}
	if storage.IsCompanyMode() {
		if first, last, ok := strings.Cut(strings.TrimSpace(displayName), " "); ok {
			if f, err1 := normalizePersonName(first, "first name"); err1 == nil {
				if l, err2 := normalizePersonName(strings.TrimSpace(last), "last name"); err2 == nil && f != "" && l != "" {
					_ = storage.SetUserNames(userID, f, l)
				}
			}
		}
	}
	reset, err := storage.GenerateResetTokenTTL(userID, email, SetPasswordLinkTTL)
	if err != nil {
		return 0, nil, err
	}
	return userID, reset, nil
}

// chatAccountUsername turns a chat username into a free Ordryn one: chat
// allows dots and dashes, Ordryn only letters, numbers and underscores.
func chatAccountUsername(raw string) (string, error) {
	base := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			return r
		case r == '.' || r == '-':
			return '_'
		}
		return -1
	}, username.Normalize(raw))
	for utf8.RuneCountInString(base) < username.MinLength {
		base += "_"
	}
	if len(base) > username.MaxLength-3 {
		base = base[:username.MaxLength-3]
	}
	for i := 1; i <= 50; i++ {
		name := base
		if i > 1 {
			name = base + strconv.Itoa(i)
		}
		if !username.ValidFormat(name) {
			continue
		}
		taken, err := storage.UsernameTaken(name, 0)
		if err != nil {
			return "", err
		}
		if !taken {
			return name, nil
		}
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "chat_" + hex.EncodeToString(b), nil
}
