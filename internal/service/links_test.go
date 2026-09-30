package service

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

// relations lists a task's links as "relation other".
func relations(t *testing.T, s *Service, tid string) []string {
	t.Helper()
	ls, err := s.Links("view", tid)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, l := range ls {
		out = append(out, l.Relation+" "+l.Task)
	}
	return out
}

func TestLinks(t *testing.T) {
	f := newFixture(t)
	s := f.s
	_, err := s.CreateProject("admin", NewProject{ID: "API", Name: "API"})
	f.ok(err)
	for _, pid := range []string{"WEB", "WEB", "WEB", "API"} {
		_, err := s.CreateTask("mem", pid, NewTask{Title: "T"})
		f.ok(err)
	}
	if got := relations(t, s, "WEB-1"); len(got) != 0 {
		t.Errorf("new task has links %v", got)
	}

	f.tick()
	l, created, err := s.AddLink("mem", "WEB-1", "blocked_by", "WEB-2")
	f.ok(err)
	if !created || l.Relation != "blocked_by" || l.Task != "WEB-2" || l.Creator != "mem" || !l.Created.Equal(f.clock) {
		t.Errorf("AddLink = %+v, %v", l, created)
	}
	// Adding the same link again, from either end, changes nothing.
	_, created, err = s.AddLink("mem2", "WEB-2", "blocks", "WEB-1")
	f.ok(err)
	if created {
		t.Error("the same link, added from the other end, was created again")
	}
	f.ok2(s.AddLink("mem", "WEB-1", "related", "WEB-3"))
	f.ok2(s.AddLink("mem", "WEB-3", "related", "WEB-1"))
	f.ok2(s.AddLink("mem", "WEB-1", "duplicates", "API-1")) // across projects
	f.ok2(s.AddLink("mem", "WEB-3", "dependency_of", "WEB-1"))
	f.ok2(s.AddLink("mem", "WEB-1", "conflicts", "WEB-2")) // a second type for the same pair

	want := []string{"blocked_by WEB-2", "depends_on WEB-3", "duplicates API-1", "related WEB-3", "conflicts WEB-2"}
	if got := relations(t, s, "WEB-1"); !slices.Equal(got, want) {
		t.Errorf("WEB-1 links = %v, want %v", got, want)
	}
	want = []string{"blocks WEB-1", "conflicts WEB-1"}
	if got := relations(t, s, "WEB-2"); !slices.Equal(got, want) {
		t.Errorf("WEB-2 links = %v, want %v", got, want)
	}
	want = []string{"duplicated_by WEB-1"}
	if got := relations(t, s, "API-1"); !slices.Equal(got, want) {
		t.Errorf("API-1 links = %v, want %v", got, want)
	}
	got, err := s.Link("view", "WEB-2", "blocks", "WEB-1")
	f.ok(err)
	if got.Relation != "blocks" || got.Task != "WEB-1" || got.Creator != "mem" {
		t.Errorf("Link = %+v", got)
	}
	_, err = s.Link("view", "WEB-2", "blocked_by", "WEB-1")
	wantErr(t, err, ErrNotFound)

	// Links never touch the tasks.
	task, _ := s.Task("view", "WEB-1")
	if task.Version != 1 || !task.Modified.Equal(t0) {
		t.Errorf("linking changed the task: version %d, modified %v", task.Version, task.Modified)
	}

	// The file holds each link once, in stored order.
	data, err := os.ReadFile(filepath.Join(f.root, store.LinksFile))
	f.ok(err)
	wantFile := `- a: WEB-1
  type: conflicts
  b: WEB-2
  creator: mem
  created: 2026-09-27T12:01:00Z
- a: WEB-1
  type: depends
  b: WEB-3
  creator: mem
  created: 2026-09-27T12:01:00Z
- a: WEB-1
  type: duplicates
  b: API-1
  creator: mem
  created: 2026-09-27T12:01:00Z
- a: WEB-1
  type: related
  b: WEB-3
  creator: mem
  created: 2026-09-27T12:01:00Z
- a: WEB-2
  type: blocks
  b: WEB-1
  creator: mem
  created: 2026-09-27T12:01:00Z
`
	if string(data) != wantFile {
		t.Errorf("links.yaml =\n%s\nwant\n%s", data, wantFile)
	}
	if x := f.reload(); len(x.Links()) != 5 {
		t.Errorf("reloaded links = %v", x.Links())
	}

	// Removing works from either end.
	f.ok(s.RemoveLink("mem2", "WEB-1", "blocked_by", "WEB-2"))
	f.ok(s.RemoveLink("mem2", "WEB-1", "related", "WEB-3"))
	wantErr(t, s.RemoveLink("mem", "WEB-2", "blocks", "WEB-1"), ErrNotFound)
	wantErr(t, s.RemoveLink("mem", "WEB-1", "nonsense", "WEB-2"), ErrNotFound)
	wantErr(t, s.RemoveLink("mem", "WEB-9", "related", "WEB-2"), ErrNotFound)
	// WEB-3 is a dependency of WEB-1, not the other way round.
	wantErr(t, s.RemoveLink("mem", "WEB-3", "depends_on", "WEB-1"), ErrNotFound)
	want = []string{"depends_on WEB-3", "duplicates API-1", "conflicts WEB-2"}
	if got := relations(t, s, "WEB-1"); !slices.Equal(got, want) {
		t.Errorf("after removing, WEB-1 links = %v, want %v", got, want)
	}
	f.reload()
}

