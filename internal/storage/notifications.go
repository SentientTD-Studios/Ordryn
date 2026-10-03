package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	NotificationTaskCreated   = "task_created"
	NotificationTaskCommented = "task_commented"
	NotificationTaskMentioned = "task_mentioned"
	NotificationJoinRequest   = "join_request"
)

// UserNotification is an in-app notification for a user.
type UserNotification struct {
	ID          int
	UserID      int
	ActorUserID int
	Type        string
	ProjectID   int
	TaskID      int
	Title       string
	Body        string
	ReadAt      *time.Time
	CreatedAt   time.Time
	ActorName   string
	ProjectName string
}

// CreateUserNotificationsTable creates the notifications store.
func CreateUserNotificationsTable() error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS user_notifications (
			id SERIAL PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			actor_user_id INTEGER REFERENCES users(id) ON DELETE SET NULL,
			type TEXT NOT NULL,
			project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
			task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
			title TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL DEFAULT '',
			read_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_user_notifications_user_created
			ON user_notifications (user_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_user_notifications_user_unread
			ON user_notifications (user_id) WHERE read_at IS NULL`,
	}
	for _, s := range stmts {
		if _, err := pool.Exec(context.Background(), s); err != nil {
			return fmt.Errorf("failed to create user_notifications: %v", err)
		}
	}
	return nil
}

// CreateUserNotification inserts a single notification.
func CreateUserNotification(n UserNotification) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var actorArg, projectArg, taskArg interface{}
	if n.ActorUserID > 0 {
		actorArg = n.ActorUserID
	}
	if n.ProjectID > 0 {
		projectArg = n.ProjectID
	}
	if n.TaskID > 0 {
		taskArg = n.TaskID
	}

	// AI agents have no inbox; their notifications are dropped (id 0).
	var id int
	err = pool.QueryRow(context.Background(),
		`INSERT INTO user_notifications
			(user_id, actor_user_id, type, project_id, task_id, title, body)
		 SELECT $1, $2, $3, $4, $5, $6, $7
		 WHERE NOT EXISTS (SELECT 1 FROM users WHERE id = $1 AND is_agent)
		 RETURNING id`,
		n.UserID, actorArg, n.Type, projectArg, taskArg, n.Title, n.Body,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// CreateUserNotificationsBulk inserts many notifications (best-effort per row via multi-insert).
func CreateUserNotificationsBulk(items []UserNotification) error {
	if len(items) == 0 {
		return nil
	}
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	for _, n := range items {
		var actorArg, projectArg, taskArg interface{}
		if n.ActorUserID > 0 {
			actorArg = n.ActorUserID
		}
		if n.ProjectID > 0 {
			projectArg = n.ProjectID
		}
		if n.TaskID > 0 {
			taskArg = n.TaskID
		}
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO user_notifications
				(user_id, actor_user_id, type, project_id, task_id, title, body)
			 SELECT $1, $2, $3, $4, $5, $6, $7
			 WHERE NOT EXISTS (SELECT 1 FROM users WHERE id = $1 AND is_agent)`,
			n.UserID, actorArg, n.Type, projectArg, taskArg, n.Title, n.Body,
		); err != nil {
			return err
		}
	}
	return nil
}

// ListUserNotifications returns newest notifications for a user.
func ListUserNotifications(userID, limit, offset int) ([]UserNotification, int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return nil, 0, err
	}
	defer CloseDatabase(pool)

	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM user_notifications WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := pool.Query(context.Background(),
		`SELECT n.id, n.user_id, COALESCE(n.actor_user_id, 0), n.type,
		        COALESCE(n.project_id, 0), COALESCE(n.task_id, 0),
		        n.title, n.body, n.read_at, n.created_at,
		        COALESCE(u.user_name, u.email, ''),
		        COALESCE(p.name, '')
		 FROM user_notifications n
		 LEFT JOIN users u ON u.id = n.actor_user_id
		 LEFT JOIN projects p ON p.id = n.project_id
		 WHERE n.user_id = $1
		 ORDER BY n.created_at DESC, n.id DESC
		 LIMIT $2 OFFSET $3`,
		userID, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []UserNotification
	for rows.Next() {
		var n UserNotification
		var readAt sql.NullTime
		if err := rows.Scan(
			&n.ID, &n.UserID, &n.ActorUserID, &n.Type,
			&n.ProjectID, &n.TaskID, &n.Title, &n.Body, &readAt, &n.CreatedAt,
			&n.ActorName, &n.ProjectName,
		); err != nil {
			return nil, 0, err
		}
		if readAt.Valid {
			t := readAt.Time
			n.ReadAt = &t
		}
		out = append(out, n)
	}
	return out, total, rows.Err()
}

// CountUnreadUserNotifications returns unread count for a user.
func CountUnreadUserNotifications(userID int) (int, error) {
	pool, err := OpenDatabase()
	if err != nil {
		return 0, err
	}
	defer CloseDatabase(pool)

	var n int
	err = pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM user_notifications WHERE user_id = $1 AND read_at IS NULL`,
		userID).Scan(&n)
	return n, err
}

// MarkUserNotificationRead marks one notification read if owned by userID.
func MarkUserNotificationRead(userID, notificationID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	tag, err := pool.Exec(context.Background(),
		`UPDATE user_notifications SET read_at = NOW()
		 WHERE id = $1 AND user_id = $2 AND read_at IS NULL`,
		notificationID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either already read or not found / not owned — distinguish via existence.
		var exists bool
		err = pool.QueryRow(context.Background(),
			`SELECT EXISTS(SELECT 1 FROM user_notifications WHERE id = $1 AND user_id = $2)`,
			notificationID, userID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return sql.ErrNoRows
		}
	}
	return nil
}

// MarkAllUserNotificationsRead marks all unread notifications for a user.
func MarkAllUserNotificationsRead(userID int) error {
	pool, err := OpenDatabase()
	if err != nil {
		return err
	}
	defer CloseDatabase(pool)

	_, err = pool.Exec(context.Background(),
		`UPDATE user_notifications SET read_at = NOW()
		 WHERE user_id = $1 AND read_at IS NULL`, userID)
	return err
}
