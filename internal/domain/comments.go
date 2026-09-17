package domain

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

const maxCommentPreview = 120

var (
	commentTaskRefPattern = regexp.MustCompile(`\[\[(\d+)\]\]|#(\d+)\b`)
	commentMentionPattern = regexp.MustCompile(`(^|[^A-Za-z0-9_@])@([A-Za-z0-9_]{3,32})\b`)
)

func requireProjectTaskAccess(taskID, userID int) (projectID int, err error) {
	canRead, _, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return 0, err
	}
	if !canRead {
		return 0, ErrNotFound
	}
	if projectID <= 0 {
		return 0, fmt.Errorf("%w: discussion is only available on project tasks", ErrValidation)
	}
	return projectID, nil
}

// ParseCommentTaskIDs returns unique task IDs referenced as [[123]] or #123.
func ParseCommentTaskIDs(body string) []int {
	matches := commentTaskRefPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[int]struct{}, len(matches))
	ids := make([]int, 0, len(matches))
	for _, m := range matches {
		raw := m[1]
		if raw == "" {
			raw = m[2]
		}
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// ParseCommentMentions returns unique @usernames from a comment body, in first-seen order.
// Email addresses are not treated as mentions.
func ParseCommentMentions(body string) []string {
	matches := commentMentionPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		name := strings.TrimSpace(m[2])
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		names = append(names, name)
	}
	return names
}

// ResolveCommentMentions maps @usernames in body to current project members.
func ResolveCommentMentions(projectID int, body string) []storage.ProjectMember {
	names := ParseCommentMentions(body)
	if projectID <= 0 || len(names) == 0 {
		return nil
	}
	members, err := storage.ListProjectMembers(projectID)
	if err != nil || len(members) == 0 {
		return nil
	}
	wanted := make(map[string]struct{}, len(names))
	for _, name := range names {
		wanted[strings.ToLower(name)] = struct{}{}
	}
	out := make([]storage.ProjectMember, 0, len(names))
	seen := make(map[int]struct{}, len(names))
	for _, m := range members {
		key := strings.ToLower(strings.TrimSpace(m.UserName))
		if key == "" {
			continue
		}
		if _, ok := wanted[key]; !ok {
			continue
		}
		if _, ok := seen[m.UserID]; ok {
			continue
		}
		seen[m.UserID] = struct{}{}
		out = append(out, m)
	}
	return out
}

// ResolveCommentTaskLinks returns titles for referenced tasks within projectID (excluding currentTaskID) that the user can access.
func ResolveCommentTaskLinks(currentTaskID, projectID, userID int, body string) []storage.TaskCommentLink {
	ids := ParseCommentTaskIDs(body)
	if len(ids) == 0 {
		return nil
	}
	out := make([]storage.TaskCommentLink, 0, len(ids))
	for _, id := range ids {
		if currentTaskID > 0 && id == currentTaskID {
			continue
		}
		canRead, _, taskProjectID, err := storage.CanUserAccessTask(id, userID)
		if err != nil || !canRead {
			continue
		}
		if projectID > 0 && taskProjectID != projectID {
			continue
		}
		td, err := storage.GetTaskTitleDescription(id)
		if err != nil || td == nil {
			continue
		}
		title := strings.TrimSpace(td.Title)
		if title == "" {
			title = fmt.Sprintf("Task #%d", id)
		}
		out = append(out, storage.TaskCommentLink{ID: id, Title: title})
	}
	return out
}

func attachCommentLinks(taskID, projectID, userID int, comments []storage.TaskComment) {
	for i := range comments {
		if comments[i].DeletedAt != nil || comments[i].Body == "" {
			continue
		}
		comments[i].Links = ResolveCommentTaskLinks(taskID, projectID, userID, comments[i].Body)
	}
}

// ListCommentsForUser lists discussion posts on a project task the user can read.
func ListCommentsForUser(ctx context.Context, userID, taskID int) ([]storage.TaskComment, error) {
	_ = ctx
	projectID, err := requireProjectTaskAccess(taskID, userID)
	if err != nil {
		return nil, err
	}
	comments, err := storage.ListTaskComments(taskID)
	if err != nil {
		return nil, err
	}
	if comments == nil {
		comments = []storage.TaskComment{}
	}
	attachCommentLinks(taskID, projectID, userID, comments)
	return comments, nil
}

// AddCommentForUser posts a discussion message. Any project member (including viewers) may post.
func AddCommentForUser(ctx context.Context, userID, taskID int, body string) (*storage.TaskComment, error) {
	_ = ctx
	projectID, err := requireProjectTaskAccess(taskID, userID)
	if err != nil {
		return nil, err
	}
	body, err = validateCommentBody(body)
	if err != nil {
		return nil, err
	}
	comment, err := storage.CreateTaskComment(taskID, userID, body)
	if err != nil {
		return nil, err
	}
	comment.Links = ResolveCommentTaskLinks(taskID, projectID, userID, comment.Body)
	NotifyProjectMembersTaskCommented(taskID, userID, projectID, body)
	live.AfterTaskChangeMeta(userID, taskID, live.TypeTaskCommented, &live.TaskHookMeta{Comment: body})
	if meta := mentionHookMeta(projectID, userID, body); meta != nil {
		live.DispatchHook(userID, taskID, live.TypeTaskMentioned, meta)
	}
	return comment, nil
}

