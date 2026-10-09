package hooks

import (
	"log"
	"strings"
	"time"

	"GoTodo/internal/extensions"

	"github.com/google/uuid"
)

// Prepared holds deliveries resolved ahead of a change that deletes their own
// destination settings (project.deleted). Prepare it while the project still
// exists, then Send it only once the change has committed.
type Prepared struct {
	items []preparedDelivery
}

type preparedDelivery struct {
	entry extensions.Entry
	ctx   destContext
}

// Prepare resolves ev against every loaded extension, capturing destination
// URLs and secrets now. Nothing is sent and nothing is written.
func Prepare(ev Event) *Prepared {
	p := &Prepared{}
	if ev.TaskID <= 0 && ev.Snapshot == nil && ev.ProjectID <= 0 && !ev.isSiteEvent() {
		return p
	}
	if ev.EventID == "" {
		ev.EventID = uuid.NewString()
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	collect := func(entry extensions.Entry, ctx destContext) {
		secrets, err := loadDestSecrets(entry.Manifest, ctx.ProjectID, ctx.UserID)
		if err != nil {
			log.Printf("hooks: prepare %s project=%d: %v", entry.ID, ctx.ProjectID, err)
			return
		}
		if strings.TrimSpace(secrets.URL) == "" {
			return
		}
		ctx.Secrets = secrets
		p.items = append(p.items, preparedDelivery{entry: entry, ctx: ctx})
	}
	for _, entry := range extensions.LoadedEntries() {
		deliverToExtension(entry, ev, collect)
	}
	return p
}

// Len reports how many deliveries were prepared.
func (p *Prepared) Len() int {
	if p == nil {
		return 0
	}
	return len(p.items)
}

// Send delivers every prepared item immediately. Quiet hours, digests, and the
// retry queue are skipped: each of those re-reads destination settings later,
// after they have been deleted. Safe to call from a goroutine.
func (p *Prepared) Send() {
	if p == nil {
		return
	}
	for _, it := range p.items {
		if _, err := deliverNow(it.entry, it.ctx); err != nil {
			log.Printf("hooks: %s %s project=%d: %v", it.entry.ID, it.ctx.Event.Type, it.ctx.ProjectID, err)
		}
	}
}
