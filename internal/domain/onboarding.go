package domain

import (
	"context"
	"strings"

	"GoTodo/internal/mailer"
	"GoTodo/internal/storage"
)

// OnboardingStep is one item on the getting-started checklist.
type OnboardingStep struct {
	ID          string
	Label       string
	Description string
	Href        string
	Done        bool
	AdminOnly   bool
}

// Onboarding is a user's getting-started checklist.
type Onboarding struct {
	Dismissed bool
	Complete  bool
	Steps     []OnboardingStep
}

// GetOnboardingForUser builds the checklist from what the user (and, for site
// admins, the site) has already done. Agents and the system account get none.
func GetOnboardingForUser(ctx context.Context, userID int) (*Onboarding, error) {
	_ = ctx
	if storage.IsAgentUser(userID) || storage.IsSystemUser(userID) {
		return &Onboarding{Dismissed: true, Complete: true, Steps: []OnboardingStep{}}, nil
	}
	f, err := storage.GetOnboardingFacts(userID)
	if err != nil {
		return nil, err
	}
	company := storage.IsCompanyMode()
	canCreateProject := storage.UserHasSitePermission(userID, storage.SitePermProjectsCreate) ||
		storage.UserHasSitePermission(userID, storage.SitePermAdmin)

	steps := []OnboardingStep{}
	if canCreateProject {
		steps = append(steps, OnboardingStep{
			ID: "project", Label: "Create a project",
			Description: "Projects hold related tasks. Use a board for team work or a simple list.",
			Href:        "/projects", Done: f.HasProject,
		})
	} else {
		steps = append(steps, OnboardingStep{
			ID: "project", Label: "Join a project",
			Description: "Ask a project manager to add you, or open a project shared with you.",
			Href:        "/projects", Done: f.HasProject,
		})
	}
	steps = append(steps,
		OnboardingStep{
			ID: "task", Label: "Add a task",
			Description: "Type in the quick-add bar on your task list. Add a due date to see it in the planner.",
			Href:        "/", Done: f.HasTask,
		},
		OnboardingStep{
			ID: "complete", Label: "Finish a task",
			Description: "Tick it off in the list, or drag a card to Done on a board.",
			Href:        "/planner", Done: f.CompletedTask,
		},
	)
	if f.OwnsProject || canCreateProject {
		steps = append(steps, OnboardingStep{
			ID: "teammate", Label: "Work with someone",
			Description: "Add a teammate to one of your projects from its Sharing settings.",
			Href:        "/projects", Done: f.HasTeammate,
		})
	}

	if storage.UserHasSitePermission(userID, storage.SitePermAdmin) {
		settings, err := storage.GetSiteSettings()
		if err != nil {
			return nil, err
		}
		named := settings != nil && strings.TrimSpace(settings.SiteName) != "" && settings.SiteName != "GoTodo"
		mail := settings != nil && mailer.Configured(storage.SiteEmailConfig(settings))
		peopleHref := "/admin/invites"
		if company {
			peopleHref = "/admin/users"
		}
		steps = append(steps,
			OnboardingStep{
				ID: "site_name", Label: "Name your site", AdminOnly: true,
				Description: "Shown in the header, emails, and API key prefixes.",
				Href:        "/admin", Done: named,
			},
			OnboardingStep{
				ID: "email", Label: "Set up outgoing email", AdminOnly: true,
				Description: "Needed for invites, password resets, and notification emails.",
				Href:        "/admin", Done: mail,
			},
			OnboardingStep{
				ID: "people", Label: "Bring in your people", AdminOnly: true,
				Description: "Invite teammates or create their accounts.",
				Href:        peopleHref, Done: f.SiteUsers > 1,
			},
		)
	}

	complete := true
	for _, s := range steps {
		if !s.Done {
			complete = false
			break
		}
	}
	return &Onboarding{Dismissed: f.DismissedAt != nil, Complete: complete, Steps: steps}, nil
}

// SetOnboardingDismissedForUser hides or brings back the checklist.
func SetOnboardingDismissedForUser(ctx context.Context, userID int, dismissed bool) error {
	_ = ctx
	return storage.SetOnboardingDismissed(userID, dismissed)
}
