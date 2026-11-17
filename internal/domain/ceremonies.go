package domain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"GoTodo/internal/live"
	"GoTodo/internal/storage"
)

// ceremonyFacilitatePerm maps a ceremony to the role permission that runs it.
var ceremonyFacilitatePerm = map[string]string{
	storage.CeremonyRefinement: storage.PermRefinementRun,
	storage.CeremonyRetro:      storage.PermRetroRun,
	storage.CeremonyStandup:    storage.PermStandupRun,
}

const maxCeremonyTitleLen = 120

// CeremonySettings is what the project settings page shows for ceremonies and estimates.
type CeremonySettings struct {
	Kanban                 bool
	Site                   map[string]bool
	Project                map[string]bool
	HoursPerPoint          float64
	NewTasksNeedRefinement bool
	CanManage              bool
	CanFacilitate          map[string]bool
	Retro                  RetroConfig
	Standup                StandupConfig
}

// RetroConfig is a project's retrospective defaults; each session copies them when it starts.
type RetroConfig struct {
	Template  string `json:"template"`
	MaxVotes  int    `json:"max_votes"`
	Anonymous bool   `json:"anonymous"`
}

// StandupConfig is a project's standup defaults.
type StandupConfig struct {
	// SpeakerSeconds is the per-person timer; 0 turns it off.
	SpeakerSeconds int  `json:"speaker_seconds"`
	Shuffle        bool `json:"shuffle"`
}

const (
	maxRetroVotes         = 20
	maxStandupSpeakerSecs = 900
)

func defaultRetroConfig() RetroConfig {
	return RetroConfig{Template: RetroTemplateWentWell, MaxVotes: 5}
}
func defaultStandupConfig() StandupConfig { return StandupConfig{SpeakerSeconds: 120} }

func (c RetroConfig) validate() error {
	if _, ok := retroTemplates[c.Template]; !ok {
		return fmt.Errorf("%w: unknown retro template %q", ErrValidation, c.Template)
	}
	if c.MaxVotes < 0 || c.MaxVotes > maxRetroVotes {
		return fmt.Errorf("%w: max_votes must be 0-%d (0 = unlimited)", ErrValidation, maxRetroVotes)
	}
	return nil
}

func (c StandupConfig) validate() error {
	if c.SpeakerSeconds < 0 || c.SpeakerSeconds > maxStandupSpeakerSecs {
		return fmt.Errorf("%w: speaker_seconds must be 0-%d", ErrValidation, maxStandupSpeakerSecs)
	}
	return nil
}

func loadRetroConfig(projectID int) RetroConfig {
	cfg := defaultRetroConfig()
	if raw, err := storage.GetProjectCeremonyConfig(projectID, storage.CeremonyRetro); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.validate() != nil {
		return defaultRetroConfig()
	}
	return cfg
}

func loadStandupConfig(projectID int) StandupConfig {
	cfg := defaultStandupConfig()
	if raw, err := storage.GetProjectCeremonyConfig(projectID, storage.CeremonyStandup); err == nil {
		_ = json.Unmarshal(raw, &cfg)
	}
	if cfg.validate() != nil {
		return defaultStandupConfig()
	}
	return cfg
}

// Active reports whether kind is usable on this project right now.
func (s CeremonySettings) Active(kind string) bool {
	return s.Kanban && s.Site[kind] && s.Project[kind]
}

// GetCeremonySettings returns the ceremony switches and estimate settings for a project member.
func GetCeremonySettings(_ context.Context, userID, projectID int) (*CeremonySettings, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	site, err := storage.GetSiteCeremonyFeatures()
	if err != nil {
		return nil, err
	}
	opted, err := storage.GetProjectCeremonies(projectID)
	if err != nil {
		return nil, err
	}
	est, err := storage.GetProjectEstimateSettings(projectID)
	if err != nil {
		return nil, err
	}
	out := &CeremonySettings{
		Kanban:                 proj.WorkflowMode == storage.WorkflowKanban,
		Site:                   site,
		Project:                opted,
		HoursPerPoint:          est.HoursPerPoint,
		NewTasksNeedRefinement: est.NewTasksNeedRefinement,
		CanManage:              storage.RoleCanManageProject(projectID, proj.Role),
		CanFacilitate:          map[string]bool{},
		Retro:                  loadRetroConfig(projectID),
		Standup:                loadStandupConfig(projectID),
	}
	for kind, perm := range ceremonyFacilitatePerm {
		out.CanFacilitate[kind] = storage.HasProjectPerm(projectID, proj.Role, perm)
	}
	return out, nil
}

// UpdateCeremonySettingsInput is a partial update; nil fields are left alone.
type UpdateCeremonySettingsInput struct {
	Ceremonies             map[string]bool
	HoursPerPoint          *float64
	NewTasksNeedRefinement *bool
	Retro                  *RetroConfig
	Standup                *StandupConfig
}

