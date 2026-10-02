package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/index"
	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

// fixture is an open service with users admin, mem (member), mem2 (member)
// and view (viewer), and project WEB.  The clock starts at t0 and advances
// one minute per write-ish call to tick.
type fixture struct {
	t     *testing.T
	s     *Service
	root  string
	clock time.Time
	pw    map[string]string // one-time passwords
}

var t0 = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	admin := &model.User{ID: "admin", Version: 1, Name: "Admin", Email: "admin@example.com",
		Role: model.RoleAdmin, Active: true, Created: t0, Modified: t0}
	if err := store.Init(root, admin, &model.Auth{User: "admin", Password: auth.HashPassword("admin-password")}); err != nil {
		t.Fatal(err)
	}
	s, report, err := Open(root, nil)
	if err != nil {
		t.Fatalf("Open: %v %v", err, report)
	}
	f := &fixture{t: t, s: s, root: root, clock: t0, pw: map[string]string{}}
	s.now = func() time.Time { return f.clock }
	t.Cleanup(func() { s.Close() })

	for _, u := range []NewUser{
		{ID: "mem", Name: "Mem", Email: "mem@example.com", Role: model.RoleMember},
		{ID: "mem2", Name: "Mem Two", Email: "mem2@example.com", Role: model.RoleMember},
		{ID: "view", Name: "View", Email: "view@example.com", Role: model.RoleViewer},
	} {
		_, pw, err := s.CreateUser("admin", u)
		f.ok(err)
		f.pw[u.ID] = pw
	}
	_, err = s.CreateProject("admin", NewProject{ID: "WEB", Name: "Website"})
	f.ok(err)
	return f
}

func (f *fixture) ok(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) tick() { f.clock = f.clock.Add(time.Minute) }

func ptr[T any](v T) *T { return &v }

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("err = %v, want %v", err, target)
	}
}

func wantInvalid(t *testing.T, err error, field string) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Errorf("err = %v, want a ValidationError on %s", err, field)
		return
	}
	for _, fe := range ve.Fields {
		if fe.Field == field {
			return
		}
	}
	t.Errorf("err = %v, want a problem with %s", err, field)
}

func wantConflict(t *testing.T, err error) any {
	t.Helper()
	var ce *ConflictError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v, want ConflictError", err)
	}
	return ce.Current
}

// reload checks the dataset on disk is clean and returns it freshly loaded.
func (f *fixture) reload() *index.Index {
	f.t.Helper()
	x, r := index.Load(f.root)
	if !r.OK() || r.Warnings() != 0 {
		f.t.Fatalf("dataset on disk has problems: %v", r.Problems)
	}
	return x
}

func TestOpenRefusesBrokenDataset(t *testing.T) {
	f := newFixture(t)
	f.s.Close()

	// A task owned by a user who does not exist is an error.
	st, err := store.Open(f.root)
	f.ok(err)
	f.ok(st.WriteTask(&model.Task{ID: "WEB-1", Version: 1, Title: "x", Type: model.TypeTask, Creator: "ghost", Owner: "ghost",
		State: model.TaskNew, Priority: 3, Created: t0, Modified: t0}))
	st.Close()

	_, report, err := Open(f.root, nil)
	var oe *OpenError
	if !errors.As(err, &oe) || report.OK() {
		t.Fatalf("Open = %v", err)
	}
	if _, _, err := store.ReadLock(f.root); err == nil {
		t.Error("lock left behind after a refused Open")
	}
}

func TestPermissions(t *testing.T) {
	f := newFixture(t)
	s := f.s

	// Everyone active can read.
	for _, who := range []string{"admin", "mem", "view"} {
		if _, err := s.Projects(who); err != nil {
			t.Errorf("%s cannot list projects: %v", who, err)
		}
	}
	// Unknown actors can do nothing.
	_, err := s.Projects("nobody")
	wantErr(t, err, ErrForbidden)

	// Viewers cannot write.
	_, err = s.CreateTask("view", "WEB", NewTask{Title: "T"})
	wantErr(t, err, ErrForbidden)

	task, err := s.CreateTask("mem", "WEB", NewTask{Title: "T"})
	f.ok(err)
	_, err = s.AddComment("view", task.ID, "hi")
	wantErr(t, err, ErrForbidden)

	// Members cannot manage projects or users.
	_, err = s.CreateProject("mem", NewProject{ID: "API", Name: "API"})
	wantErr(t, err, ErrForbidden)
	_, err = s.UpdateProject("mem", "WEB", 1, ProjectPatch{Name: ptr("x")})
	wantErr(t, err, ErrForbidden)
	_, _, err = s.CreateUser("mem", NewUser{ID: "x1", Name: "X", Email: "x@example.com", Role: model.RoleViewer})
	wantErr(t, err, ErrForbidden)
	_, err = s.ResetPassword("mem", "view")
	wantErr(t, err, ErrForbidden)

	// Deactivated users can do nothing, not even read.
	u, _ := s.User("admin", "mem")
	_, err = s.UpdateUser("admin", "mem", u.Version, UserPatch{Active: ptr(false)})
	f.ok(err)
	_, err = s.Task("mem", task.ID)
	wantErr(t, err, ErrForbidden)
}

