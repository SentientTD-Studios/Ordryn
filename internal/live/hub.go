package live

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const userChannelPrefix = "user:"

// Event is a lightweight invalidation payload pushed over SSE.
type Event struct {
	Type      string `json:"type"`
	TaskID    int    `json:"task_id,omitempty"`
	ProjectID int    `json:"project_id,omitempty"`
	ActorID   int    `json:"actor_id,omitempty"`
	Origin    string `json:"origin,omitempty"`
	Timestamp string `json:"timestamp"`
}

const (
	TypeTaskCreated     = "task.created"
	TypeTaskUpdated     = "task.updated"
	TypeTaskDeleted     = "task.deleted"
	TypeTaskReordered   = "task.reordered"
	TypeTaskCommented   = "task.commented"
	TypeTaskClaimed     = "task.claimed"
	TypeTaskUnclaimed   = "task.unclaimed"
	TypeTaskDueChanged  = "task.due_changed"
	TypeTaskMoved       = "task.moved"
	TypeTaskTagged      = "task.tagged"
	TypeTaskOverdue     = "task.overdue"
	TypeProjectUpdated  = "project.updated"
	TypeJoinRequest     = "join.request"
)

// Hub fans events out to in-process SSE subscribers and, when Redis is
// configured, across process instances.
type Hub struct {
	mu       sync.RWMutex
	channels map[string]map[chan []byte]struct{}
	bridge   *redisBridge
	origin   string
}

func NewHub(client *redis.Client) *Hub {
	h := &Hub{
		channels: make(map[string]map[chan []byte]struct{}),
		origin:   uuid.NewString(),
	}
	h.bridge = newRedisBridge(client, h.handleRemote)
	return h
}

func UserChannelKey(userID int) string {
	if userID <= 0 {
		return ""
	}
	return userChannelPrefix + itoa(userID)
}

func (h *Hub) Origin() string {
	if h == nil {
		return ""
	}
	return h.origin
}

func (h *Hub) handleRemote(channelKey string, payload []byte) {
	if h == nil {
		return
	}
	var ev Event
	if err := json.Unmarshal(payload, &ev); err == nil && ev.Origin != "" && ev.Origin == h.origin {
		return
	}
	h.deliverLocal(channelKey, payload)
}

func (h *Hub) deliverLocal(channelKey string, event []byte) {
	if h == nil || channelKey == "" {
		return
	}
	h.mu.RLock()
	subs := h.channels[channelKey]
	targets := make([]chan []byte, 0, len(subs))
	for ch := range subs {
		targets = append(targets, ch)
	}
	h.mu.RUnlock()
	for _, ch := range targets {
		select {
		case ch <- event:
		default:
		}
	}
}

// Subscribe registers a buffered subscriber until ctx is done.
func (h *Hub) Subscribe(ctx context.Context, channelKey string) <-chan []byte {
	ch := make(chan []byte, 8)
	if h == nil || channelKey == "" {
		close(ch)
		return ch
	}
	h.mu.Lock()
	if h.channels[channelKey] == nil {
		h.channels[channelKey] = make(map[chan []byte]struct{})
	}
	h.channels[channelKey][ch] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		delete(h.channels[channelKey], ch)
		if len(h.channels[channelKey]) == 0 {
			delete(h.channels, channelKey)
		}
		h.mu.Unlock()
	}()
	return ch
}

func (h *Hub) Broadcast(channelKey string, event []byte) {
	if h == nil || channelKey == "" {
		return
	}
	h.deliverLocal(channelKey, event)
	if h.bridge != nil {
		h.bridge.publish(channelKey, event)
	}
}

// Publish sends ev to each unique positive user ID.
func (h *Hub) Publish(ev Event, userIDs []int) {
	if h == nil {
		return
	}
	ev.Origin = h.origin
	if ev.Timestamp == "" {
		ev.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return
	}
	for _, id := range uniquePositive(userIDs) {
		h.Broadcast(UserChannelKey(id), payload)
	}
}