// UpdateCeremonySettings changes a project's ceremony opt-ins and estimate settings (project managers only).
func UpdateCeremonySettings(ctx context.Context, userID, projectID int, in UpdateCeremonySettingsInput) (*CeremonySettings, error) {
	if _, err := requireProjectManage(projectID, userID); err != nil {
		return nil, err
	}
	current, err := GetCeremonySettings(ctx, userID, projectID)
	if err != nil {
		return nil, err
	}
	for kind, on := range in.Ceremonies {
		if !storage.ValidCeremonyKind(kind) {
			return nil, fmt.Errorf("%w: unknown ceremony %q", ErrValidation, kind)
		}
		if on && !current.Kanban {
			return nil, fmt.Errorf("%w: ceremonies require a kanban project", ErrValidation)
		}
		if on && !current.Site[kind] {
			return nil, fmt.Errorf("%w: a site admin has not enabled %s", ErrValidation, kind)
		}
	}
	if in.HoursPerPoint != nil {
		if err := ValidateHoursPerPoint(*in.HoursPerPoint); err != nil {
			return nil, err
		}
	}
	if in.Retro != nil {
		if err := in.Retro.validate(); err != nil {
			return nil, err
		}
	}
	if in.Standup != nil {
		if err := in.Standup.validate(); err != nil {
			return nil, err
		}
	}
	if in.Retro != nil {
		if err := saveCeremonyConfig(projectID, storage.CeremonyRetro, *in.Retro); err != nil {
			return nil, err
		}
	}
	if in.Standup != nil {
		if err := saveCeremonyConfig(projectID, storage.CeremonyStandup, *in.Standup); err != nil {
			return nil, err
		}
	}
	for kind, on := range in.Ceremonies {
		if err := storage.SetProjectCeremony(projectID, kind, on); err != nil {
			return nil, err
		}
	}
	if in.HoursPerPoint != nil && *in.HoursPerPoint != current.HoursPerPoint {
		if err := storage.SetProjectHoursPerPoint(projectID, *in.HoursPerPoint); err != nil {
			return nil, err
		}
	}
	if in.NewTasksNeedRefinement != nil {
		if err := storage.SetProjectNewTasksNeedRefinement(projectID, *in.NewTasksNeedRefinement); err != nil {
			return nil, err
		}
	}
	live.AfterProjectChangeLive(userID, projectID, live.TypeProjectUpdated)
	return GetCeremonySettings(ctx, userID, projectID)
}

// SetSiteCeremonies turns ceremonies on or off site-wide. Callers check admin access.
func SetSiteCeremonies(features map[string]bool) error {
	for kind := range features {
		if !storage.ValidCeremonyKind(kind) {
			return fmt.Errorf("%w: unknown ceremony %q", ErrValidation, kind)
		}
	}
	for kind, on := range features {
		if err := storage.SetSiteCeremonyFeature(kind, on); err != nil {
			return err
		}
	}
	return nil
}

// ceremonyAccess is a caller's standing in a project's ceremony.
type ceremonyAccess struct {
	project       *storage.ProjectWithAccess
	canFacilitate bool
}

// requireCeremony checks the viewer can see the project and the ceremony is switched on.
func requireCeremony(projectID, userID int, kind string) (*ceremonyAccess, error) {
	proj, err := storage.GetAccessibleProjectByID(projectID, userID)
	if err != nil {
		return nil, ErrNotFound
	}
	on, err := storage.CeremonyEnabledForProject(projectID, kind)
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, fmt.Errorf("%w: %s is not enabled for this project", ErrForbidden, kind)
	}
	return &ceremonyAccess{
		project:       proj,
		canFacilitate: storage.HasProjectPerm(projectID, proj.Role, ceremonyFacilitatePerm[kind]),
	}, nil
}

func normalizeCeremonyTitle(title, fallback string) (string, error) {
	t := strings.TrimSpace(title)
	if t == "" {
		t = fallback
	}
	if utf8.RuneCountInString(t) > maxCeremonyTitleLen {
		return "", fmt.Errorf("%w: title must be %d characters or less", ErrValidation, maxCeremonyTitleLen)
	}
	return t, nil
}

func mapCeremonyStoreErr(err error) error {
	switch {
	case errors.Is(err, storage.ErrCeremonySessionNotFound):
		return ErrNotFound
	case errors.Is(err, storage.ErrCeremonyRevisionConflict):
		return fmt.Errorf("%w: the session changed; reload and try again", ErrConflict)
	case errors.Is(err, storage.ErrCeremonySessionActive):
		return fmt.Errorf("%w: a session is already running", ErrConflict)
	}
	return err
}

func saveCeremonyConfig(projectID int, kind string, cfg interface{}) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return storage.SetProjectCeremonyConfig(projectID, kind, raw)
}

// loadCeremonySession loads a session of kind in a project, mapping storage errors.
func loadCeremonySession(projectID, sessionID int, kind string) (*storage.CeremonySession, error) {
	s, err := storage.GetCeremonySession(projectID, sessionID)
	if err != nil {
		return nil, mapCeremonyStoreErr(err)
	}
	if s.Kind != kind {
		return nil, ErrNotFound
	}
	return s, nil
}

// requireActiveSession rejects changes to an ended session.
func requireActiveSession(s *storage.CeremonySession) error {
	if s.Status != storage.CeremonySessionActive {
		return fmt.Errorf("%w: this session has ended", ErrConflict)
	}
	return nil
}

// ceremonyStep is the shared facilitator gate: the caller must hold the permission, the session must be
// running, the revision must match, and only the facilitator may act unless the step is a take-over.
func ceremonyStep(acc *ceremonyAccess, s *storage.CeremonySession, userID, revision int, takeOver bool) error {
	if !acc.canFacilitate {
		return fmt.Errorf("%w: your role cannot run this ceremony", ErrForbidden)
	}
	if err := requireActiveSession(s); err != nil {
		return err
	}
	if revision != s.Revision {
		return mapCeremonyStoreErr(storage.ErrCeremonyRevisionConflict)
	}
	if !takeOver && s.FacilitatorID != userID {
		return fmt.Errorf("%w: only the facilitator can drive this session; take over first", ErrForbidden)
	}
	return nil
}

// ceremonyHistory is one finished-or-running session with item counts, for history lists.
type CeremonySessionSummary struct {
	storage.CeremonySession
	Counts map[string]int
}