func mentionHookMeta(projectID, actorUserID int, body string) *live.TaskHookMeta {
	mentioned := ResolveCommentMentions(projectID, body)
	if len(mentioned) == 0 {
		return nil
	}
	ids := make([]int, 0, len(mentioned))
	names := make([]string, 0, len(mentioned))
	for _, m := range mentioned {
		if m.UserID == actorUserID {
			continue
		}
		ids = append(ids, m.UserID)
		name := strings.TrimSpace(m.UserName)
		if name == "" {
			name = strings.TrimSpace(m.Email)
		}
		if name != "" {
			names = append(names, name)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return &live.TaskHookMeta{
		Comment:          body,
		MentionedUserIDs: ids,
		Mentions:         names,
	}
}

func mentionHookMetaDiff(projectID, actorUserID int, oldBody, newBody string) *live.TaskHookMeta {
	oldMeta := mentionHookMeta(projectID, actorUserID, oldBody)
	newMeta := mentionHookMeta(projectID, actorUserID, newBody)
	if newMeta == nil {
		return nil
	}
	if oldMeta == nil {
		return newMeta
	}
	seen := make(map[int]struct{}, len(oldMeta.MentionedUserIDs))
	for _, id := range oldMeta.MentionedUserIDs {
		seen[id] = struct{}{}
	}
	ids := make([]int, 0)
	names := make([]string, 0)
	nameByID := make(map[int]string)
	for i, id := range newMeta.MentionedUserIDs {
		if i < len(newMeta.Mentions) {
			nameByID[id] = newMeta.Mentions[i]
		}
		if _, ok := seen[id]; ok {
			continue
		}
		ids = append(ids, id)
		if n := nameByID[id]; n != "" {
			names = append(names, n)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return &live.TaskHookMeta{
		Comment:          newBody,
		MentionedUserIDs: ids,
		Mentions:         names,
	}
}

// DeleteCommentForUser soft-deletes a comment. Authors delete as "user";
// project owners deleting someone else's comment delete as "owner".
func DeleteCommentForUser(ctx context.Context, userID, taskID, commentID int) error {
	_ = ctx
	projectID, err := requireProjectTaskAccess(taskID, userID)
	if err != nil {
		return err
	}
	comment, err := storage.GetTaskComment(commentID)
	if err != nil || comment.TaskID != taskID {
		return ErrNotFound
	}
	if comment.DeletedAt != nil {
		return fmt.Errorf("%w: comment already deleted", ErrConflict)
	}

	isAuthor := comment.UserID == userID
	role, _ := storage.GetProjectRole(projectID, userID)
	isOwner := storage.RoleCanManage(role)

	if !isAuthor && !isOwner {
		return ErrForbidden
	}

	kind := storage.CommentDeletedByUser
	if !isAuthor && isOwner {
		kind = storage.CommentDeletedByOwner
	}
	if err := storage.SoftDeleteTaskComment(commentID, userID, kind); err != nil {
		return err
	}
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskCommented)
	live.DispatchHook(userID, taskID, live.TypeTaskCommentDeleted, &live.TaskHookMeta{Comment: comment.Body})
	return nil
}

func commentActorRole(projectID, userID int, comment *storage.TaskComment) (isAuthor, isOwner, isAdmin bool) {
	isAuthor = comment.UserID == userID
	role, _ := storage.GetProjectRole(projectID, userID)
	isOwner = storage.RoleCanManage(role)
	isAdmin = storage.UserHasPermission(userID, "admin")
	return isAuthor, isOwner, isAdmin
}

func validateCommentBody(body string) (string, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return "", fmt.Errorf("%w: comment cannot be empty", ErrValidation)
	}
	if len(body) > storage.MaxTaskCommentBody {
		return "", fmt.Errorf("%w: comment must be %d characters or less", ErrValidation, storage.MaxTaskCommentBody)
	}
	return body, nil
}

func loadCommentOnTask(taskID, commentID int) (*storage.TaskComment, error) {
	comment, err := storage.GetTaskComment(commentID)
	if err != nil || comment.TaskID != taskID {
		return nil, ErrNotFound
	}
	return comment, nil
}

// EditCommentForUser updates a comment. Authors edit their own posts;
// project owners may edit anyone's comment on that project.
func EditCommentForUser(ctx context.Context, userID, taskID, commentID int, body string) (*storage.TaskComment, error) {
	_ = ctx
	projectID, err := requireProjectTaskAccess(taskID, userID)
	if err != nil {
		return nil, err
	}
	comment, err := loadCommentOnTask(taskID, commentID)
	if err != nil {
		return nil, err
	}
	if comment.DeletedAt != nil {
		return nil, fmt.Errorf("%w: cannot edit a deleted comment", ErrConflict)
	}
	isAuthor, isOwner, _ := commentActorRole(projectID, userID, comment)
	if !isAuthor && !isOwner {
		return nil, ErrForbidden
	}
	body, err = validateCommentBody(body)
	if err != nil {
		return nil, err
	}
	updated, err := storage.UpdateTaskComment(commentID, userID, body)
	if err != nil {
		return nil, err
	}
	updated.Links = ResolveCommentTaskLinks(taskID, projectID, userID, updated.Body)
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskCommented)
	live.DispatchHook(userID, taskID, live.TypeTaskCommentEdited, &live.TaskHookMeta{Comment: body})
	if meta := mentionHookMetaDiff(projectID, userID, comment.Body, body); meta != nil {
		live.DispatchHook(userID, taskID, live.TypeTaskMentioned, meta)
	}
	return updated, nil
}

func canViewCommentHistory(projectID, userID int) bool {
	role, _ := storage.GetProjectRole(projectID, userID)
	return storage.RoleCanManage(role) || storage.UserHasPermission(userID, "admin")
}

// ListCommentRevisionsForUser returns prior versions for a project owner or site admin.
func ListCommentRevisionsForUser(ctx context.Context, userID, taskID, commentID int) ([]storage.TaskCommentRevision, error) {
	_ = ctx
	projectID, err := requireProjectTaskAccess(taskID, userID)
	if err != nil {
		return nil, err
	}
	if _, err := loadCommentOnTask(taskID, commentID); err != nil {
		return nil, err
	}
	if !canViewCommentHistory(projectID, userID) {
		return nil, ErrForbidden
	}
	revs, err := storage.ListCommentRevisions(commentID)
	if err != nil {
		return nil, err
	}
	if revs == nil {
		revs = []storage.TaskCommentRevision{}
	}
	return revs, nil
}

// RestoreCommentRevisionForUser restores a previous version. Project owners
// and site admins with task access may restore.
func RestoreCommentRevisionForUser(ctx context.Context, userID, taskID, commentID, revisionID int) (*storage.TaskComment, error) {
	_ = ctx
	projectID, err := requireProjectTaskAccess(taskID, userID)
	if err != nil {
		return nil, err
	}
	if _, err := loadCommentOnTask(taskID, commentID); err != nil {
		return nil, err
	}
	if !canViewCommentHistory(projectID, userID) {
		return nil, ErrForbidden
	}
	rev, err := storage.GetCommentRevision(revisionID)
	if err != nil || rev.CommentID != commentID || rev.TaskID != taskID {
		return nil, ErrNotFound
	}
	if strings.TrimSpace(rev.Body) == "" {
		return nil, fmt.Errorf("%w: cannot restore empty comment content", ErrValidation)
	}
	updated, err := storage.RestoreTaskCommentFromRevision(revisionID, userID)
	if err != nil {
		return nil, err
	}
	updated.Links = ResolveCommentTaskLinks(taskID, projectID, userID, updated.Body)
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskCommented)
	live.DispatchHook(userID, taskID, live.TypeTaskCommentRestored, &live.TaskHookMeta{Comment: updated.Body})
	return updated, nil
}

