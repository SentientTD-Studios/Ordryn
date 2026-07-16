package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	UndoTTLSeconds   = 120
	undoTokenPrefix  = "gotodo:undo:"
)

// UndoTaskSnapshot is the Redis-serializable form of a deleted task for restore.
type UndoTaskSnapshot struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	DueDate     string `json:"due_date"`
	Completed   bool   `json:"completed"`
	IsFavorite  bool   `json:"is_favorite"`
	Priority    int    `json:"priority"`
	Position    int    `json:"position"`
	ProjectID   *int   `json:"project_id,omitempty"`
	TagIDs      []int  `json:"tag_ids,omitempty"`
}

type undoPayload struct {
	UserID int                `json:"user_id"`
	Tasks  []UndoTaskSnapshot `json:"tasks"`
}

func undoRedisKey(userID int, token string) string {
	return fmt.Sprintf("%s%d:%s", undoTokenPrefix, userID, token)
}

// SaveUndoToken stores deleted-task snapshots in Redis and returns an opaque token.
func SaveUndoToken(ctx context.Context, userID int, tasks []UndoTaskSnapshot) (token string, err error) {
	if RedisClient == nil {
		return "", fmt.Errorf("redis unavailable")
	}
	if len(tasks) == 0 {
		return "", fmt.Errorf("nothing to undo")
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token = hex.EncodeToString(b)
	data, err := json.Marshal(undoPayload{UserID: userID, Tasks: tasks})
	if err != nil {
		return "", err
	}
	if err := RedisClient.Set(ctx, undoRedisKey(userID, token), data, time.Duration(UndoTTLSeconds)*time.Second).Err(); err != nil {
		return "", err
	}
	return token, nil
}

// LoadUndoToken loads and deletes a one-time undo payload for the user.
func LoadUndoToken(ctx context.Context, userID int, token string) ([]UndoTaskSnapshot, error) {
	if RedisClient == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("undo_token is required")
	}
	key := undoRedisKey(userID, token)
	data, err := RedisClient.Get(ctx, key).Bytes()
	if err != nil {
		return nil, fmt.Errorf("undo expired or invalid")
	}
	_ = RedisClient.Del(ctx, key).Err()
	var payload undoPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("invalid undo state")
	}
	if payload.UserID != userID || len(payload.Tasks) == 0 {
		return nil, fmt.Errorf("undo expired or invalid")
	}
	return payload.Tasks, nil
}
