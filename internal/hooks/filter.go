package hooks

import (
	"strings"

	"GoTodo/internal/mods"
	"GoTodo/internal/storage"
)

func triggerAllowed(triggers []string, eventType string) bool {
	for _, t := range triggers {
		if strings.TrimSpace(t) == eventType {
			return true
		}
	}
	return false
}

func hookDeclared(m mods.Manifest, eventType string) bool {
	for _, h := range m.Hooks {
		if strings.TrimSpace(h.On) == eventType {
			return true
		}
	}
	return false
}

func templateFor(m mods.Manifest, templates map[string]string, eventType string) string {
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

// ShouldDeliver reports whether this event should produce an outbound message.
// Site enabled plus project enabled are both required. Personal tasks (project_id 0) never post.
func ShouldDeliver(m mods.Manifest, site storage.ModSettings, project storage.ModProjectSettings, ev Event, projectID int) bool {
	if !site.Enabled {
		return false
	}
	if projectID <= 0 {
		return false
	}
	if !project.Enabled {
		return false
	}
	if !hookDeclared(m, ev.Type) {
		return false
	}
	if !triggerAllowed(project.Triggers, ev.Type) {
		return false
	}
	if ev.Type == "task.updated" && project.StatusOnly && !ev.StatusChanged {
		return false
	}
	return true
}