func TestCreateProject(t *testing.T) {
	f := newFixture(t)
	s := f.s
	p, err := s.CreateProject("admin", NewProject{ID: "API", Name: "API",
		URLs: model.URLs{model.URLCode: {"https://example.com/api"}, model.URLDoc: {}}})
	f.ok(err)
	if p.State != model.ProjectPlanned || p.Version != 1 || !p.Created.Equal(t0) {
		t.Errorf("project = %+v", p)
	}
	if _, ok := p.URLs[model.URLDoc]; ok {
		t.Error("empty URL list kept")
	}

	_, err = s.CreateProject("admin", NewProject{ID: "API", Name: "Again"})
	wantErr(t, err, ErrExists)
	_, err = s.CreateProject("admin", NewProject{ID: "api", Name: "x"})
	wantInvalid(t, err, "id")
	_, err = s.CreateProject("admin", NewProject{ID: "NOPE", Name: ""})
	wantInvalid(t, err, "name")
	_, err = s.CreateProject("admin", NewProject{ID: "NOPE", Name: "x", State: "someday"})
	wantInvalid(t, err, "state")
	_, err = s.CreateProject("admin", NewProject{ID: "NOPE", Name: "x",
		URLs: model.URLs{model.URLWeb: {"javascript:alert(1)"}}})
	wantInvalid(t, err, "urls.web[0]")
	_, err = s.CreateProject("admin", NewProject{ID: "NOPE", Name: "x",
		URLs: model.URLs{"wiki": {"https://example.com"}}})
	wantInvalid(t, err, "urls.wiki")

	if got := f.reload().Project("API"); got == nil || got.Name != "API" {
		t.Errorf("project on disk = %+v", got)
	}
}

func TestUpdateProject(t *testing.T) {
	f := newFixture(t)
	s := f.s

	_, err := s.UpdateProject("admin", "WEB", 0, ProjectPatch{Name: ptr("New")})
	wantErr(t, err, ErrVersionRequired)
	_, err = s.UpdateProject("admin", "NOPE", 1, ProjectPatch{Name: ptr("New")})
	wantErr(t, err, ErrNotFound)

	f.tick()
	p, err := s.UpdateProject("admin", "WEB", 1, ProjectPatch{Name: ptr("New"), State: ptr(model.ProjectActive)})
	f.ok(err)
	if p.Version != 2 || p.Name != "New" || !p.Modified.Equal(f.clock) || !p.Created.Equal(t0) {
		t.Errorf("project = %+v", p)
	}

	// The second of two edits based on version 1 is rejected, with the
	// current record.
	cur := wantConflict(t, func() error {
		_, err := s.UpdateProject("admin", "WEB", 1, ProjectPatch{Name: ptr("Other")})
		return err
	}())
	if cur.(*model.Project).Name != "New" {
		t.Errorf("conflict carries %+v", cur)
	}

	// A change that changes nothing does not bump the version.
	p, err = s.UpdateProject("admin", "WEB", 2, ProjectPatch{Name: ptr("New"), URLs: &model.URLs{}})
	f.ok(err)
	if p.Version != 2 {
		t.Errorf("no-op update bumped version to %d", p.Version)
	}

	// URLs are replaced as a whole.
	p, err = s.UpdateProject("admin", "WEB", 2, ProjectPatch{URLs: &model.URLs{model.URLWeb: {"https://a.example", "https://b.example"}}})
	f.ok(err)
	p, err = s.UpdateProject("admin", "WEB", 3, ProjectPatch{URLs: &model.URLs{model.URLCode: {"https://c.example"}}})
	f.ok(err)
	if len(p.URLs) != 1 || p.URLs[model.URLCode][0] != "https://c.example" {
		t.Errorf("urls = %v", p.URLs)
	}
	if got := f.reload().Project("WEB"); got.Version != 4 {
		t.Errorf("version on disk = %d", got.Version)
	}
}