// ok2 fails the test if a call returning a value and an error failed.
func (f *fixture) ok2(_ any, _ bool, err error) {
	f.t.Helper()
	f.ok(err)
}

func TestLinkRules(t *testing.T) {
	f := newFixture(t)
	s := f.s
	for range 2 {
		_, err := s.CreateTask("mem", "WEB", NewTask{Title: "T"})
		f.ok(err)
	}

	_, _, err := s.AddLink("mem", "WEB-1", "blocks", "WEB-9")
	wantInvalid(t, err, "task")
	_, _, err = s.AddLink("mem", "WEB-1", "blocks", "")
	wantInvalid(t, err, "task")
	_, _, err = s.AddLink("mem", "WEB-1", "blocks", "web-2")
	wantInvalid(t, err, "task")
	_, _, err = s.AddLink("mem", "WEB-1", "friends", "WEB-2")
	wantInvalid(t, err, "type")
	_, _, err = s.AddLink("mem", "WEB-1", "depends", "WEB-2") // a link type, not a relation
	wantInvalid(t, err, "type")
	_, _, err = s.AddLink("mem", "WEB-1", "related", "WEB-1")
	wantInvalid(t, err, "b")
	_, _, err = s.AddLink("mem", "WEB-9", "blocks", "WEB-1")
	wantErr(t, err, ErrNotFound)
	_, err = s.Links("view", "WEB-9")
	wantErr(t, err, ErrNotFound)

	// Viewers may read links but not change them.
	_, _, err = s.AddLink("view", "WEB-1", "blocks", "WEB-2")
	wantErr(t, err, ErrForbidden)
	f.ok2(s.AddLink("admin", "WEB-1", "blocks", "WEB-2"))
	wantErr(t, s.RemoveLink("view", "WEB-1", "blocks", "WEB-2"), ErrForbidden)

	// Links are informational: a blocked task can still be completed, and
	// the links stay.  (A contradictory pair is allowed too.)
	f.ok2(s.AddLink("mem", "WEB-2", "blocks", "WEB-1"))
	_, err = s.UpdateTask("mem", "WEB-2", 1, TaskPatch{State: ptr(model.TaskComplete), Substate: ptr(model.SubstateDone)})
	f.ok(err)
	if got := relations(t, s, "WEB-2"); len(got) != 2 {
		t.Errorf("WEB-2 links = %v", got)
	}
	f.reload()
}

func TestLinksAfterReopen(t *testing.T) {
	f := newFixture(t)
	for range 2 {
		_, err := f.s.CreateTask("mem", "WEB", NewTask{Title: "T"})
		f.ok(err)
	}
	f.ok2(f.s.AddLink("mem", "WEB-1", "depends_on", "WEB-2"))
	f.s.Close()

	s, _, err := Open(f.root, nil)
	f.ok(err)
	defer s.Close()
	if got := relations(t, s, "WEB-2"); !slices.Equal(got, []string{"dependency_of WEB-1"}) {
		t.Errorf("after reopening, WEB-2 links = %v", got)
	}
}
