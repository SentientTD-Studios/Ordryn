package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/config"
	"GoTodo/internal/domain"
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
)

// ── Manager endpoints: /api/v2/projects/{id}/feedback/... (session or API key) ──

type apiFeedbackBoardJSON struct {
	Exists          bool   `json:"exists"`
	ID              int    `json:"id,omitempty"`
	URL             string `json:"url,omitempty"`
	Title           string `json:"title,omitempty"`
	Intro           string `json:"intro"`
	Enabled         bool   `json:"enabled"`
	SubmissionsOpen bool   `json:"submissions_open"`
	PendingCount    int    `json:"pending_count"`
	CreatedAt       string `json:"created_at,omitempty"`
	UpdatedAt       string `json:"updated_at,omitempty"`
}

type apiFeedbackPostJSON struct {
	ID          int     `json:"id"`
	Title       string  `json:"title"`
	Body        string  `json:"body"`
	AuthorName  string  `json:"author_name"`
	State       string  `json:"state"`
	TaskID      *int    `json:"task_id"`
	Status      string  `json:"status"`
	VoteCount   int     `json:"vote_count"`
	CreatedAt   string  `json:"created_at"`
	ModeratedAt *string `json:"moderated_at,omitempty"`
}

func feedbackBoardURL(r *http.Request, slug string) string {
	return utils.AbsoluteURLForRequest(r, "/feedback/"+slug)
}

func feedbackBoardToJSON(r *http.Request, s *domain.FeedbackBoardSummary) apiFeedbackBoardJSON {
	if s == nil || s.Board == nil {
		return apiFeedbackBoardJSON{Exists: false}
	}
	b := s.Board
	return apiFeedbackBoardJSON{
		Exists:          true,
		ID:              b.ID,
		URL:             feedbackBoardURL(r, b.Slug),
		Title:           b.Title,
		Intro:           b.Intro,
		Enabled:         b.Enabled,
		SubmissionsOpen: b.SubmissionsOpen,
		PendingCount:    s.PendingCount,
		CreatedAt:       formatRFC3339(b.CreatedAt),
		UpdatedAt:       formatRFC3339(b.UpdatedAt),
	}
}

func feedbackPostToJSON(p *storage.FeedbackPost) apiFeedbackPostJSON {
	taskID := p.TaskID
	if p.TaskTrashed {
		taskID = nil
	}
	return apiFeedbackPostJSON{
		ID:          p.ID,
		Title:       p.Title,
		Body:        p.Body,
		AuthorName:  p.AuthorName,
		State:       p.State,
		TaskID:      taskID,
		Status:      domain.PublicFeedbackStatus(p),
		VoteCount:   p.VoteCount,
		CreatedAt:   formatRFC3339(p.CreatedAt),
		ModeratedAt: optionalRFC3339(p.ModeratedAt),
	}
}

