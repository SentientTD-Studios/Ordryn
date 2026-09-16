package hooks

import (
	"strconv"
	"strings"
	"time"

	"GoTodo/internal/extensions"
	"GoTodo/internal/storage"
)

func triggerAllowed(triggers []string, eventType string) bool {
	if len(triggers) == 0 {
		return false
	}
	for _, t := range triggers {
		if strings.TrimSpace(t) == eventType {
			return true
		}
	}
	return false
}

func hookDeclared(m extensions.Manifest, eventType string) bool {
	return m.DeclaresHook(eventType)
}

func templateFor(m extensions.Manifest, templates map[string]string, eventType string) string {
	if templates != nil {
		if s, ok := templates[eventType]; ok {
			return s
		}
	}
	if m.Templates != nil {
		if s, ok := m.Templates[eventType]; ok {
			return s
		}
	}
	return ""
}

// destFilter is the filter view of team or member settings.
type destFilter struct {
	Enabled         bool
	Triggers        []string
	Templates       map[string]string
	StatusOnly      bool
	SkipSelf        bool
	MinPriority     int
	TagIDs          []int
	ClaimedOnly     bool
	ClaimedIsMe     bool
	FieldKey        string
	FieldValue      string
	QuietHoursStart string
	QuietHoursEnd   string
	Digest          string
	MentionMap      map[string]string
	SubscriberID    int
	Personal        bool
	Values          map[string]string
}

func destFromProject(m extensions.Manifest, p storage.ExtensionProjectSettings) destFilter {
	d := destFilter{
		Enabled:   p.Enabled,
		Triggers:  p.Triggers,
		Templates: p.Templates,
		Values:    p.Values,
	}
	copyDeclaredFilters(&d, m, storedFilters{
		StatusOnly:      p.StatusOnly,
		SkipSelf:        p.SkipSelf,
		MinPriority:     p.MinPriority,
		TagIDs:          p.TagIDs,
		ClaimedOnly:     p.ClaimedOnly,
		FieldKey:        p.FieldKey,
		FieldValue:      p.FieldValue,
		QuietHoursStart: p.QuietHoursStart,
		QuietHoursEnd:   p.QuietHoursEnd,
		Digest:          p.Digest,
		MentionMap:      p.MentionMap,
	})
	return d
}

func destFromMember(m extensions.Manifest, s storage.ExtensionMemberSettings, userID int, personal bool) destFilter {
	d := destFilter{
		Enabled:      s.Enabled,
		Triggers:     s.Triggers,
		Templates:    s.Templates,
		SubscriberID: userID,
		Personal:     personal,
		Values:       s.Values,
	}
	skipSelf := false
	if m.HasSetting("skip_self") {
		skipSelf = s.SkipSelfOrDefault()
	}
	copyDeclaredFilters(&d, m, storedFilters{
		StatusOnly:      s.StatusOnly,
		SkipSelf:        skipSelf,
		MinPriority:     s.MinPriority,
		TagIDs:          s.TagIDs,
		ClaimedOnly:     s.ClaimedOnly,
		ClaimedIsMe:     s.ClaimedIsMe,
		FieldKey:        s.FieldKey,
		FieldValue:      s.FieldValue,
		QuietHoursStart: s.QuietHoursStart,
		QuietHoursEnd:   s.QuietHoursEnd,
		Digest:          s.Digest,
	})
	return d
}

type storedFilters struct {
	StatusOnly      bool
	SkipSelf        bool
	MinPriority     int
	TagIDs          []int
	ClaimedOnly     bool
	ClaimedIsMe     bool
	FieldKey        string
	FieldValue      string
	QuietHoursStart string
	QuietHoursEnd   string
	Digest          string
	MentionMap      map[string]string
}

func copyDeclaredFilters(d *destFilter, m extensions.Manifest, s storedFilters) {
	if m.HasSetting("status_only") {
		d.StatusOnly = s.StatusOnly
	}
	if m.HasSetting("skip_self") {
		d.SkipSelf = s.SkipSelf
	}
	if m.HasSetting("min_priority") {
		d.MinPriority = s.MinPriority
	}
	if m.HasSetting("tag_ids") {
		d.TagIDs = s.TagIDs
	}
	if m.HasSetting("claimed_only") {
		d.ClaimedOnly = s.ClaimedOnly
	}
	if m.HasSetting("claimed_is_me") {
		d.ClaimedIsMe = s.ClaimedIsMe
	}
	if m.HasSetting("field_key") || m.HasSetting("field_value") || m.HasSetting("field_filter") {
		d.FieldKey = s.FieldKey
		d.FieldValue = s.FieldValue
	}
	if m.HasSetting("quiet_hours_start") {
		d.QuietHoursStart = s.QuietHoursStart
	}
	if m.HasSetting("quiet_hours_end") {
		d.QuietHoursEnd = s.QuietHoursEnd
	}
	if m.HasSetting("digest") {
		d.Digest = s.Digest
	}
	if m.HasSetting("mention_map") {
		d.MentionMap = s.MentionMap
	}
}

