package hooks

import (
	"log"
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

const coalesceWindow = 3 * time.Second
const maxDeliveryAttempts = 8

func enqueueOrSend(entry extensions.Entry, ctx destContext) {
	key := coalesceKey(entry.ID, ctx)
	if existing, err := storage.FindPendingCoalesceDelivery(key, coalesceWindow); err == nil && existing > 0 {
		_ = storage.ReplaceDeliveryPayload(existing, ctx.Event.EventID, queuedPayloadJSON(ctx))
		return
	}
	host := destinationHost(entry, ctx)
	id, err := storage.InsertExtensionDelivery(storage.ExtensionDelivery{
		ExtensionID:   entry.ID,
		ProjectID:     ctx.ProjectID,
		UserID:        ctx.UserID,
		TaskID:        ctx.Event.TaskID,
		EventType:     ctx.Event.Type,
		EventID:       ctx.Event.EventID,
		URLHost:       host,
		Status:        storage.DeliveryStatusPending,
		CoalesceKey:   key,
		Payload:       queuedPayloadJSON(ctx),
		NextAttemptAt: time.Now().UTC().Add(coalesceWindow),
	})
	if err != nil {
		log.Printf("hooks: queue %s: %v", entry.ID, err)
		_, sendErr := deliverNow(entry, ctx)
		recordLast(entry.ID, ctx.ProjectID, ctx.UserID, sendErr)
		return
	}
	_ = id
}

func enqueueDigest(entry extensions.Entry, ctx destContext) {
	delay := digestDelay(ctx.Dest.Digest)
	if delay <= 0 {
		delay = time.Hour
	}
	host := destinationHost(entry, ctx)
	_, err := storage.InsertExtensionDelivery(storage.ExtensionDelivery{
		ExtensionID:   entry.ID,
		ProjectID:     ctx.ProjectID,
		UserID:        ctx.UserID,
		TaskID:        ctx.Event.TaskID,
		EventType:     ctx.Event.Type,
		EventID:       ctx.Event.EventID,
		URLHost:       host,
		Status:        storage.DeliveryStatusDigest,
		CoalesceKey:   "digest:" + coalesceKey(entry.ID, ctx),
		Payload:       queuedPayloadJSON(ctx),
		NextAttemptAt: time.Now().UTC().Add(delay),
	})
	if err != nil {
		log.Printf("hooks: digest queue %s: %v", entry.ID, err)
	}
}

func coalesceKey(extensionID string, ctx destContext) string {
	return strings.Join([]string{
		extensionID,
		itoa(ctx.ProjectID),
		itoa(ctx.UserID),
		itoa(ctx.Event.TaskID),
		ctx.Event.Type,
	}, ":")
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func destinationHost(entry extensions.Entry, ctx destContext) string {
	if entry.Manifest.Delivery == nil {
		return ""
	}
	key := entry.Manifest.Delivery.DestinationKey()
	u, _ := storage.GetExtensionSecretForUser(entry.ID, ctx.ProjectID, ctx.UserID, key)
	return hostOf(u)
}

// StartDeliveryWorker retries failed/pending deliveries and flushes digests.
func StartDeliveryWorker() {
	go func() {
		runDeliveryPass()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			runDeliveryPass()
		}
	}()
}

func runDeliveryPass() {
	flushDigests()
	rows, err := storage.ListRetryableDeliveries(40)
	if err != nil {
		log.Printf("hooks: list deliveries: %v", err)
		return
	}
	for _, row := range rows {
		flushDelivery(row)
	}
}

func flushDelivery(row storage.ExtensionDelivery) {
	entry, ok := extensions.Get(row.ExtensionID)
	if !ok || !entry.Loaded {
		_ = storage.UpdateExtensionDelivery(row.ID, storage.DeliveryStatusFailed, 0, "extension not loaded", row.Attempts+1, time.Now().UTC().Add(time.Hour))
		return
	}
	p := parseQueuedPayload(row.Payload)
	ctx := destContext{
		ProjectID: row.ProjectID,
		UserID:    row.UserID,
		Event: Event{
			Type:      p.EventType,
			EventID:   p.EventID,
			TaskID:    p.TaskID,
			Immediate: true,
		},
		Vars:      p.Vars,
		Message:   p.Message,
		Immediate: true,
	}
	_, err := deliverNow(entry, ctx)
	attempts := row.Attempts + 1
	if err == nil {
		_ = storage.UpdateExtensionDelivery(row.ID, storage.DeliveryStatusSent, 200, "", attempts, time.Now().UTC())
		recordLast(row.ExtensionID, row.ProjectID, row.UserID, nil)
		return
	}
	status := storage.DeliveryStatusFailed
	next := time.Now().UTC().Add(backoff(attempts))
	if retryableStatus(err) && attempts < maxDeliveryAttempts {
		status = storage.DeliveryStatusPending
	}
	_ = storage.UpdateExtensionDelivery(row.ID, status, httpStatusOf(err), err.Error(), attempts, next)
	recordLast(row.ExtensionID, row.ProjectID, row.UserID, err)
}

func flushDigests() {
	dests, err := storage.ListDistinctDigestDestinations()
	if err != nil {
		return
	}
	for _, d := range dests {
		rows, err := storage.ListDigestDeliveries(d.ExtensionID, d.ProjectID, d.UserID)
		if err != nil || len(rows) == 0 {
			continue
		}
		entry, ok := extensions.Get(d.ExtensionID)
		if !ok || !entry.Loaded {
			continue
		}
		ids := make([]int64, 0, len(rows))
		project := ""
		for _, r := range rows {
			ids = append(ids, r.ID)
			p := parseQueuedPayload(r.Payload)
			if project == "" {
				project = p.Vars["project"]
			}
		}
		if project == "" {
			project = "project"
		}
		msg := Interpolate("{count} events in {project}", map[string]string{
			"count":   itoa(len(rows)),
			"project": project,
		})
		ctx := destContext{
			ProjectID: d.ProjectID,
			UserID:    d.UserID,
			Event:     Event{Type: EventTaskUpdated, Immediate: true, EventID: d.EventID},
			Vars:      map[string]string{"project": project, "name": msg, "count": itoa(len(rows))},
			Message:   msg,
			Immediate: true,
		}
		_, err = deliverNow(entry, ctx)
		if err != nil {
			log.Printf("hooks: digest flush %s: %v", d.ExtensionID, err)
			continue
		}
		_ = storage.MarkDeliveriesStatus(ids, storage.DeliveryStatusSent)
		recordLast(d.ExtensionID, d.ProjectID, d.UserID, nil)
	}
}

func backoff(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	d := time.Duration(attempts*attempts) * 15 * time.Second
	if d > 30*time.Minute {
		return 30 * time.Minute
	}
	return d
}