// apiV1ProjectFeedback routes /api/v2/projects/{id}/feedback[/...].
func apiV1ProjectFeedback(w http.ResponseWriter, r *http.Request, projectID int, rest []string) {
	userID, ok := apiUserFromRequest(r)
	if !ok {
		utils.APIJSONError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated.")
		return
	}
	ctx := r.Context()

	if len(rest) == 0 {
		switch r.Method {
		case http.MethodGet:
			s, err := domain.GetFeedbackBoardForManager(ctx, userID, projectID)
			if err != nil {
				writeSharingDomainError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, feedbackBoardToJSON(r, s))
		case http.MethodPut, http.MethodPatch:
			var req struct {
				Title           *string `json:"title"`
				Intro           *string `json:"intro"`
				Enabled         *bool   `json:"enabled"`
				SubmissionsOpen *bool   `json:"submissions_open"`
				Password        *string `json:"password"`
			}
			if err := decodeJSONBody(r, &req); err != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
			s, err := domain.SaveFeedbackBoard(ctx, userID, projectID, domain.FeedbackBoardInput{
				Title: req.Title, Intro: req.Intro, Enabled: req.Enabled,
				SubmissionsOpen: req.SubmissionsOpen, Password: req.Password,
			})
			if err != nil {
				writeSharingDomainError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, feedbackBoardToJSON(r, s))
		case http.MethodDelete:
			if err := domain.DeleteFeedbackBoardForUser(ctx, userID, projectID); err != nil {
				writeSharingDomainError(w, err)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		}
		return
	}

	switch rest[0] {
	case "rotate-link":
		if len(rest) != 1 || r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if _, err := domain.RotateFeedbackBoardLink(ctx, userID, projectID); err != nil {
			writeSharingDomainError(w, err)
			return
		}
		s, err := domain.GetFeedbackBoardForManager(ctx, userID, projectID)
		if err != nil {
			writeSharingDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, feedbackBoardToJSON(r, s))
	case "posts":
		apiV1ProjectFeedbackPosts(w, r, userID, projectID, rest[1:])
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

func apiV1ProjectFeedbackPosts(w http.ResponseWriter, r *http.Request, userID, projectID int, rest []string) {
	ctx := r.Context()
	if len(rest) == 0 {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		state := strings.TrimSpace(r.URL.Query().Get("state"))
		if state == "" {
			state = storage.FeedbackStatePending
		}
		posts, err := domain.ListFeedbackPostsForManager(ctx, userID, projectID, state)
		if err != nil {
			writeSharingDomainError(w, err)
			return
		}
		out := make([]apiFeedbackPostJSON, 0, len(posts))
		for i := range posts {
			out = append(out, feedbackPostToJSON(&posts[i]))
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	postID, err := strconv.Atoi(rest[0])
	if err != nil || postID <= 0 {
		utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid post id.")
		return
	}
	if len(rest) == 1 {
		if r.Method != http.MethodDelete {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if err := domain.DeleteFeedbackPostForUser(ctx, userID, projectID, postID); err != nil {
			writeSharingDomainError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if len(rest) != 2 || r.Method != http.MethodPost {
		utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
		return
	}

	var post *storage.FeedbackPost
	switch rest[1] {
	case "approve":
		post, err = domain.ModerateFeedbackPost(ctx, userID, projectID, postID, storage.FeedbackStateApproved)
	case "reject":
		post, err = domain.ModerateFeedbackPost(ctx, userID, projectID, postID, storage.FeedbackStateRejected)
	case "reopen":
		post, err = domain.ModerateFeedbackPost(ctx, userID, projectID, postID, storage.FeedbackStatePending)
	case "promote":
		var req struct {
			StatusID *int `json:"status_id"`
		}
		if r.ContentLength != 0 {
			if derr := decodeJSONBody(r, &req); derr != nil {
				utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
				return
			}
		}
		post, _, err = domain.PromoteFeedbackPost(ctx, userID, projectID, postID, req.StatusID)
	case "link":
		var req struct {
			TaskID int `json:"task_id"`
		}
		if derr := decodeJSONBody(r, &req); derr != nil || req.TaskID <= 0 {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "task_id is required.")
			return
		}
		post, err = domain.LinkFeedbackPostToTask(ctx, userID, projectID, postID, req.TaskID)
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	if err != nil {
		writeSharingDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, feedbackPostToJSON(post))
}

// ── Public endpoints: /api/v2/public/feedback/{slug}[/...] (no account) ──
//
// Kept outside the session/API-key chain on purpose. Every request is rate
// limited per IP; unlock attempts are limited per IP and per board; submits
// per visitor, per IP and per board; votes per visitor. Unknown, disabled and
// archived boards all return the same 404.

type apiPublicFeedbackPostJSON struct {
	ID         int    `json:"id"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	AuthorName string `json:"author_name"`
	Status     string `json:"status"`
	VoteCount  int    `json:"vote_count"`
	Voted      bool   `json:"voted"`
	CreatedAt  string `json:"created_at"`
}

type apiPublicFeedbackPendingJSON struct {
	ID        int    `json:"id"`
	Title     string `json:"title"`
	CreatedAt string `json:"created_at"`
}

type apiPublicFeedbackViewJSON struct {
	Locked          bool                           `json:"locked"`
	Title           string                         `json:"title,omitempty"`
	Intro           string                         `json:"intro,omitempty"`
	SubmissionsOpen bool                           `json:"submissions_open"`
	FormToken       string                         `json:"form_token,omitempty"`
	Posts           []apiPublicFeedbackPostJSON    `json:"posts,omitempty"`
	MyPending       []apiPublicFeedbackPendingJSON `json:"my_pending,omitempty"`
}

func feedbackCookieName(boardID int) string {
	return "ordryn_fb_" + strconv.Itoa(boardID)
}

func feedbackCookiePath() string {
	return strings.TrimSuffix(utils.GetBasePath(), "/") + "/api/"
}

func setFeedbackCookie(w http.ResponseWriter, boardID int, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     feedbackCookieName(boardID),
		Value:    value,
		Path:     feedbackCookiePath(),
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   config.Cfg.UseHTTPS,
		// Strict: the cookie is only ever needed by same-site fetches from the
		// SPA, so it is never sent on cross-site requests (CSRF).
		SameSite: http.SameSiteStrictMode,
	})
}

func readFeedbackCookie(r *http.Request, boardID int) string {
	c, err := r.Cookie(feedbackCookieName(boardID))
	if err != nil {
		return ""
	}
	return c.Value
}

// feedbackAllow applies one token bucket. It fails closed: public write
// endpoints must not run unthrottled if Redis is unavailable.
func feedbackAllow(w http.ResponseWriter, r *http.Request, key string, capacity int, refill float64, ttl int, msg string) bool {
	if utils.RedisClient == nil {
		utils.APIJSONError(w, http.StatusServiceUnavailable, "unavailable", "Feedback boards are temporarily unavailable.")
		return false
	}
	allowed, err := utils.AllowRequest(r.Context(), utils.RedisClient, "rl:tb:fb:"+key, capacity, refill, 1, ttl)
	if err != nil {
		utils.APIJSONError(w, http.StatusServiceUnavailable, "unavailable", "Feedback boards are temporarily unavailable.")
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(utils.RetryAfterSeconds(capacity, refill)))
		utils.APIJSONError(w, http.StatusTooManyRequests, "rate_limit_exceeded", msg)
		return false
	}
	return true
}

// APIV1PublicFeedback serves the password-gated public feedback board.
func APIV1PublicFeedback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")

	ip := utils.KeyByIP(r)
	if !feedbackAllow(w, r, "ip:"+ip, 120, 2.0, 120, "Too many requests. Please wait a moment and try again.") {
		return
	}

	sub := utils.ParseAPIV1Subpath(r, "public/feedback")
	parts := strings.Split(sub, "/")
	if sub == "" || len(parts) > 4 {
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
		return
	}
	board, err := domain.LoadPublicFeedbackBoard(parts[0])
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
		return
	}
	rest := parts[1:]
	now := time.Now()
	visitorID, unlocked := domain.VerifyFeedbackVisitorToken(board, readFeedbackCookie(r, board.ID), now)

	// GET /public/feedback/{slug}
	if len(rest) == 0 {
		if r.Method != http.MethodGet {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		if !unlocked {
			// Reveal nothing about the board until the password is entered.
			writeJSON(w, http.StatusOK, apiPublicFeedbackViewJSON{Locked: true})
			return
		}
		view, err := domain.GetPublicFeedbackView(board, visitorID, now)
		if err != nil {
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Failed to load board.")
			return
		}
		writeJSON(w, http.StatusOK, publicFeedbackViewToJSON(view))
		return
	}

	switch {
	case len(rest) == 1 && rest[0] == "unlock":
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		const tooMany = "Too many password attempts. Please wait a few minutes and try again."
		if !feedbackAllow(w, r, "unlock:ip:"+ip, 8, 1.0/30, 900, tooMany) ||
			!feedbackAllow(w, r, "unlock:board:"+strconv.Itoa(board.ID), 40, 0.2, 900, tooMany) {
			return
		}
		var req struct {
			Password string `json:"password"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		prior := domain.FeedbackVisitorIDFromToken(board.ID, readFeedbackCookie(r, board.ID))
		token, err := domain.UnlockFeedbackBoard(board, req.Password, prior, now)
		if err != nil {
			if errors.Is(err, domain.ErrForbidden) {
				utils.APIJSONError(w, http.StatusUnauthorized, "incorrect_password", "Incorrect password.")
				return
			}
			utils.APIJSONError(w, http.StatusInternalServerError, "internal_error", "Request failed.")
			return
		}
		setFeedbackCookie(w, board.ID, token, int(domain.FeedbackVisitorTTL/time.Second))
		w.WriteHeader(http.StatusNoContent)
		return
	case len(rest) == 1 && rest[0] == "lock":
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		setFeedbackCookie(w, board.ID, "", -1)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if !unlocked {
		utils.APIJSONError(w, http.StatusUnauthorized, "locked", "Enter the board password first.")
		return
	}
	visitorKey := domain.FeedbackVisitorHash(visitorID)[:32]

	switch {
	case len(rest) == 1 && rest[0] == "posts":
		if r.Method != http.MethodPost {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		const slowDown = "You're sending feedback too quickly. Please wait a few minutes and try again."
		if !feedbackAllow(w, r, "post:v:"+visitorKey, 5, 1.0/120, 3600, slowDown) ||
			!feedbackAllow(w, r, "post:ip:"+ip, 10, 1.0/60, 3600, slowDown) ||
			!feedbackAllow(w, r, "post:board:"+strconv.Itoa(board.ID), 60, 0.05, 3600,
				"This board is receiving a lot of feedback right now. Please try again later.") {
			return
		}
		var req struct {
			Title      string `json:"title"`
			Body       string `json:"body"`
			AuthorName string `json:"author_name"`
			Website    string `json:"website"`
			FormToken  string `json:"form_token"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			utils.APIJSONError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body.")
			return
		}
		post, err := domain.SubmitFeedbackPost(board, visitorID, domain.FeedbackSubmission{
			Title: req.Title, Body: req.Body, AuthorName: req.AuthorName,
			Website: req.Website, FormToken: req.FormToken,
		}, now)
		if errors.Is(err, domain.ErrFeedbackHoneypot) {
			// Look like success so bots learn nothing.
			writeJSON(w, http.StatusCreated, map[string]interface{}{"id": 0, "state": storage.FeedbackStatePending})
			return
		}
		if err != nil {
			writeSharingDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{"id": post.ID, "state": post.State})
	case len(rest) == 3 && rest[0] == "posts" && rest[2] == "vote":
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			utils.APIJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed.")
			return
		}
		postID, err := strconv.Atoi(rest[1])
		if err != nil || postID <= 0 {
			utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
			return
		}
		if !feedbackAllow(w, r, "vote:v:"+visitorKey, 30, 0.5, 300, "Too many votes. Please slow down.") {
			return
		}
		voted := r.Method == http.MethodPost
		count, err := domain.VoteFeedbackPost(board, visitorID, postID, voted)
		if err != nil {
			writeSharingDomainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"vote_count": count, "voted": voted})
	default:
		utils.APIJSONError(w, http.StatusNotFound, "not_found", "Not found.")
	}
}

func publicFeedbackViewToJSON(v *domain.PublicFeedbackView) apiPublicFeedbackViewJSON {
	out := apiPublicFeedbackViewJSON{
		Locked:          false,
		Title:           v.Board.Title,
		Intro:           v.Board.Intro,
		SubmissionsOpen: v.Board.SubmissionsOpen,
		Posts:           make([]apiPublicFeedbackPostJSON, 0, len(v.Posts)),
		MyPending:       make([]apiPublicFeedbackPendingJSON, 0, len(v.MyPending)),
	}
	if v.Board.SubmissionsOpen {
		out.FormToken = v.FormToken
	}
	for i := range v.Posts {
		p := &v.Posts[i]
		out.Posts = append(out.Posts, apiPublicFeedbackPostJSON{
			ID:         p.ID,
			Title:      p.Title,
			Body:       p.Body,
			AuthorName: p.AuthorName,
			Status:     domain.PublicFeedbackStatus(p),
			VoteCount:  p.VoteCount,
			Voted:      p.VotedByViewer,
			CreatedAt:  formatRFC3339(p.CreatedAt),
		})
	}
	for _, p := range v.MyPending {
		out.MyPending = append(out.MyPending, apiPublicFeedbackPendingJSON{
			ID: p.ID, Title: p.Title, CreatedAt: formatRFC3339(p.CreatedAt),
		})
	}
	return out
}