func TestCreateTask(t *testing.T) {
	f := newFixture(t)
	s := f.s

	t1, err := s.CreateTask("mem", "WEB", NewTask{Title: "First"})
	f.ok(err)
	if t1.ID != "WEB-1" || t1.Type != model.TypeTask || t1.Creator != "mem" || t1.Owner != "mem" ||
		t1.State != model.TaskNew || t1.Priority != 3 || t1.Version != 1 {
		t.Errorf("task = %+v", t1)
	}
	t2, err := s.CreateTask("admin", "WEB", NewTask{Title: "Second", Type: model.TypeBugfix, FoundIn: "v1.0.4", Owner: "mem2", Priority: 1,
		State: model.TaskComplete, Substate: model.SubstateRejected})
	f.ok(err)
	if t2.ID != "WEB-2" || t2.Type != model.TypeBugfix || t2.FoundIn != "v1.0.4" || t2.Owner != "mem2" || t2.Priority != 1 || t2.Substate != model.SubstateRejected {
		t.Errorf("task = %+v", t2)
	}

	_, err = s.CreateTask("mem", "NOPE", NewTask{Title: "x"})
	wantErr(t, err, ErrNotFound)
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: ""})
	wantInvalid(t, err, "title")
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "x", Type: "chore"})
	wantInvalid(t, err, "type")
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "x", FoundIn: strings.Repeat("1", model.MaxVersionLen+1)})
	wantInvalid(t, err, "found_in")
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "x", ResolvedIn: "1.0\n2"})
	wantInvalid(t, err, "resolved_in")
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "x", State: model.TaskComplete})
	wantInvalid(t, err, "substate")
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "x", Substate: model.SubstateDone})
	wantInvalid(t, err, "substate")
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "x", Priority: 6})
	wantInvalid(t, err, "priority")
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "x", Owner: "ghost"})
	wantInvalid(t, err, "owner")

	// A deactivated user cannot be made the owner.
	u, _ := s.User("admin", "mem2")
	_, err = s.UpdateUser("admin", "mem2", u.Version, UserPatch{Active: ptr(false)})
	f.ok(err)
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "x", Owner: "mem2"})
	wantInvalid(t, err, "owner")

	// Failed creates do not use up numbers.
	t3, err := s.CreateTask("mem", "WEB", NewTask{Title: "Third"})
	f.ok(err)
	if t3.ID != "WEB-3" {
		t.Errorf("third task is %s", t3.ID)
	}
	if x := f.reload(); x.TaskCount() != 3 || x.Task("WEB-2").Owner != "mem2" {
		t.Error("tasks on disk wrong")
	}

	// A task can be closed as a duplicate.
	dup, err := s.CreateTask("mem", "WEB", NewTask{Title: "Again", State: model.TaskComplete, Substate: model.SubstateDuplicate})
	f.ok(err)
	if dup.Substate != model.SubstateDuplicate {
		t.Errorf("task = %+v", dup)
	}
}

func TestUpdateTask(t *testing.T) {
	f := newFixture(t)
	s := f.s
	task, err := s.CreateTask("mem", "WEB", NewTask{Title: "T"})
	f.ok(err)

	_, err = s.UpdateTask("mem", "WEB-1", 0, TaskPatch{Title: ptr("x")})
	wantErr(t, err, ErrVersionRequired)

	// Complete needs a substate; leaving complete clears it.
	_, err = s.UpdateTask("mem", "WEB-1", 1, TaskPatch{State: ptr(model.TaskComplete)})
	wantInvalid(t, err, "substate")
	f.tick()
	task, err = s.UpdateTask("mem2", "WEB-1", 1, TaskPatch{State: ptr(model.TaskComplete), Substate: ptr(model.SubstateDone)})
	f.ok(err)
	if task.Version != 2 || !task.Modified.Equal(f.clock) || task.Creator != "mem" {
		t.Errorf("task = %+v", task)
	}
	task, err = s.UpdateTask("mem", "WEB-1", 2, TaskPatch{State: ptr(model.TaskInProgress)})
	f.ok(err)
	if task.Substate != "" {
		t.Errorf("substate not cleared: %+v", task)
	}

	// Clients cannot set an unknown type.
	_, err = s.UpdateTask("mem", "WEB-1", 3, TaskPatch{Type: ptr("chore")})
	wantInvalid(t, err, "type")

	// Conflicting edit.
	cur := wantConflict(t, func() error {
		_, err := s.UpdateTask("mem2", "WEB-1", 2, TaskPatch{Priority: ptr(1)})
		return err
	}())
	if cur.(*model.Task).Version != 3 {
		t.Errorf("conflict carries %+v", cur)
	}

	// An owner who has since been deactivated can stay the owner, but
	// cannot be newly assigned.
	task, err = s.UpdateTask("mem", "WEB-1", 3, TaskPatch{Owner: ptr("mem2")})
	f.ok(err)
	u, _ := s.User("admin", "mem2")
	_, err = s.UpdateUser("admin", "mem2", u.Version, UserPatch{Active: ptr(false)})
	f.ok(err)
	task, err = s.UpdateTask("mem", "WEB-1", 4, TaskPatch{Title: ptr("Renamed")})
	f.ok(err)
	_, err = s.UpdateTask("mem", "WEB-1", 5, TaskPatch{Owner: ptr("mem")})
	f.ok(err)
	_, err = s.UpdateTask("mem", "WEB-1", 6, TaskPatch{Owner: ptr("mem2")})
	wantInvalid(t, err, "owner")
}

