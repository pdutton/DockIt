// Package index holds the whole dataset in memory.  It is the read path:
// every read is served from here, and every write updates it only after the
// file is safely on disk.
//
// An Index is not safe for concurrent use; the service layer serializes
// access to it.
package index

import (
	"cmp"
	"slices"

	"github.com/pdutton/DockIt/internal/model"
)

// Index is an in-memory copy of a dataset.  Records go in and come out as
// copies, so callers can never modify the index by accident.
type Index struct {
	meta     model.Meta
	projects map[string]*model.Project
	tasks    map[string]*model.Task
	maxTask  map[string]int // highest task number per project
	users    map[string]*model.User
	auth     map[string]*model.Auth
}

// New returns an empty index for a dataset with metadata meta.
func New(meta model.Meta) *Index {
	return &Index{
		meta:     meta,
		projects: make(map[string]*model.Project),
		tasks:    make(map[string]*model.Task),
		maxTask:  make(map[string]int),
		users:    make(map[string]*model.User),
		auth:     make(map[string]*model.Auth),
	}
}

// Meta returns the dataset metadata.
func (x *Index) Meta() model.Meta { return x.meta }

// Project returns project pid, or nil.
func (x *Index) Project(pid string) *model.Project {
	if p := x.projects[pid]; p != nil {
		return p.Clone()
	}
	return nil
}

// Projects returns all projects, ordered by ID.
func (x *Index) Projects() []*model.Project {
	out := make([]*model.Project, 0, len(x.projects))
	for _, p := range x.projects {
		out = append(out, p.Clone())
	}
	slices.SortFunc(out, func(a, b *model.Project) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// PutProject adds or replaces a project.
func (x *Index) PutProject(p *model.Project) {
	x.projects[p.ID] = p.Clone()
}

// Task returns task tid, or nil.
func (x *Index) Task(tid string) *model.Task {
	if t := x.tasks[tid]; t != nil {
		return t.Clone()
	}
	return nil
}

// Tasks returns the tasks of project pid, ordered by task number.
func (x *Index) Tasks(pid string) []*model.Task {
	type numbered struct {
		n int
		t *model.Task
	}
	var list []numbered
	for _, t := range x.tasks {
		if p, n, err := model.ParseTaskID(t.ID); err == nil && p == pid {
			list = append(list, numbered{n, t})
		}
	}
	slices.SortFunc(list, func(a, b numbered) int { return cmp.Compare(a.n, b.n) })
	out := make([]*model.Task, len(list))
	for i, e := range list {
		out[i] = e.t.Clone()
	}
	return out
}

// PutTask adds or replaces a task.  The task's ID must be well-formed.
func (x *Index) PutTask(t *model.Task) {
	x.tasks[t.ID] = t.Clone()
	if pid, n, err := model.ParseTaskID(t.ID); err == nil && n > x.maxTask[pid] {
		x.maxTask[pid] = n
	}
}

// NextTaskNumber returns the number for the next task in project pid:
// one more than the highest existing number.  Tasks are never deleted, so
// numbers are never reused.
func (x *Index) NextTaskNumber(pid string) int {
	return x.maxTask[pid] + 1
}

// User returns user uid, or nil.
func (x *Index) User(uid string) *model.User {
	if u := x.users[uid]; u != nil {
		return u.Clone()
	}
	return nil
}

// Users returns all users, ordered by ID.
func (x *Index) Users() []*model.User {
	out := make([]*model.User, 0, len(x.users))
	for _, u := range x.users {
		out = append(out, u.Clone())
	}
	slices.SortFunc(out, func(a, b *model.User) int { return cmp.Compare(a.ID, b.ID) })
	return out
}

// PutUser adds or replaces a user.
func (x *Index) PutUser(u *model.User) {
	x.users[u.ID] = u.Clone()
}

// Auth returns user uid's secrets, or nil.
func (x *Index) Auth(uid string) *model.Auth {
	if a := x.auth[uid]; a != nil {
		return a.Clone()
	}
	return nil
}

// PutAuth adds or replaces a user's secrets.
func (x *Index) PutAuth(a *model.Auth) {
	x.auth[a.User] = a.Clone()
}

// TaskCount returns the number of tasks in all projects.
func (x *Index) TaskCount() int { return len(x.tasks) }
