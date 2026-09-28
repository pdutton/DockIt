package service

import (
	"cmp"
	"slices"

	"github.com/pdutton/DockIt/internal/model"
)

// NewTask is the input for CreateTask.
type NewTask struct {
	Title       string
	Type        string // defaults to task
	Description string
	Owner       string // defaults to the creator
	State       string // defaults to new
	Substate    string
	Priority    int // defaults to 3
	FoundIn     string
	ResolvedIn  string
}

// TaskPatch is the input for UpdateTask.  Nil fields are left as they are.
type TaskPatch struct {
	Title       *string
	Type        *string
	Description *string
	Owner       *string
	State       *string
	Substate    *string
	Priority    *int
	FoundIn     *string
	ResolvedIn  *string
}

// TaskFilter selects tasks.  Zero fields match everything.
type TaskFilter struct {
	States   []string // any of these
	Owner    string
	Priority int
	// PriorityOrHigher matches Priority or higher, that is, a number no
	// greater, instead of exactly Priority.
	PriorityOrHigher bool
}

// TaskSort orders a task list.
type TaskSort int

const (
	SortByID       TaskSort = iota // task number, ascending
	SortByPriority                 // highest priority (1) first, then by number
	SortByModified                 // most recently modified first, then by number
)

// Tasks returns the tasks of project pid that match f, in order.
func (s *Service) Tasks(actor, pid string, f TaskFilter, order TaskSort) ([]*model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	if s.x.Project(pid) == nil {
		return nil, ErrNotFound
	}
	tasks := slices.DeleteFunc(s.x.Tasks(pid), func(t *model.Task) bool {
		return len(f.States) > 0 && !slices.Contains(f.States, t.State) ||
			f.Owner != "" && t.Owner != f.Owner ||
			f.Priority != 0 && (f.PriorityOrHigher && t.Priority > f.Priority ||
				!f.PriorityOrHigher && t.Priority != f.Priority)
	})
	// Tasks come from the index in number order, so a stable sort keeps
	// that as the tie-breaker.
	switch order {
	case SortByPriority:
		slices.SortStableFunc(tasks, func(a, b *model.Task) int { return cmp.Compare(a.Priority, b.Priority) })
	case SortByModified:
		slices.SortStableFunc(tasks, func(a, b *model.Task) int { return b.Modified.Compare(a.Modified) })
	}
	return tasks, nil
}

// Task returns task tid.
func (s *Service) Task(actor, tid string) (*model.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	t := s.x.Task(tid)
	if t == nil {
		return nil, ErrNotFound
	}
	return t, nil
}

// CreateTask creates a task in project pid, assigning it the next number.
// Members and admins.
func (s *Service) CreateTask(actor, pid string, in NewTask) (*model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, err := s.authorize(actor, editors)
	if err != nil {
		return nil, err
	}
	if s.x.Project(pid) == nil {
		return nil, ErrNotFound
	}
	now := s.now()
	t := &model.Task{
		ID:          model.TaskID(pid, s.x.NextTaskNumber(pid)),
		Version:     1,
		Title:       in.Title,
		Type:        cmp.Or(in.Type, model.DefaultTaskType),
		Description: in.Description,
		Creator:     u.ID,
		Owner:       cmp.Or(in.Owner, u.ID),
		State:       cmp.Or(in.State, model.TaskNew),
		Substate:    in.Substate,
		Priority:    cmp.Or(in.Priority, model.DefaultPriority),
		FoundIn:     in.FoundIn,
		ResolvedIn:  in.ResolvedIn,
		Created:     now,
		Modified:    now,
	}
	if err := validate(t.Validate(), all); err != nil {
		return nil, err
	}
	if err := s.checkOwner(t.Owner); err != nil {
		return nil, err
	}
	if err := s.store.WriteTask(t); err != nil {
		return nil, err
	}
	s.x.PutTask(t)
	return t.Clone(), nil
}

// UpdateTask changes a task.  version is the version the change is based on.
// Members and admins.
//
// Moving to a state without substates clears the substate.  Moving to a
// state with substates requires one, unless the task already has a valid
// one for that state.
func (s *Service) UpdateTask(actor, tid string, version int, patch TaskPatch) (*model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, editors); err != nil {
		return nil, err
	}
	t := s.x.Task(tid)
	if t == nil {
		return nil, ErrNotFound
	}
	if err := checkVersion(version, t.Version, t); err != nil {
		return nil, err
	}

	changed := fieldSet{}
	set(&t.Title, patch.Title, "title", changed)
	set(&t.Type, patch.Type, "type", changed)
	set(&t.Description, patch.Description, "description", changed)
	set(&t.Owner, patch.Owner, "owner", changed)
	set(&t.State, patch.State, "state", changed)
	set(&t.Substate, patch.Substate, "substate", changed)
	set(&t.Priority, patch.Priority, "priority", changed)
	set(&t.FoundIn, patch.FoundIn, "found_in", changed)
	set(&t.ResolvedIn, patch.ResolvedIn, "resolved_in", changed)
	if changed["state"] && patch.Substate == nil {
		if _, hasSubs := model.Substates[t.State]; !hasSubs && t.Substate != "" {
			t.Substate = ""
			changed["substate"] = true
		}
	}
	if len(changed) == 0 {
		return t, nil
	}
	if err := validate(t.Validate(), changed.has); err != nil {
		return nil, err
	}
	// A deactivated user may stay the owner, but cannot be newly assigned.
	if changed["owner"] {
		if err := s.checkOwner(t.Owner); err != nil {
			return nil, err
		}
	}

	t.Version++
	t.Modified = s.now()
	if err := s.store.WriteTask(t); err != nil {
		return nil, err
	}
	s.x.PutTask(t)
	return t.Clone(), nil
}

// checkOwner checks a newly assigned owner.
func (s *Service) checkOwner(uid string) error {
	u := s.x.User(uid)
	switch {
	case u == nil:
		return invalid("owner", "user %q does not exist", uid)
	case !u.Active:
		return invalid("owner", "user %q is deactivated", uid)
	}
	return nil
}