func TestTransitionTask(t *testing.T) {
	f := newFixture(t)
	s := f.s
	_, err := s.CreateTask("mem", "WEB", NewTask{Title: "T"})
	f.ok(err)
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "Later"})
	f.ok(err)

	_, err = s.TransitionTask("view", "WEB-1", 1, "start")
	wantErr(t, err, ErrForbidden)
	_, err = s.TransitionTask("mem", "WEB-9", 1, "start")
	wantErr(t, err, ErrNotFound)
	_, err = s.TransitionTask("mem", "WEB-1", 1, "finish")
	wantErr(t, err, ErrNotFound)
	_, err = s.TransitionTask("mem", "WEB-1", 0, "start")
	wantErr(t, err, ErrVersionRequired)
	_, err = s.TransitionTask("mem", "WEB-1", 1, "pause")
	wantErr(t, err, ErrWrongState)

	// A transition is an edit like any other, with the same version check.
	f.tick()
	task, err := s.TransitionTask("mem2", "WEB-1", 1, "start")
	f.ok(err)
	if task.State != model.TaskInProgress || task.Version != 2 || !task.Modified.Equal(f.clock) {
		t.Errorf("started: %+v", task)
	}
	cur := wantConflict(t, func() error {
		_, err := s.TransitionTask("mem", "WEB-1", 1, "start")
		return err
	}())
	if cur.(*model.Task).Version != 2 {
		t.Errorf("conflict carries %+v", cur)
	}

	for i, step := range []struct{ id, state, substate string }{
		{"pause", model.TaskPaused, ""},
		{"restart", model.TaskInProgress, ""},
		{"complete", model.TaskComplete, model.SubstateDone},
	} {
		task, err = s.TransitionTask("mem", "WEB-1", 2+i, step.id)
		f.ok(err)
		if task.State != step.state || task.Substate != step.substate {
			t.Errorf("%s: %+v", step.id, task)
		}
	}
	// Nothing is offered on a complete task.
	_, err = s.TransitionTask("mem", "WEB-1", 5, "restart")
	wantErr(t, err, ErrWrongState)

	task, err = s.TransitionTask("admin", "WEB-2", 1, "defer")
	f.ok(err)
	if task.State != model.TaskDeferred {
		t.Errorf("deferred: %+v", task)
	}

	x := f.reload()
	if t1, t2 := x.Task("WEB-1"), x.Task("WEB-2"); t1.State != model.TaskComplete || t1.Substate != model.SubstateDone ||
		t1.Version != 5 || t2.State != model.TaskDeferred {
		t.Errorf("on disk: %+v %+v", t1, t2)
	}
}

func TestTaskVersionFields(t *testing.T) {
	f := newFixture(t)
	s := f.s
	_, err := s.CreateTask("mem", "WEB", NewTask{Title: "T"})
	f.ok(err)

	// Versions can be set and cleared, independently.
	task, err := s.UpdateTask("mem", "WEB-1", 1, TaskPatch{FoundIn: ptr("1.0"), ResolvedIn: ptr("1.1")})
	f.ok(err)
	if task.FoundIn != "1.0" || task.ResolvedIn != "1.1" {
		t.Errorf("task = %+v", task)
	}
	task, err = s.UpdateTask("mem", "WEB-1", 2, TaskPatch{FoundIn: ptr("")})
	f.ok(err)
	if task.FoundIn != "" || task.ResolvedIn != "1.1" {
		t.Errorf("task = %+v", task)
	}
	_, err = s.UpdateTask("mem", "WEB-1", 3, TaskPatch{ResolvedIn: ptr(strings.Repeat("1", model.MaxVersionLen+1))})
	wantInvalid(t, err, "resolved_in")
	if x := f.reload(); x.Task("WEB-1").ResolvedIn != "1.1" {
		t.Error("versions on disk wrong")
	}
}

func TestTaskURLs(t *testing.T) {
	f := newFixture(t)
	s := f.s
	pr1, pr2 := "https://github.com/example/site/pull/1", "https://github.com/example/site/pull/2"

	// Empty lists are dropped.
	task, err := s.CreateTask("mem", "WEB", NewTask{Title: "T", URLs: model.URLs{model.URLPR: {}}})
	f.ok(err)
	if task.URLs != nil {
		t.Errorf("urls = %v", task.URLs)
	}
	task, err = s.CreateTask("mem", "WEB", NewTask{Title: "T", URLs: model.URLs{model.URLPR: {pr1}}})
	f.ok(err)
	if got := task.URLs[model.URLPR]; len(got) != 1 || got[0] != pr1 {
		t.Errorf("urls = %v", task.URLs)
	}

	// Project URL types are not task URL types, and URLs must be absolute.
	_, err = s.CreateTask("mem", "WEB", NewTask{Title: "T", URLs: model.URLs{model.URLCode: {pr1}}})
	wantInvalid(t, err, "urls.code")
	_, err = s.UpdateTask("mem", "WEB-2", 1, TaskPatch{URLs: &model.URLs{model.URLPR: {"pull/2"}}})
	wantInvalid(t, err, "urls.pr[0]")

	// The returned task is a copy.
	task.URLs[model.URLPR][0] = "https://changed.example"
	if got, _ := s.Task("mem", "WEB-2"); got.URLs[model.URLPR][0] != pr1 {
		t.Error("changing a returned task changed the stored one")
	}

	// An update replaces the list; the same list is no change.
	task, err = s.UpdateTask("mem", "WEB-2", 1, TaskPatch{URLs: &model.URLs{model.URLPR: {pr1, pr2}}})
	f.ok(err)
	if task.Version != 2 || len(task.URLs[model.URLPR]) != 2 {
		t.Errorf("task = %+v", task)
	}
	task, err = s.UpdateTask("mem", "WEB-2", 2, TaskPatch{URLs: &model.URLs{model.URLPR: {pr1, pr2}}})
	f.ok(err)
	if task.Version != 2 {
		t.Errorf("unchanged URLs wrote a new version: %+v", task)
	}
	_, err = s.UpdateTask("mem", "WEB-2", 2, TaskPatch{URLs: &model.URLs{}})
	f.ok(err)
	if x := f.reload(); x.Task("WEB-2").URLs != nil {
		t.Errorf("urls on disk = %v", x.Task("WEB-2").URLs)
	}
}

