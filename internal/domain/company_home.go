package domain

import (
	"context"
	"sort"

	"GoTodo/internal/storage"
)

// CompanyHomeMaxProjects caps the projects listed on the company home page.
const CompanyHomeMaxProjects = 12

// CompanyHomeProject is one of the caller's projects with its open work.
type CompanyHomeProject struct {
	ID      int
	Name    string
	Slug    string
	Open    int
	Overdue int
}

// CompanyHomeSummary is the live part of the company home page.
type CompanyHomeSummary struct {
	MyWork        storage.MyWorkSummary
	Projects      []CompanyHomeProject
	TotalProjects int
	Teams         []storage.UserTeamSummary
}

// GetCompanyHomeSummary gathers the caller's work, projects and teams. today
// is YYYY-MM-DD in the caller's timezone. Projects with overdue work come
// first, then the busiest; archived projects are left out.
func GetCompanyHomeSummary(ctx context.Context, userID int, today string) (*CompanyHomeSummary, error) {
	_ = ctx
	work, err := storage.GetMyWorkSummary(userID, today)
	if err != nil {
		return nil, err
	}
	projects, err := storage.GetAccessibleProjects(userID)
	if err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(projects))
	for _, p := range projects {
		if !p.Archived {
			ids = append(ids, p.ID)
		}
	}
	counts, err := storage.GetProjectCounts(ids, today)
	if err != nil {
		return nil, err
	}
	list := make([]CompanyHomeProject, 0, len(ids))
	for _, p := range projects {
		if p.Archived {
			continue
		}
		c := counts[p.ID]
		list = append(list, CompanyHomeProject{ID: p.ID, Name: p.Name, Slug: p.Slug, Open: c.Open, Overdue: c.Overdue})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Overdue != list[j].Overdue {
			return list[i].Overdue > list[j].Overdue
		}
		if list[i].Open != list[j].Open {
			return list[i].Open > list[j].Open
		}
		return list[i].Name < list[j].Name
	})
	total := len(list)
	if len(list) > CompanyHomeMaxProjects {
		list = list[:CompanyHomeMaxProjects]
	}
	teams, err := storage.ListUserTeamSummaries(userID)
	if err != nil {
		return nil, err
	}
	return &CompanyHomeSummary{MyWork: work, Projects: list, TotalProjects: total, Teams: teams}, nil
}