// ListCommentAuditForAdmin returns the site-wide comment revision log.
func ListCommentAuditForAdmin(ctx context.Context, userID int, filter storage.CommentAuditFilter) ([]storage.TaskCommentRevision, int, error) {
	_ = ctx
	if !storage.UserHasPermission(userID, "admin") {
		return nil, 0, ErrForbidden
	}
	return storage.ListCommentAudit(filter)
}

// RestoreCommentRevisionForAdmin restores from the site-wide audit log.
func RestoreCommentRevisionForAdmin(ctx context.Context, userID, revisionID int) (*storage.TaskComment, error) {
	_ = ctx
	if !storage.UserHasPermission(userID, "admin") {
		return nil, ErrForbidden
	}
	rev, err := storage.GetCommentRevision(revisionID)
	if err != nil {
		return nil, ErrNotFound
	}
	if _, err := validateCommentBody(rev.Body); err != nil {
		return nil, err
	}
	updated, err := storage.RestoreTaskCommentFromRevision(revisionID, userID)
	if err != nil {
		return nil, err
	}
	updated.Links = ResolveCommentTaskLinks(rev.TaskID, rev.ProjectID, userID, updated.Body)
	live.AfterTaskChangeLive(userID, updated.TaskID, live.TypeTaskCommented)
	live.DispatchHook(userID, updated.TaskID, live.TypeTaskCommentRestored, &live.TaskHookMeta{Comment: updated.Body})
	return updated, nil
}

func commentPreview(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return "New comment"
	}
	if len(body) <= maxCommentPreview {
		return body
	}
	return body[:maxCommentPreview-3] + "..."
}