// setFormat rewrites the dataset's format, as an older or newer build would
// have left it.
func (f *fixture) setFormat(v model.Format) {
	f.t.Helper()
	st, err := store.OpenAnyFormat(f.root)
	f.ok(err)
	defer st.Close()
	meta := st.Meta()
	meta.SetVersion(v)
	f.ok(st.WriteMeta(meta))
}

func (f *fixture) format() model.Format {
	f.t.Helper()
	meta, err := store.ReadMeta(f.root)
	f.ok(err)
	return meta.Version()
}

// formatChanges records the calls Open makes to its formatChanged callback.
type formatChanges []string

func (c *formatChanges) add(from, to model.Format) { *c = append(*c, from.String()+"->"+to.String()) }

func olderMinor(t *testing.T) model.Format {
	t.Helper()
	if model.FormatCurrent.Minor == 0 {
		t.Skip("the current format has no older minor")
	}
	return model.Format{Major: model.FormatCurrent.Major, Minor: model.FormatCurrent.Minor - 1}
}

func TestOpenUpdatesOlderMinor(t *testing.T) {
	f := newFixture(t)
	f.s.Close()
	older := olderMinor(t)
	f.setFormat(older)

	var changes formatChanges
	s, _, err := Open(f.root, changes.add)
	f.ok(err)
	defer s.Close()
	if want := older.String() + "->" + model.FormatCurrent.String(); len(changes) != 1 || changes[0] != want {
		t.Errorf("changes = %v, want [%s]", changes, want)
	}
	if got := f.format(); got != model.FormatCurrent {
		t.Errorf("format on disk = %s", got)
	}
	if got := s.x.Meta().Version(); got != model.FormatCurrent {
		t.Errorf("format in the index = %s", got)
	}

	// Already current: nothing to report.
	s.Close()
	changes = nil
	s, _, err = Open(f.root, changes.add)
	f.ok(err)
	s.Close()
	if len(changes) != 0 {
		t.Errorf("changes = %v on a current dataset", changes)
	}
}

func TestOpenKeepsOlderMinorOfBrokenDataset(t *testing.T) {
	f := newFixture(t)
	f.s.Close()
	st, err := store.Open(f.root)
	f.ok(err)
	f.ok(st.WriteTask(&model.Task{ID: "WEB-1", Version: 1, Title: "x", Type: model.TypeTask, Creator: "ghost", Owner: "ghost",
		State: model.TaskNew, Priority: 3, Created: t0, Modified: t0}))
	st.Close()
	older := olderMinor(t)
	f.setFormat(older)

	var changes formatChanges
	var oe *OpenError
	if _, _, err := Open(f.root, changes.add); !errors.As(err, &oe) {
		t.Fatalf("Open = %v", err)
	}
	if got := f.format(); got != older || len(changes) != 0 {
		t.Errorf("format %s, changes %v; a refused dataset must be left as it was", got, changes)
	}
}

func TestOpenUpgradesOlderMajor(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.CreateTask("mem", "WEB", NewTask{Title: "Old"})
	f.ok(err)
	f.s.Close()

	// Make it a format 1 dataset: tasks had no type.
	path := store.TaskPath(f.root, "WEB-1")
	b, err := os.ReadFile(path)
	f.ok(err)
	old := strings.Replace(string(b), "\ntype: task\n", "\n", 1)
	if old == string(b) {
		t.Fatalf("no type line to remove:\n%s", b)
	}
	f.ok(os.WriteFile(path, []byte(old), 0o644))
	f.setFormat(model.Format{Major: 1})

	var changes formatChanges
	s, _, err := Open(f.root, changes.add)
	f.ok(err)
	defer s.Close()
	if want := "1.0->" + model.FormatCurrent.String(); len(changes) != 1 || changes[0] != want {
		t.Errorf("changes = %v, want [%s]", changes, want)
	}
	if got := f.format(); got != model.FormatCurrent {
		t.Errorf("format on disk = %s", got)
	}
	if task, err := s.Task("mem", "WEB-1"); err != nil || task.Type != model.TypeTask {
		t.Errorf("task = %+v, %v", task, err)
	}
}

