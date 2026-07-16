package handlers

import (
	"GoTodo/internal/server/utils"
	"GoTodo/internal/storage"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
)

// Invite represents an invite in the system
type Invite struct {
	ID         int
	Email      string
	Token      string
	InviteUsed int
	IsBanned   bool
}

// CreateInvitePageHandler renders the create invite page
func CreateInvitePageHandler(w http.ResponseWriter, r *http.Request) {
	email, _, permissions, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		http.Redirect(w, r, "/", http.StatusUnauthorized)
		return
	}

	// Check if user has createinvites permission
	hasPermission := false
	for _, p := range permissions {
		if p == "createinvites" {
			hasPermission = true
			break
		}
	}

	if !hasPermission {
		http.Error(w, "Forbidden: You don't have permission to access this resource", http.StatusForbidden)
		return
	}

	// Fetch all invites
	pool, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Error opening database", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(pool)

	err = storage.MigrateInvitesTable()
	if err != nil {
		fmt.Printf("Warning: Could not migrate invites table: %v\n", err)
	}
	// Ensure users table has is_banned column for future ban/unban support
	if err := storage.MigrateUsersAddIsBanned(); err != nil {
		fmt.Printf("Warning: Could not migrate users table (is_banned): %v\n", err)
	}

	rows, err := pool.Query(context.Background(), "SELECT id, email, token, inviteused FROM invites ORDER BY id DESC")
	if err != nil {
		http.Error(w, fmt.Sprintf("Error fetching invites: %v", err), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var invites []Invite
	for rows.Next() {
		var invite Invite
		err := rows.Scan(&invite.ID, &invite.Email, &invite.Token, &invite.InviteUsed)
		if err != nil {
			continue
		}
		// If a user exists with this email, consider the invite used; also check ban status
		var uid int
		errUser := pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", invite.Email).Scan(&uid)
		if errUser == nil {
			invite.InviteUsed = 1
			// populate ban status
			var isB bool
			err = pool.QueryRow(context.Background(), "SELECT is_banned FROM users WHERE email = $1", invite.Email).Scan(&isB)
			if err == nil {
				invite.IsBanned = isB
			} else {
				invite.IsBanned = false
			}
		} else {
			// no user found, mark as not used and ensure ban false
			invite.IsBanned = false
		}
		invites = append(invites, invite)
	}

	context := map[string]interface{}{
		"LoggedIn":  loggedIn,
		"UserEmail": email,
		"Invites":   invites,
	}

	utils.RenderTemplate(w, r, "createinvite.html", context)
}

// APIConfirmDeleteInvite shows the confirmation modal for deleting an invite
func APIConfirmDeleteInvite(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "Invite ID is required", http.StatusBadRequest)
		return
	}

	inviteID, err := strconv.Atoi(id)
	if err != nil {
		http.Error(w, "Invalid invite ID", http.StatusBadRequest)
		return
	}

	// Fetch the invite to show email in confirmation
	pool, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Error opening database", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(pool)

	var email string
	err = pool.QueryRow(context.Background(), "SELECT email FROM invites WHERE id = $1", inviteID).Scan(&email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "Invite not found", http.StatusNotFound)
			return
		}
		http.Error(w, "Error fetching invite", http.StatusInternalServerError)
		return
	}

	data := struct {
		ID    int
		Email string
	}{
		ID:    inviteID,
		Email: email,
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	err = utils.Templates.ExecuteTemplate(w, "confirm_invite.html", data)
	if err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

// APICreateInvite handles creating a new invite
func APICreateInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	_, _, permissions, loggedIn := utils.GetSessionUser(r)
	basePath := utils.GetBasePath()
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in to create invites")
		return
	}

	// Check if user has createinvites permission
	hasPermission := false
	for _, p := range permissions {
		if p == "createinvites" {
			hasPermission = true
			break
		}
	}

	if !hasPermission {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "You don't have permission to create invites")
		return
	}

	err := r.ParseForm()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Error parsing form data")
		return
	}

	email := strings.TrimSpace(r.FormValue("email"))
	if email == "" {
		w.Header().Set("HX-Retarget", "#invite-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Email is required")
		return
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error opening database")
		return
	}
	defer storage.CloseDatabase(pool)

	// Check if email already exists
	var existingID int
	err = pool.QueryRow(context.Background(), "SELECT id FROM invites WHERE email = $1", email).Scan(&existingID)
	if err == nil {
		w.Header().Set("HX-Retarget", "#invite-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "An invite for this email already exists")
		return
	}

	errStr := err.Error()
	if errStr != "no rows in result set" && !errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error checking for existing invite")
		return
	}

	tokenBytes := make([]byte, 16)
	_, err = rand.Read(tokenBytes)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error generating token")
		return
	}
	token := hex.EncodeToString(tokenBytes)

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error creating invite")
		return
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, "INSERT INTO invites (email, token, inviteused) VALUES ($1, $2, 0)", email, token)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error creating invite")
		return
	}

	err = tx.Commit(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error creating invite")
		return
	}
	subject := "You're invited to join GoTodo!"
	body := fmt.Sprintf("You have been invited to join GoTodo. Use the following link to register:\n\n%s/register?token=%s\n\nIf you did not request this invite, please ignore this email.", basePath, token)
	err = utils.SendEmail(subject, body, email)
	if err != nil {
		fmt.Printf("Warning: Failed to send invite email to %s: %v\n", email, err)
	}

	// Redirect to reload the page
	w.Header().Set("HX-Redirect", basePath+"/createinvite")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

