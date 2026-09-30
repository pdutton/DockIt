package service

import (
	"cmp"
	"slices"
	"time"

	"github.com/pdutton/DockIt/internal/model"
)

// Links are informational only: they place no limits on state changes.
// They live in links.yaml, not in any task, so adding or removing one never
// changes a task's version or modified time.  A link has no version, since
// it can only be added or removed.

// TaskLink is a link as seen from one of its tasks.
type TaskLink struct {
	Relation string    `json:"type"` // a model.Relation ID, from this task's side
	Task     string    `json:"task"` // the other task
	Creator  string    `json:"creator"`
	Created  time.Time `json:"created"`
}

// taskLink shows link l from task tid's side.
func taskLink(tid string, l model.Link) TaskLink {
	reverse, other := l.B == tid, l.B
	if reverse {
		other = l.A
	}
	return TaskLink{Relation: model.RelationOf(l.Type, reverse).ID, Task: other, Creator: l.Creator, Created: l.Created}
}

// relationOrder is a relation's position in display order, with unknown
// relations last.
func relationOrder(id string) int {
	i := slices.IndexFunc(model.Relations, func(r model.Relation) bool { return r.ID == id })
	if i < 0 {
		return len(model.Relations)
	}
	return i
}

// Links returns the links to and from task tid, seen from its side, ordered
// by relation and then by the other task.  Anyone.
func (s *Service) Links(actor, tid string) ([]TaskLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	if s.x.Task(tid) == nil {
		return nil, ErrNotFound
	}
	out := []TaskLink{}
	for _, l := range s.x.TaskLinks(tid) {
		out = append(out, taskLink(tid, l))
	}
	slices.SortFunc(out, func(a, b TaskLink) int {
		return cmp.Or(
			cmp.Compare(relationOrder(a.Relation), relationOrder(b.Relation)),
			cmp.Compare(a.Relation, b.Relation),
			model.CompareTaskIDs(a.Task, b.Task),
		)
	})
	return out, nil
}

// Link returns the link of relation rel from task tid to task other.  Anyone.
func (s *Service) Link(actor, tid, rel, other string) (*TaskLink, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	_, i, err := s.findLink(tid, rel, other)
	if err != nil {
		return nil, err
	}
	tl := taskLink(tid, s.x.Links()[i])
	return &tl, nil
}

// AddLink links task tid to task other with relation rel, such as
// "blocked_by".  Both tasks must exist, and may be in different projects.
// Adding a link that already exists changes nothing; created reports whether
// the link is new.  Members and admins.
func (s *Service) AddLink(actor, tid, rel, other string) (link *TaskLink, created bool, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, err := s.authorize(actor, editors)
	if err != nil {
		return nil, false, err
	}
	if s.x.Task(tid) == nil {
		return nil, false, ErrNotFound
	}
	r, ok := model.RelationByID(rel)
	if !ok {
		return nil, false, invalid("type", "unknown link type %q", rel)
	}
	if other == "" {
		return nil, false, invalid("task", "required")
	}
	if s.x.Task(other) == nil {
		return nil, false, invalid("task", "task %q does not exist", other)
	}

	l := model.NewLink(tid, r, other)
	links := s.x.Links()
	if i := slices.IndexFunc(links, l.Same); i >= 0 {
		existing := taskLink(tid, links[i])
		return &existing, false, nil
	}
	l.Creator, l.Created = u.ID, s.now()
	if err := validate(l.Validate(), all); err != nil {
		return nil, false, err
	}
	if err := s.writeLinks(append(links, l)); err != nil {
		return nil, false, err
	}
	added := taskLink(tid, l)
	return &added, true, nil
}

// RemoveLink removes the link of relation rel from task tid to task other.
// Members and admins.
func (s *Service) RemoveLink(actor, tid, rel, other string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, editors); err != nil {
		return err
	}
	links, i, err := s.findLink(tid, rel, other)
	if err != nil {
		return err
	}
	return s.writeLinks(slices.Delete(links, i, i+1))
}

// findLink returns every link and the position of the one of relation rel
// from task tid to task other.  The caller must hold s.mu.
func (s *Service) findLink(tid, rel, other string) ([]model.Link, int, error) {
	r, ok := model.RelationByID(rel)
	if !ok || s.x.Task(tid) == nil {
		return nil, 0, ErrNotFound
	}
	links := s.x.Links()
	i := slices.IndexFunc(links, model.NewLink(tid, r, other).Same)
	if i < 0 {
		return nil, 0, ErrNotFound
	}
	return links, i, nil
}

func (s *Service) writeLinks(links []model.Link) error {
	model.SortLinks(links)
	if err := s.store.WriteLinks(links); err != nil {
		return err
	}
	s.x.SetLinks(links)
	return nil
}