func TestOpenRefusesFormat(t *testing.T) {
	cur := model.FormatCurrent
	for _, v := range []model.Format{
		{Major: cur.Major, Minor: cur.Minor + 1},
		{Major: cur.Major + 1},
		{Major: model.FormatMin - 1},
	} {
		f := newFixture(t)
		f.s.Close()
		f.setFormat(v)
		var changes formatChanges
		var fe *store.FormatError
		if _, _, err := Open(f.root, changes.add); !errors.As(err, &fe) {
			t.Errorf("format %s: Open = %v", v, err)
		}
		if got := f.format(); got != v || len(changes) != 0 {
			t.Errorf("format %s: now %s, changes %v", v, got, changes)
		}
		if _, _, err := store.ReadLock(f.root); err == nil {
			t.Errorf("format %s: lock left behind", v)
		}
	}
}

func TestUnknownStatePreserved(t *testing.T) {
	f := newFixture(t)
	f.s.Close()
	st, err := store.Open(f.root)
	f.ok(err)
	f.ok(st.WriteTask(&model.Task{ID: "WEB-1", Version: 1, Title: "Old", Type: model.TypeTask, Creator: "mem", Owner: "mem",
		State: "someday", Priority: 3, Created: t0, Modified: t0}))
	st.Close()

	s, report, err := Open(f.root, nil)
	f.ok(err)
	defer s.Close()
	if report.Warnings() != 1 {
		t.Errorf("warnings = %v", report.Problems)
	}

	// Editing other fields keeps the unknown state...
	task, err := s.UpdateTask("mem", "WEB-1", 1, TaskPatch{Title: ptr("New")})
	f.ok(err)
	if task.State != "someday" {
		t.Errorf("state = %q", task.State)
	}
	// ...but a client can never set one.
	_, err = s.UpdateTask("mem", "WEB-1", 2, TaskPatch{State: ptr("whenever")})
	wantInvalid(t, err, "state")
}

func TestTaskListing(t *testing.T) {
	f := newFixture(t)
	s := f.s
	for _, in := range []NewTask{
		{Title: "a", Priority: 3},
		{Title: "b", Priority: 1, Owner: "mem2"},
		{Title: "c", Priority: 3, State: model.TaskPaused},
		{Title: "d", Priority: 2},
	} {
		f.tick()
		_, err := s.CreateTask("mem", "WEB", in)
		f.ok(err)
	}
	f.tick()
	_, err := s.UpdateTask("mem", "WEB-1", 1, TaskPatch{Title: ptr("a2")})
	f.ok(err)

	ids := func(tasks []*model.Task, err error) string {
		f.ok(err)
		var out []string
		for _, t := range tasks {
			out = append(out, t.ID)
		}
		return strings.Join(out, " ")
	}
	for _, tc := range []struct {
		f     TaskFilter
		order TaskSort
		want  string
	}{
		{TaskFilter{}, SortByID, "WEB-1 WEB-2 WEB-3 WEB-4"},
		{TaskFilter{}, SortByPriority, "WEB-2 WEB-4 WEB-1 WEB-3"},
		{TaskFilter{}, SortByModified, "WEB-1 WEB-4 WEB-3 WEB-2"},
		{TaskFilter{Owner: "mem"}, SortByID, "WEB-1 WEB-3 WEB-4"},
		{TaskFilter{States: []string{model.TaskPaused}}, SortByID, "WEB-3"},
		{TaskFilter{States: []string{model.TaskPaused, model.TaskNew}, Owner: "mem"}, SortByID, "WEB-1 WEB-3 WEB-4"},
		{TaskFilter{Priority: 3, Owner: "mem"}, SortByID, "WEB-1 WEB-3"},
		{TaskFilter{Priority: 2, PriorityOrHigher: true}, SortByID, "WEB-2 WEB-4"},
		{TaskFilter{Priority: 1, PriorityOrHigher: true}, SortByID, "WEB-2"},
	} {
		if got := ids(s.Tasks("view", "WEB", tc.f, tc.order)); got != tc.want {
			t.Errorf("%+v %v: got %s, want %s", tc.f, tc.order, got, tc.want)
		}
	}
	_, err = s.Tasks("view", "NOPE", TaskFilter{}, SortByID)
	wantErr(t, err, ErrNotFound)
}

