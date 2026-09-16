package extensions

// HookLabel returns a short human label for a core event hook.
func HookLabel(on string) string {
	if label, ok := hookLabels[on]; ok {
		return label
	}
	return on
}

var hookLabels = map[string]string{
	"task.created":          "Task created",
	"task.updated":          "Task updated",
	"task.deleted":          "Task deleted",
	"task.commented":        "Comment posted",
	"task.reordered":        "Tasks reordered",
	"task.claimed":          "Task claimed",
	"task.unclaimed":        "Task unclaimed",
	"task.due_changed":      "Due date changed",
	"task.moved":            "Task moved (project or sprint)",
	"task.project_changed":  "Moved to another project",
	"task.sprint_changed":   "Sprint changed",
	"task.tagged":           "Tags changed",
	"task.overdue":          "Task overdue",
	"task.mentioned":        "You were mentioned",
	"task.completed":        "Task completed",
	"task.reopened":         "Task reopened",
	"task.due_soon":         "Due tomorrow",
	"task.archived":         "Task archived",
	"task.restored":         "Task restored",
	"project.updated":       "Project updated",
	"project.archived":      "Project archived",
	"project.restored":      "Project restored",
	"project.member_joined": "Member joined",
	"project.member_left":   "Member left",
	"sprint.created":        "Sprint created",
	"sprint.started":        "Sprint started",
	"sprint.ended":          "Sprint ended",
	"join.request":          "Join request submitted",
	"join.approved":         "Join request approved",
	"join.denied":           "Join request denied",
}