// ShouldDeliver reports whether this event should produce an outbound message for a team channel.
func ShouldDeliver(m extensions.Manifest, site storage.ExtensionSettings, project storage.ExtensionProjectSettings, ev Event, projectID int) bool {
	return shouldDeliverDest(m, site, destFromProject(m, project), ev, projectID)
}

func shouldDeliverDest(m extensions.Manifest, site storage.ExtensionSettings, dest destFilter, ev Event, projectID int) bool {
	if !site.Enabled {
		return false
	}
	if !dest.Enabled {
		return false
	}
	if !dest.Personal && !ev.isSiteEvent() && projectID <= 0 {
		return false
	}
	if dest.Personal && projectID > 0 {
		return false
	}
	if !hookDeclared(m, ev.Type) {
		return false
	}
	if !triggerAllowed(dest.Triggers, ev.Type) {
		return false
	}
	if ev.Type == EventTaskUpdated && dest.StatusOnly && !ev.StatusChanged {
		return false
	}
	if dest.SkipSelf && dest.SubscriberID > 0 && ev.ActorID > 0 && ev.ActorID == dest.SubscriberID {
		return false
	}
	if dest.SkipSelf && dest.SubscriberID == 0 && ev.ActorID > 0 && ev.OwnerID > 0 && ev.ActorID == ev.OwnerID && dest.Personal {
		return false
	}
	snap := ev.Snapshot
	if dest.MinPriority > 0 && snap != nil && snap.Priority < dest.MinPriority {
		return false
	}
	if len(dest.TagIDs) > 0 && snap != nil && !tagOverlap(dest.TagIDs, snap.TagIDs) {
		return false
	}
	if dest.ClaimedOnly && snap != nil && snap.ClaimedBy <= 0 {
		return false
	}
	if dest.ClaimedIsMe && dest.SubscriberID > 0 && snap != nil && snap.ClaimedBy != dest.SubscriberID {
		return false
	}
	if ev.Type == EventTaskMentioned && dest.SubscriberID > 0 && !containsInt(ev.MentionedUserIDs, dest.SubscriberID) {
		return false
	}
	if ev.Type == EventProjectMemberLeft && dest.SubscriberID > 0 && ev.MemberID > 0 && dest.SubscriberID == ev.MemberID {
		return false
	}
	if strings.TrimSpace(dest.FieldKey) != "" && snap != nil {
		got := ""
		if snap.CustomFields != nil {
			got = snap.CustomFields[dest.FieldKey]
		}
		want := strings.TrimSpace(dest.FieldValue)
		if want != "" && !strings.EqualFold(strings.TrimSpace(got), want) {
			return false
		}
		changed := false
		for _, c := range ev.Changed {
			if c == "fields" || c == dest.FieldKey {
				changed = true
				break
			}
		}
		if !changed && ev.Type == EventTaskUpdated {
			return false
		}
	}
	return true
}

func shouldDeliverSite(m extensions.Manifest, site storage.ExtensionSettings, ev Event) bool {
	if !site.Enabled || !ev.isSiteEvent() {
		return false
	}
	if !hookDeclared(m, ev.Type) {
		return false
	}
	if len(site.Triggers) == 0 {
		return true
	}
	return triggerAllowed(site.Triggers, ev.Type)
}

func tagOverlap(want, have []int) bool {
	if len(want) == 0 {
		return true
	}
	set := make(map[int]struct{}, len(have))
	for _, id := range have {
		set[id] = struct{}{}
	}
	for _, id := range want {
		if _, ok := set[id]; ok {
			return true
		}
	}
	return false
}

func containsInt(ids []int, want int) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func inQuietHours(start, end, tz string, now time.Time) bool {
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start == "" || end == "" || start == end {
		return false
	}
	loc := time.UTC
	if tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc = l
		}
	}
	now = now.In(loc)
	sh, sm := parseHM(start)
	eh, em := parseHM(end)
	if sh < 0 || eh < 0 {
		return false
	}
	mins := now.Hour()*60 + now.Minute()
	s := sh*60 + sm
	e := eh*60 + em
	if s < e {
		return mins >= s && mins < e
	}
	return mins >= s || mins < e
}

func parseHM(s string) (h, m int) {
	parts := strings.Split(s, ":")
	if len(parts) < 2 {
		return -1, -1
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || h < 0 || h > 23 || m < 0 || m > 59 {
		return -1, -1
	}
	return h, m
}

func digestDelay(kind string) time.Duration {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "hourly":
		return time.Hour
	case "daily":
		return 24 * time.Hour
	default:
		return 0
	}
}