func TestComments(t *testing.T) {
	f := newFixture(t)
	s := f.s
	_, err := s.CreateTask("mem", "WEB", NewTask{Title: "T"})
	f.ok(err)

	f.tick()
	c1, err := s.AddComment("mem", "WEB-1", "one")
	f.ok(err)
	c2, err := s.AddComment("mem2", "WEB-1", "two")
	f.ok(err)
	if c1.ID != 1 || c2.ID != 2 || c1.Commenter != "mem" || c1.Version != 1 {
		t.Errorf("comments = %+v %+v", c1, c2)
	}
	task, _ := s.Task("view", "WEB-1")
	if task.Version != 1 || !task.Modified.Equal(f.clock) || len(task.Comments) != 2 {
		t.Errorf("comments changed the task version or left modified alone: %+v", task)
	}
	// Anyone may read comments.
	cs, err := s.Comments("view", "WEB-1")
	f.ok(err)
	if len(cs) != 2 || cs[0].Text != "one" || cs[1].Text != "two" {
		t.Errorf("Comments = %+v", cs)
	}
	c, err := s.Comment("view", "WEB-1", 2)
	f.ok(err)
	if c.Text != "two" || c.Commenter != "mem2" {
		t.Errorf("Comment = %+v", c)
	}
	_, err = s.Comment("view", "WEB-1", 3)
	wantErr(t, err, ErrNotFound)
	_, err = s.Comments("view", "WEB-9")
	wantErr(t, err, ErrNotFound)

	// So a task edit based on version 1 still succeeds.
	_, err = s.UpdateTask("mem2", "WEB-1", 1, TaskPatch{Title: ptr("T2")})
	f.ok(err)

	_, err = s.AddComment("mem", "WEB-1", "")
	wantInvalid(t, err, "text")
	_, err = s.AddComment("mem", "WEB-9", "x")
	wantErr(t, err, ErrNotFound)

	// Only the commenter may edit or delete, admins included.
	_, err = s.EditComment("mem2", "WEB-1", 1, 1, "hijack")
	wantErr(t, err, ErrForbidden)
	_, err = s.EditComment("admin", "WEB-1", 1, 1, "hijack")
	wantErr(t, err, ErrForbidden)
	wantErr(t, s.DeleteComment("admin", "WEB-1", 2, 1), ErrForbidden)

	c1, err = s.EditComment("mem", "WEB-1", 1, 1, "one, edited")
	f.ok(err)
	if c1.Version != 2 || c1.Text != "one, edited" {
		t.Errorf("edited = %+v", c1)
	}
	_, err = s.EditComment("mem", "WEB-1", 1, 1, "stale")
	wantConflict(t, err)
	_, err = s.EditComment("mem", "WEB-1", 1, 0, "x")
	wantErr(t, err, ErrVersionRequired)
	_, err = s.EditComment("mem", "WEB-1", 7, 1, "x")
	wantErr(t, err, ErrNotFound)

	// Deleting the newest comment must not let its ID be reused.
	f.ok(s.DeleteComment("mem2", "WEB-1", 2, 1))
	c3, err := s.AddComment("mem", "WEB-1", "three")
	f.ok(err)
	if c3.ID != 3 {
		t.Errorf("new comment after delete got ID %d, want 3", c3.ID)
	}

	x := f.reload()
	task = x.Task("WEB-1")
	if len(task.Comments) != 2 || task.Comments[0].Text != "one, edited" || task.LastCommentID != 3 {
		t.Errorf("task on disk = %+v", task)
	}
}

func TestUsers(t *testing.T) {
	f := newFixture(t)
	s := f.s

	_, _, err := s.CreateUser("admin", NewUser{ID: "mem", Name: "x", Email: "x@example.com", Role: model.RoleViewer})
	wantErr(t, err, ErrExists)
	_, _, err = s.CreateUser("admin", NewUser{ID: "Bad", Name: "x", Email: "x@example.com", Role: model.RoleViewer})
	wantInvalid(t, err, "id")
	_, _, err = s.CreateUser("admin", NewUser{ID: "newbie", Name: "x", Email: "nope", Role: model.RoleViewer})
	wantInvalid(t, err, "email")
	_, _, err = s.CreateUser("admin", NewUser{ID: "newbie", Name: "x", Email: "x@example.com", Role: "owner"})
	wantInvalid(t, err, "role")

	u, err := s.UpdateUser("admin", "view", 1, UserPatch{Role: ptr(model.RoleMember), Name: ptr("Viewer No More")})
	f.ok(err)
	if u.Role != model.RoleMember || u.Version != 2 {
		t.Errorf("user = %+v", u)
	}

	// The last active admin can be neither demoted nor deactivated...
	_, err = s.UpdateUser("admin", "admin", 1, UserPatch{Role: ptr(model.RoleMember)})
	wantInvalid(t, err, "role")
	_, err = s.UpdateUser("admin", "admin", 1, UserPatch{Active: ptr(false)})
	wantInvalid(t, err, "active")
	// ...but once there is another, they can.
	_, err = s.UpdateUser("admin", "mem", 1, UserPatch{Role: ptr(model.RoleAdmin)})
	f.ok(err)
	_, err = s.UpdateUser("admin", "admin", 1, UserPatch{Role: ptr(model.RoleMember)})
	f.ok(err)
	_, err = s.CreateProject("admin", NewProject{ID: "API", Name: "x"})
	wantErr(t, err, ErrForbidden)

	f.reload()
}