// APIGetInvites returns all invites as JSON
func APIGetInvites(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	_, _, permissions, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in")
		return
	}

	// Check if user has createinvites permission
	hasPermission := false
	for _, p := range permissions {
		if p == "createinvites" {
			hasPermission = true
			break
		}
	}

	if !hasPermission {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "You don't have permission")
		return
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error opening database")
		return
	}
	defer storage.CloseDatabase(pool)

	rows, err := pool.Query(context.Background(), "SELECT id, email, token, inviteused FROM invites ORDER BY id DESC")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error fetching invites")
		return
	}
	defer rows.Close()

	var invites []Invite
	for rows.Next() {
		var invite Invite
		err := rows.Scan(&invite.ID, &invite.Email, &invite.Token, &invite.InviteUsed)
		if err != nil {
			continue
		}
		// populate is_banned for API responses as well
		var uid int
		errUser := pool.QueryRow(context.Background(), "SELECT id FROM users WHERE email = $1", invite.Email).Scan(&uid)
		if errUser == nil {
			invite.InviteUsed = 1
			var isB bool
			err = pool.QueryRow(context.Background(), "SELECT is_banned FROM users WHERE email = $1", invite.Email).Scan(&isB)
			if err == nil {
				invite.IsBanned = isB
			} else {
				invite.IsBanned = false
			}
		} else {
			invite.IsBanned = false
		}
		invites = append(invites, invite)
	}

	// Return as HTML table rows for HTMX. For invites that have been used, show Active/Banned and ban/unban actions.
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	for _, invite := range invites {
		// Determine status badge: Pending when unused, Active/Banned when used
		statusBadge := "<span class=\"badge bg-warning\">Pending</span>"
		if invite.InviteUsed == 1 {
			if invite.IsBanned {
				statusBadge = "<span class=\"badge bg-danger\">Banned</span>"
			} else {
				statusBadge = "<span class=\"badge bg-success\">Active</span>"
			}
		}

		// Render the title cell with matching markup to the template so HTMX swaps keep behavior
		tokenCell := maskedTokenHTML(invite.Token)
		if invite.InviteUsed == 0 {
			fmt.Fprintf(w, `<tr id="invite-row-%d">
			<td class="title-column" data-label="Email">
				<form id="edit-invite-%d" style="display:inline; width:100%%;">
					<input type="email" class="form-control form-control-sm" name="email-%d" value="%s" style="display:block; width:100%%;" />
				</form>
				<button type="button" class="task-toggle btn btn-link d-md-none" aria-expanded="false" title="Show invite details"></button>
			</td>
			<td class="desc-column" data-label="Token">%s</td>
			<td class="status-column" data-label="Status">%s</td>
			<td class="delete-column" data-label="Delete">`, invite.ID, invite.ID, invite.ID, invite.Email, tokenCell, statusBadge)
		} else {
			fmt.Fprintf(w, `<tr id="invite-row-%d">
			<td class="title-column" data-label="Email"><span class="task-toggle" aria-expanded="false" aria-label="Invite %d details">%s</span></td>
			<td class="desc-column" data-label="Token">%s</td>
			<td class="status-column" data-label="Status">%s</td>
			<td class="delete-column" data-label="Delete">`, invite.ID, invite.ID, invite.Email, tokenCell, statusBadge)
		}

		if invite.InviteUsed == 0 {
			fmt.Fprintf(w, `<button class="btn btn-sm btn-warning me-1" hx-put="/api/invite/%d" hx-target="#invite-error" hx-swap="innerHTML" hx-include="[name='email-%d']" title="Edit invite" aria-label="Edit invite %d"><i class=\"bi bi-pencil\"></i></button>
				<button class="btn btn-sm btn-danger" hx-get="/api/confirm-invite-delete?id=%d" hx-target="#modal .modal-content" hx-trigger="click" data-bs-toggle="modal" data-bs-target="#modal" title="Delete invite" aria-label="Delete invite %d"><i class=\"bi bi-trash\"></i></button>`, invite.ID, invite.ID, invite.ID, invite.ID, invite.ID)
		} else {
			// Used invites: edit/delete only; user moderation lives on the admin page.
			fmt.Fprint(w, `<span class="text-muted">—</span>`)
		}
		fmt.Fprint(w, `</td>
		</tr>`)
	}
}

