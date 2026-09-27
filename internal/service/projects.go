package service

import (
	"maps"
	"slices"

	"github.com/pdutton/DockIt/internal/model"
)

// NewProject is the input for CreateProject.
type NewProject struct {
	ID          string
	Name        string
	State       string // defaults to planned
	Description string
	URLs        model.URLs
}

// ProjectPatch is the input for UpdateProject.  Nil fields are left as they
// are.
type ProjectPatch struct {
	Name        *string
	State       *string
	Description *string
	URLs        *model.URLs
}

// Projects returns all projects, ordered by ID.
func (s *Service) Projects(actor string) ([]*model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	return s.x.Projects(), nil
}

// Project returns project pid.
func (s *Service) Project(actor, pid string) (*model.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	p := s.x.Project(pid)
	if p == nil {
		return nil, ErrNotFound
	}
	return p, nil
}

// CreateProject creates a project.  Admin only.
func (s *Service) CreateProject(actor string, in NewProject) (*model.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, adminOnly); err != nil {
		return nil, err
	}
	if !model.ValidProjectID(in.ID) {
		return nil, invalid("id", "%q is not a valid project ID: 2-10 characters, A-Z then A-Z or 0-9", in.ID)
	}
	if s.x.Project(in.ID) != nil {
		return nil, ErrExists
	}
	now := s.now()
	p := &model.Project{
		ID:          in.ID,
		Version:     1,
		Name:        in.Name,
		State:       in.State,
		Description: in.Description,
		URLs:        maps.Clone(in.URLs),
		Created:     now,
		Modified:    now,
	}
	if p.State == "" {
		p.State = model.ProjectPlanned
	}
	dropEmptyURLs(p)
	if err := validate(p.Validate(), all); err != nil {
		return nil, err
	}
	if err := s.store.WriteProject(p); err != nil {
		return nil, err
	}
	s.x.PutProject(p)
	return p.Clone(), nil
}

// UpdateProject changes a project.  version is the version the change is
// based on.  Admin only.
func (s *Service) UpdateProject(actor, pid string, version int, patch ProjectPatch) (*model.Project, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, adminOnly); err != nil {
		return nil, err
	}
	p := s.x.Project(pid)
	if p == nil {
		return nil, ErrNotFound
	}
	if err := checkVersion(version, p.Version, p); err != nil {
		return nil, err
	}

	changed := fieldSet{}
	set(&p.Name, patch.Name, "name", changed)
	set(&p.State, patch.State, "state", changed)
	set(&p.Description, patch.Description, "description", changed)
	if patch.URLs != nil {
		before := p.URLs
		p.URLs = maps.Clone(*patch.URLs)
		dropEmptyURLs(p)
		if !maps.EqualFunc(before, p.URLs, slices.Equal) {
			changed["urls"] = true
		}
	}
	if len(changed) == 0 {
		return p, nil
	}
	if err := validate(p.Validate(), changed.has); err != nil {
		return nil, err
	}

	p.Version++
	p.Modified = s.now()
	if err := s.store.WriteProject(p); err != nil {
		return nil, err
	}
	s.x.PutProject(p)
	return p.Clone(), nil
}

// dropEmptyURLs removes URL types with no URLs, so an empty list and an
// absent one are the same thing.
func dropEmptyURLs(p *model.Project) {
	for k, v := range p.URLs {
		if len(v) == 0 {
			delete(p.URLs, k)
		}
	}
	if len(p.URLs) == 0 {
		p.URLs = nil
	}
}

// set applies an optional patch value to a field, recording the field as
// changed only if its value actually changes.
func set[T comparable](field *T, value *T, name string, changed fieldSet) {
	if value != nil && *value != *field {
		*field = *value
		changed[name] = true
	}
}