func TestPasswords(t *testing.T) {
	f := newFixture(t)
	s := f.s

	u, mustChange, err := s.Login("mem", f.pw["mem"])
	f.ok(err)
	if u.ID != "mem" || !mustChange {
		t.Errorf("login = %+v, mustChange %v", u, mustChange)
	}
	for _, bad := range [][2]string{{"mem", "wrong"}, {"ghost", "x"}, {"MEM", f.pw["mem"]}} {
		_, _, err := s.Login(bad[0], bad[1])
		wantErr(t, err, ErrBadCredentials)
	}

	wantErr(t, s.ChangePassword("mem", "wrong", "new-password"), ErrBadCredentials)
	wantInvalid(t, s.ChangePassword("mem", f.pw["mem"], "short"), "password")
	f.ok(s.ChangePassword("mem", f.pw["mem"], "new-password"))
	_, mustChange, err = s.Login("mem", "new-password")
	f.ok(err)
	if mustChange {
		t.Error("must-change flag not cleared")
	}
	_, _, err = s.Login("mem", f.pw["mem"])
	wantErr(t, err, ErrBadCredentials)

	reset, err := s.ResetPassword("admin", "mem")
	f.ok(err)
	_, mustChange, err = s.Login("mem", reset)
	f.ok(err)
	if !mustChange {
		t.Error("reset password not marked for change")
	}

	// Deactivated users cannot log in.
	_, err = s.UpdateUser("admin", "mem", 1, UserPatch{Active: ptr(false)})
	f.ok(err)
	_, _, err = s.Login("mem", reset)
	wantErr(t, err, ErrBadCredentials)
}

func TestTokens(t *testing.T) {
	f := newFixture(t)
	s := f.s

	info, secret, err := s.CreateToken("view", "laptop script")
	f.ok(err)
	if !strings.HasPrefix(secret, auth.TokenPrefix) || !strings.HasPrefix(info.ID, "tok_") || info.Hash != "" {
		t.Errorf("token = %+v, %q", info, secret)
	}
	u, err := s.TokenLogin(secret)
	if err != nil || u.ID != "view" {
		t.Errorf("TokenLogin = %+v, %v", u, err)
	}
	_, err = s.TokenLogin(secret + "x")
	wantErr(t, err, ErrBadCredentials)

	list, err := s.Tokens("view")
	f.ok(err)
	if len(list) != 1 || list[0].ID != info.ID || list[0].Hash != "" || list[0].Name != "laptop script" {
		t.Errorf("tokens = %+v", list)
	}
	if other, _ := s.Tokens("mem"); len(other) != 0 {
		t.Errorf("mem sees tokens %+v", other)
	}

	// The secret is not on disk; only its hash is.
	a := f.reload().Auth("view")
	if len(a.Tokens) != 1 || a.Tokens[0].Hash != auth.HashToken(secret) {
		t.Errorf("stored token = %+v", a.Tokens)
	}

	// Revoked and deactivated users' tokens stop working at once.
	wantErr(t, s.RevokeToken("mem", info.ID), ErrNotFound)
	f.ok(s.RevokeToken("view", info.ID))
	_, err = s.TokenLogin(secret)
	wantErr(t, err, ErrBadCredentials)

	_, secret, err = s.CreateToken("view", "second")
	f.ok(err)
	_, err = s.UpdateUser("admin", "view", 1, UserPatch{Active: ptr(false)})
	f.ok(err)
	_, err = s.TokenLogin(secret)
	wantErr(t, err, ErrBadCredentials)

	_, _, err = s.CreateToken("mem", "")
	wantInvalid(t, err, "name")
}

// Tokens survive a restart: the lookup is rebuilt from disk.
func TestTokensAfterReopen(t *testing.T) {
	f := newFixture(t)
	_, secret, err := f.s.CreateToken("mem", "x")
	f.ok(err)
	f.ok(f.s.Close())
	s, _, err := Open(f.root, nil)
	f.ok(err)
	defer s.Close()
	if u, err := s.TokenLogin(secret); err != nil || u.ID != "mem" {
		t.Errorf("TokenLogin after reopen = %+v, %v", u, err)
	}
}

// Of many concurrent edits based on the same version, exactly one wins.
func TestConcurrentEdits(t *testing.T) {
	f := newFixture(t)
	_, err := f.s.CreateTask("mem", "WEB", NewTask{Title: "T"})
	f.ok(err)

	const n = 20
	errs := make(chan error, n)
	for i := range n {
		go func() {
			_, err := f.s.UpdateTask("mem", "WEB-1", 1, TaskPatch{Priority: ptr(i%5 + 1), Title: ptr(strings.Repeat("x", i+1))})
			errs <- err
		}()
	}
	wins := 0
	for range n {
		err := <-errs
		var ce *ConflictError
		switch {
		case err == nil:
			wins++
		case !errors.As(err, &ce):
			t.Errorf("unexpected error %v", err)
		}
	}
	if wins != 1 {
		t.Errorf("%d edits won, want 1", wins)
	}
	if v := f.reload().Task("WEB-1").Version; v != 2 {
		t.Errorf("version on disk = %d, want 2", v)
	}
}