func renderUsersTablePartial(w http.ResponseWriter) {
	users, err := storage.ListUsers()
	if err != nil {
		http.Error(w, "Error loading users", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = utils.Templates.ExecuteTemplate(w, "users_table.html", map[string]interface{}{
		"Users": users,
	})
}

func maskedTokenHTML(token string) string {
	escaped := html.EscapeString(token)
	return fmt.Sprintf(
		`<code class="token-masked" data-token="%s">••••••••</code> <button type="button" class="btn btn-sm btn-link p-0 reveal-token-btn" aria-label="Show invite token">Show</button>`,
		escaped,
	)
}

// APIBanUser sets is_banned=true for the user with the provided email (if exists)
func APIBanUser(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	basePath := utils.GetBasePath()
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Error opening database", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "UPDATE users SET is_banned = TRUE WHERE email = $1", email)
	if err != nil {
		http.Error(w, "Error banning user", http.StatusInternalServerError)
		return
	}
	_ = storage.ClearCalendarTokenByEmail(email)
	if r.URL.Query().Get("source") == "admin" {
		renderUsersTablePartial(w)
		return
	}
	// Redirect to reload the page and show updated status
	w.Header().Set("HX-Redirect", basePath+"/admin")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

// APIUnbanUser sets is_banned=false for the user with the provided email (if exists)
func APIUnbanUser(w http.ResponseWriter, r *http.Request) {
	email := r.URL.Query().Get("email")
	basePath := utils.GetBasePath()
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	pool, err := storage.OpenDatabase()
	if err != nil {
		http.Error(w, "Error opening database", http.StatusInternalServerError)
		return
	}
	defer storage.CloseDatabase(pool)

	_, err = pool.Exec(context.Background(), "UPDATE users SET is_banned = FALSE WHERE email = $1", email)
	if err != nil {
		http.Error(w, "Error unbanning user", http.StatusInternalServerError)
		return
	}
	if r.URL.Query().Get("source") == "admin" {
		renderUsersTablePartial(w)
		return
	}
	// Redirect to reload the page and show updated status
	w.Header().Set("HX-Redirect", basePath+"/admin")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

// APIUpdateInvite handles updating an invite (only email can be updated, and only if unused)
func APIUpdateInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	_, _, permissions, loggedIn := utils.GetSessionUser(r)
	basePath := utils.GetBasePath()
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in")
		return
	}

	// Check if user has createinvites permission
	hasPermission := false
	for _, p := range permissions {
		if p == "createinvites" {
			hasPermission = true
			break
		}
	}

	if !hasPermission {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "You don't have permission")
		return
	}

	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Invalid invite ID")
		return
	}

	inviteID, err := strconv.Atoi(pathParts[3])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Invalid invite ID")
		return
	}

	err = r.ParseForm()
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Error parsing form data")
		return
	}

	newEmail := strings.TrimSpace(r.FormValue(fmt.Sprintf("email-%d", inviteID)))

	if newEmail == "" {
		for key, values := range r.Form {
			if strings.HasPrefix(key, "email-") && len(values) > 0 {
				newEmail = strings.TrimSpace(values[0])
				break
			}
		}
	}

	if newEmail == "" {
		w.Header().Set("HX-Retarget", "#invite-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Email is required")
		return
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error opening database")
		return
	}
	defer storage.CloseDatabase(pool)

	var inviteUsed int
	var currentEmail string
	err = pool.QueryRow(context.Background(), "SELECT inviteused, email FROM invites WHERE id = $1", inviteID).Scan(&inviteUsed, &currentEmail)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			w.Header().Set("HX-Retarget", "#invite-error")
			w.Header().Set("HX-Reswap", "innerHTML")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "Invite not found")
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error checking invite")
		return
	}

	if inviteUsed == 1 {
		w.Header().Set("HX-Retarget", "#invite-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Cannot edit an invite that has already been used")
		return
	}

	var existingID int
	err = pool.QueryRow(context.Background(), "SELECT id FROM invites WHERE email = $1 AND id != $2", newEmail, inviteID).Scan(&existingID)
	if err == nil {
		w.Header().Set("HX-Retarget", "#invite-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "An invite for this email already exists")
		return
	}

	errStr := err.Error()
	if errStr != "no rows in result set" && !errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error checking for existing invite")
		return
	}

	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error updating invite")
		return
	}
	defer tx.Rollback(ctx)

	result, err := tx.Exec(ctx, "UPDATE invites SET email = $1 WHERE id = $2", newEmail, inviteID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error updating invite")
		return
	}

	if result.RowsAffected() == 0 {
		w.Header().Set("HX-Retarget", "#invite-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "No changes made - invite not found or email unchanged")
		return
	}

	err = tx.Commit(ctx)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error updating invite")
		return
	}

	// Redirect to reload the page
	w.Header().Set("HX-Redirect", basePath+"/createinvite")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}

