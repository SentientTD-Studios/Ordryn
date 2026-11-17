package domain

import (
	"context"
	"fmt"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// ReactToCommentForUser adds or removes the user's reaction on a comment. Anyone
// who can open the task's discussion can react.
func ReactToCommentForUser(ctx context.Context, userID, taskID, commentID int, code string, on bool) (*storage.TaskComment, error) {
	_ = ctx
	projectID, err := requireProjectTaskAccess(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !storage.ValidReactionCode(code) {
		return nil, fmt.Errorf("%w: unknown reaction", ErrValidation)
	}
	comment, err := loadCommentOnTask(taskID, commentID)
	if err != nil {
		return nil, err
	}
	if comment.DeletedAt != nil {
		return nil, fmt.Errorf("%w: the comment was deleted", ErrConflict)
	}
	if err := storage.SetCommentReaction(commentID, userID, code, on); err != nil {
		return nil, err
	}
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskCommented)
	return reloadComment(taskID, projectID, userID, commentID)
}

// ResolveCommentForUser marks a comment answered (or reopens it). The author,
// anyone who can edit the task, and comment moderators may do it.
func ResolveCommentForUser(ctx context.Context, userID, taskID, commentID int, resolved bool) (*storage.TaskComment, error) {
	_ = ctx
	canRead, writeRole, projectID, err := storage.CanUserAccessTask(taskID, userID)
	if err != nil {
		return nil, err
	}
	if !canRead {
		return nil, ErrNotFound
	}
	if projectID <= 0 {
		return nil, fmt.Errorf("%w: discussion is only available on project tasks", ErrValidation)
	}
	comment, err := loadCommentOnTask(taskID, commentID)
	if err != nil {
		return nil, err
	}
	if comment.DeletedAt != nil {
		return nil, fmt.Errorf("%w: the comment was deleted", ErrConflict)
	}
	isAuthor, isModerator, isAdmin := commentActorRole(projectID, userID, comment)
	canEdit := storage.HasProjectPerm(projectID, writeRole, storage.PermTasksEdit)
	if !isAuthor && !isModerator && !isAdmin && !canEdit {
		return nil, fmt.Errorf("%w: only the author or people who can edit the task can resolve this", ErrForbidden)
	}
	if err := storage.SetCommentResolved(commentID, userID, resolved); err != nil {
		return nil, err
	}
	event := "comment_reopened"
	if resolved {
		event = "comment_resolved"
	}
	_ = storage.LogTaskEvent(taskID, userID, event, map[string]interface{}{"comment_id": commentID})
	live.AfterTaskChangeLive(userID, taskID, live.TypeTaskCommented)
	return reloadComment(taskID, projectID, userID, commentID)
}

func reloadComment(taskID, projectID, userID, commentID int) (*storage.TaskComment, error) {
	c, err := storage.GetTaskComment(commentID)
	if err != nil {
		return nil, err
	}
	c.Links = ResolveCommentTaskLinks(taskID, projectID, userID, c.Body)
	attachCommentRole(projectID, c)
	list := []storage.TaskComment{*c}
	if err := storage.AttachCommentExtras(list, userID); err != nil {
		return nil, err
	}
	return &list[0], nil
}
