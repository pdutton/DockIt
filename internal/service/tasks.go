package service

import (
	"cmp"
	"maps"
	"math"
	"slices"
	"strings"

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
	Area        string
	FoundIn     string
	ResolvedIn  string
	URLs        model.URLs
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
	Area        *string
	FoundIn     *string
	ResolvedIn  *string
	URLs        *model.URLs
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

// TaskSort orders a task list.  The zero value is by task number.
type TaskSort struct {
	By TaskSortKey
	// Reverse turns the order round.  Tasks that tie still come in task
	// number order.
	Reverse bool
}

// TaskSortKey is what a task list is ordered by.  Ties are broken by task
// number.
type TaskSortKey int

const (
	SortByID       TaskSortKey = iota // task number, ascending
	SortByPriority                    // highest priority (1) first
	SortByModified                    // most recently modified first
	SortByTitle                       // title, A to Z, ignoring case
	SortByOwner                       // owner's user ID, A to Z
	SortByState                       // state, then substate, in built-in order; unknown ones last
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
	var by func(a, b *model.Task) int
	switch order.By {
	default: // SortByID: the index's order
		if order.Reverse {
			slices.Reverse(tasks)
		}
		return tasks, nil
	case SortByPriority:
		by = func(a, b *model.Task) int { return cmp.Compare(a.Priority, b.Priority) }
	case SortByModified:
		by = func(a, b *model.Task) int { return b.Modified.Compare(a.Modified) }
	case SortByTitle:
		by = func(a, b *model.Task) int { return cmp.Compare(strings.ToLower(a.Title), strings.ToLower(b.Title)) }
	case SortByOwner:
		by = func(a, b *model.Task) int { return cmp.Compare(a.Owner, b.Owner) }
	case SortByState:
		by = compareStates
	}
	if order.Reverse {
		forward := by
		by = func(a, b *model.Task) int { return forward(b, a) }
	}
	// Tasks come from the index in number order, so a stable sort keeps
	// that as the tie-breaker, whichever way round the list is.
	slices.SortStableFunc(tasks, by)
	return tasks, nil
}

// compareStates orders tasks by state, then by substate, each in built-in
// order.  Unknown ones, which only old data can hold, come after the known
// ones, in the order of their raw values.
func compareStates(a, b *model.Task) int {
	return cmp.Or(
		cmp.Compare(enumRank(model.TaskStates, a.State), enumRank(model.TaskStates, b.State)),
		cmp.Compare(a.State, b.State),
		cmp.Compare(enumRank(model.Substates[a.State], a.Substate), enumRank(model.Substates[b.State], b.Substate)),
		cmp.Compare(a.Substate, b.Substate),
	)
}

// enumRank returns the position of id in e, with unknown ids after every
// known one.  A nil e has no values, so every id ranks the same.
func enumRank(e *model.Enum, id string) int {
	if e == nil {
		return 0
	}
	if i := e.Index(id); i >= 0 {
		return i
	}
	return math.MaxInt
}

// TaskCounts returns the number of tasks in each project whose state is one
// of states.  Projects with no such tasks are left out.
func (s *Service) TaskCounts(actor string, states []string) (map[string]int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	return s.x.TaskCounts(states), nil
}

// Areas returns the areas that tasks of project pid belong to, each once,
// in CompareAreas order.
func (s *Service) Areas(actor, pid string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	if s.x.Project(pid) == nil {
		return nil, ErrNotFound
	}
	seen := map[string]bool{}
	var areas []string
	for _, t := range s.x.Tasks(pid) {
		if t.Area != "" && !seen[t.Area] {
			seen[t.Area] = true
			areas = append(areas, t.Area)
		}
	}
	slices.SortFunc(areas, CompareAreas)
	return areas, nil
}

// CompareAreas orders area names alphabetically, ignoring case.  Names that
// differ only in case are different areas, ordered by their exact text.
func CompareAreas(a, b string) int {
	return cmp.Or(cmp.Compare(strings.ToLower(a), strings.ToLower(b)), cmp.Compare(a, b))
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
		Area:        in.Area,
		FoundIn:     in.FoundIn,
		ResolvedIn:  in.ResolvedIn,
		URLs:        compactURLs(in.URLs),
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
	return s.patchTask(t, patch)
}

// TransitionTask makes the quick transition with id on task tid, which must
// be in the transition's from state.  version is the version the change is
// based on.  Members and admins.
func (s *Service) TransitionTask(actor, tid string, version int, id string) (*model.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, editors); err != nil {
		return nil, err
	}
	t := s.x.Task(tid)
	tr, ok := model.TransitionByID(id)
	if t == nil || !ok {
		return nil, ErrNotFound
	}
	if err := checkVersion(version, t.Version, t); err != nil {
		return nil, err
	}
	if t.State != tr.From {
		return nil, ErrWrongState
	}
	return s.patchTask(t, TaskPatch{State: &tr.To, Substate: &tr.Substate})
}

// patchTask applies patch to t, a copy of a task from the index, and writes
// it if anything changed.  The caller must hold s.mu for writing.
func (s *Service) patchTask(t *model.Task, patch TaskPatch) (*model.Task, error) {
	changed := fieldSet{}
	set(&t.Title, patch.Title, "title", changed)
	set(&t.Type, patch.Type, "type", changed)
	set(&t.Description, patch.Description, "description", changed)
	set(&t.Owner, patch.Owner, "owner", changed)
	set(&t.State, patch.State, "state", changed)
	set(&t.Substate, patch.Substate, "substate", changed)
	set(&t.Priority, patch.Priority, "priority", changed)
	set(&t.Area, patch.Area, "area", changed)
	set(&t.FoundIn, patch.FoundIn, "found_in", changed)
	set(&t.ResolvedIn, patch.ResolvedIn, "resolved_in", changed)
	if patch.URLs != nil {
		before := t.URLs
		t.URLs = compactURLs(*patch.URLs)
		if !maps.EqualFunc(before, t.URLs, slices.Equal) {
			changed["urls"] = true
		}
	}
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