// APIDeleteInvite handles deleting an invite (only if unused)
func APIDeleteInvite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	_, _, permissions, loggedIn := utils.GetSessionUser(r)
	if !loggedIn {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "Please log in")
		return
	}

	// Check if user has createinvites permission
	hasPermission := false
	for _, p := range permissions {
		if p == "createinvites" {
			hasPermission = true
			break
		}
	}

	if !hasPermission {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, "You don't have permission")
		return
	}

	// Extract invite ID from URL path
	pathParts := strings.Split(r.URL.Path, "/")
	if len(pathParts) < 4 {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Invalid invite ID")
		return
	}

	inviteID, err := strconv.Atoi(pathParts[3])
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "Invalid invite ID")
		return
	}

	pool, err := storage.OpenDatabase()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error opening database")
		return
	}
	defer storage.CloseDatabase(pool)

	// Check if invite exists and is unused
	var inviteUsed int
	err = pool.QueryRow(context.Background(), "SELECT inviteused FROM invites WHERE id = $1", inviteID).Scan(&inviteUsed)
	if err != nil {
		if err == sql.ErrNoRows {
			w.Header().Set("HX-Retarget", "#invite-error")
			w.Header().Set("HX-Reswap", "innerHTML")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "Invite not found")
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error checking invite")
		return
	}

	if inviteUsed == 1 {
		w.Header().Set("HX-Retarget", "#invite-error")
		w.Header().Set("HX-Reswap", "innerHTML")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Cannot delete an invite that has already been used")
		return
	}

	// Delete the invite
	_, err = pool.Exec(context.Background(), "DELETE FROM invites WHERE id = $1", inviteID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "Error deleting invite")
		return
	}
	basePath := utils.GetBasePath()
	w.Header().Set("HX-Redirect", basePath+"/createinvite")
	w.Header().Set("HX-Trigger", "inviteDeleted")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, " ")
}
