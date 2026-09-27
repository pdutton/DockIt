package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

var ts = time.Date(2026, 9, 26, 21, 54, 43, 0, time.UTC)

func user(id, role string) (*model.User, *model.Auth) {
	return &model.User{ID: id, Version: 1, Name: strings.ToUpper(id), Email: id + "@example.com",
			Role: role, Active: true, Created: ts, Modified: ts},
		&model.Auth{User: id, Password: "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$a2V5"}
}

func task(id string) *model.Task {
	return &model.Task{ID: id, Version: 1, Title: "Task " + id, Creator: "admin", Owner: "admin",
		State: model.TaskNew, Priority: 3, Created: ts, Modified: ts}
}

// newDataset writes a small, valid dataset and returns its root:
// users admin and bob; projects WEB (tasks 1, 2, 10) and API (no tasks).
func newDataset(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	u, a := user("admin", model.RoleAdmin)
	if err := store.Init(root, u, a); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	bob, bobAuth := user("bob", model.RoleMember)
	must(t, s.WriteUser(bob))
	must(t, s.WriteAuth(bobAuth))
	for _, pid := range []string{"WEB", "API"} {
		must(t, s.WriteProject(&model.Project{ID: pid, Version: 1, Name: pid + " project",
			State: model.ProjectActive, Created: ts, Modified: ts,
			URLs: model.URLs{model.URLCode: {"https://example.com/" + pid}}}))
	}
	for _, tid := range []string{"WEB-1", "WEB-2", "WEB-10"} {
		must(t, s.WriteTask(task(tid)))
	}
	done := task("WEB-2")
	done.State, done.Substate, done.Owner = model.TaskComplete, model.SubstateDone, "bob"
	done.Comments = []model.Comment{{ID: 1, Version: 1, Commenter: "bob", Created: ts, Modified: ts, Text: "Done."}}
	must(t, s.WriteTask(done))
	return root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	root := newDataset(t)
	x, r := Load(root)
	if len(r.Problems) != 0 {
		t.Fatalf("problems in a clean dataset: %v", r.Problems)
	}

	var pids []string
	for _, p := range x.Projects() {
		pids = append(pids, p.ID)
	}
	if strings.Join(pids, ",") != "API,WEB" {
		t.Errorf("projects = %v", pids)
	}

	var tids []string
	for _, tk := range x.Tasks("WEB") {
		tids = append(tids, tk.ID)
	}
	if strings.Join(tids, ",") != "WEB-1,WEB-2,WEB-10" {
		t.Errorf("tasks = %v, want numeric order", tids)
	}
	if n := x.NextTaskNumber("WEB"); n != 11 {
		t.Errorf("NextTaskNumber(WEB) = %d, want 11", n)
	}
	if n := x.NextTaskNumber("API"); n != 1 {
		t.Errorf("NextTaskNumber(API) = %d, want 1", n)
	}
	if got := x.Task("WEB-2"); got == nil || got.Owner != "bob" || len(got.Comments) != 1 {
		t.Errorf("Task(WEB-2) = %+v", got)
	}
	if x.User("bob") == nil || x.Auth("bob") == nil || x.User("nobody") != nil {
		t.Error("user lookups wrong")
	}
	if x.TaskCount() != 3 || len(x.Users()) != 2 {
		t.Errorf("TaskCount = %d, users = %d", x.TaskCount(), len(x.Users()))
	}
}

func TestIndexReturnsCopies(t *testing.T) {
	x, _ := Load(newDataset(t))

	tk := x.Task("WEB-2")
	tk.Title = "changed"
	tk.Comments[0].Text = "changed"
	if got := x.Task("WEB-2"); got.Title == "changed" || got.Comments[0].Text == "changed" {
		t.Error("modifying a returned task changed the index")
	}

	p := x.Project("WEB")
	p.URLs[model.URLCode][0] = "changed"
	if x.Project("WEB").URLs[model.URLCode][0] == "changed" {
		t.Error("modifying a returned project changed the index")
	}

	in := task("API-5")
	x.PutTask(in)
	in.Title = "changed"
	if x.Task("API-5").Title == "changed" {
		t.Error("modifying a stored task changed the index")
	}
	if x.NextTaskNumber("API") != 6 {
		t.Error("PutTask did not advance the task number")
	}
}

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(content), 0o644))
}

// edit rewrites a file, replacing old with new, which must be present.
func edit(t *testing.T, root, rel, old, new string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	b, err := os.ReadFile(path)
	must(t, err)
	if !strings.Contains(string(b), old) {
		t.Fatalf("%s does not contain %q:\n%s", rel, old, b)
	}
	must(t, os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644))
}

func TestLoadProblems(t *testing.T) {
	for _, tc := range []struct {
		name    string
		damage  func(t *testing.T, root string)
		path    string // expected problem path
		message string // substring of the expected problem
		warning bool
	}{
		{"unparseable", func(t *testing.T, r string) { write(t, r, "projects/WEB/tasks/WEB-1.yaml", "id: [oops\n") },
			"projects/WEB/tasks/WEB-1.yaml", "cannot parse", false},
		{"empty file", func(t *testing.T, r string) { write(t, r, "users/bob.yaml", "") },
			"users/bob.yaml", "empty file", false},
		{"unknown field", func(t *testing.T, r string) {
			edit(t, r, "users/bob.yaml", "active: true", "active: true\nshoe_size: 9")
		},
			"users/bob.yaml", "shoe_size", false},
		{"task ID mismatch", func(t *testing.T, r string) { edit(t, r, "projects/WEB/tasks/WEB-1.yaml", "id: WEB-1", "id: WEB-7") },
			"projects/WEB/tasks/WEB-1.yaml", "does not match the file name", false},
		{"task in wrong project", func(t *testing.T, r string) {
			must(t, os.MkdirAll(filepath.Join(r, "projects/API/tasks"), 0o755))
			must(t, os.Rename(filepath.Join(r, "projects/WEB/tasks/WEB-1.yaml"), filepath.Join(r, "projects/API/tasks/WEB-1.yaml")))
		}, "projects/API/tasks/WEB-1.yaml", "in the directory of project API", false},
		{"user ID mismatch", func(t *testing.T, r string) { edit(t, r, "users/bob.yaml", "id: bob", "id: rob") },
			"users/bob.yaml", "does not match the file name", false},
		{"project ID mismatch", func(t *testing.T, r string) { edit(t, r, "projects/API/API.yaml", "id: API", "id: APX") },
			"projects/API/API.yaml", "does not match the directory name", false},
		{"missing project file", func(t *testing.T, r string) { must(t, os.Remove(filepath.Join(r, "projects/API/API.yaml"))) },
			"projects/API", "project file API.yaml missing", false},
		{"lowercase project dir", func(t *testing.T, r string) { must(t, os.Mkdir(filepath.Join(r, "projects/misc"), 0o755)) },
			"projects/misc", "not a valid project ID", false},
		{"missing owner", func(t *testing.T, r string) {
			edit(t, r, "projects/WEB/tasks/WEB-1.yaml", "owner: admin", "owner: carol")
		},
			"projects/WEB/tasks/WEB-1.yaml", `owner: user "carol" does not exist`, false},
		{"missing commenter", func(t *testing.T, r string) {
			edit(t, r, "projects/WEB/tasks/WEB-2.yaml", "commenter: bob", "commenter: carol")
		},
			"projects/WEB/tasks/WEB-2.yaml", "comments[0].commenter", false},
		{"unknown state", func(t *testing.T, r string) {
			edit(t, r, "projects/WEB/tasks/WEB-1.yaml", "state: new", "state: someday")
		},
			"projects/WEB/tasks/WEB-1.yaml", `unknown task state "someday"`, true},
		{"missing substate", func(t *testing.T, r string) { edit(t, r, "projects/WEB/tasks/WEB-2.yaml", "substate: done\n", "") },
			"projects/WEB/tasks/WEB-2.yaml", "substate: required", false},
		{"unexpected substate", func(t *testing.T, r string) {
			edit(t, r, "projects/WEB/tasks/WEB-1.yaml", "state: new", "state: new\nsubstate: done")
		}, "projects/WEB/tasks/WEB-1.yaml", "has no substates", false},
		{"bad priority", func(t *testing.T, r string) {
			edit(t, r, "projects/WEB/tasks/WEB-1.yaml", "priority: 3", "priority: 9")
		},
			"projects/WEB/tasks/WEB-1.yaml", "priority: must be 1 to 5", false},
		{"javascript URL", func(t *testing.T, r string) {
			edit(t, r, "projects/WEB/WEB.yaml", "https://example.com/WEB", "javascript:alert(1)")
		}, "projects/WEB/WEB.yaml", "not an absolute http or https URL", false},
		{"unknown URL type", func(t *testing.T, r string) { edit(t, r, "projects/WEB/WEB.yaml", "code:", "wiki:") },
			"projects/WEB/WEB.yaml", `unknown URL type "wiki"`, true},
		{"duplicate comment", func(t *testing.T, r string) {
			edit(t, r, "projects/WEB/tasks/WEB-2.yaml", "comments:\n",
				"comments:\n  - id: 1\n    version: 1\n    commenter: bob\n    created: 2026-09-26T21:54:43Z\n    modified: 2026-09-26T21:54:43Z\n    text: Again.\n")
		}, "projects/WEB/tasks/WEB-2.yaml", "duplicate comment ID 1", false},
		{"non-UTC time", func(t *testing.T, r string) {
			edit(t, r, "users/bob.yaml", "created: 2026-09-26T21:54:43Z", "created: 2026-09-26T23:54:43+02:00")
		}, "users/bob.yaml", "not in UTC", true},
		{"no secrets", func(t *testing.T, r string) { must(t, os.Remove(filepath.Join(r, "auth/bob.yaml"))) },
			"users/bob.yaml", "cannot log in", true},
		{"orphan secrets", func(t *testing.T, r string) {
			b, _ := os.ReadFile(filepath.Join(r, "auth/bob.yaml"))
			write(t, r, "auth/carol.yaml", strings.Replace(string(b), "user: bob", "user: carol", 1))
		}, "auth/carol.yaml", "no profile", true},
		{"no active admin", func(t *testing.T, r string) { edit(t, r, "users/admin.yaml", "active: true", "active: false") },
			"users", "no active admin", true},
		{"stray file", func(t *testing.T, r string) { write(t, r, "notes.txt", "hi") },
			"notes.txt", "unexpected file", true},
		{"stray file in tasks", func(t *testing.T, r string) { write(t, r, "projects/WEB/tasks/README", "hi") },
			"projects/WEB/tasks/README", "unexpected file", true},
		{"leftover temp file", func(t *testing.T, r string) { write(t, r, "users/.bob.yaml.tmp", "partial") },
			"users/.bob.yaml.tmp", "leftover temporary file", true},
		{"missing users dir", func(t *testing.T, r string) {
			must(t, os.RemoveAll(filepath.Join(r, "users")))
			must(t, os.RemoveAll(filepath.Join(r, "auth")))
			must(t, os.Mkdir(filepath.Join(r, "auth"), 0o755))
		}, "users", "directory missing", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := newDataset(t)
			tc.damage(t, root)
			_, r := Load(root)
			found := false
			for _, p := range r.Problems {
				if p.Path == tc.path && strings.Contains(p.String(), tc.message) && p.Warning == tc.warning {
					found = true
				}
			}
			if !found {
				t.Errorf("no problem at %s matching %q (warning=%v); got:\n%s", tc.path, tc.message, tc.warning, dump(r))
			}
			if tc.warning && !r.OK() {
				t.Errorf("a warning-only damage produced errors:\n%s", dump(r))
			}
		})
	}
}

func dump(r *Report) string {
	var b strings.Builder
	for _, p := range r.Problems {
		b.WriteString("  " + p.String() + "\n")
	}
	return b.String()
}

func TestLoadFormat(t *testing.T) {
	root := newDataset(t)
	edit(t, root, "dockit.yaml", "format: 1", "format: 99")
	x, r := Load(root)
	if x != nil || r.OK() || !strings.Contains(dump(r), "newer than this build supports") {
		t.Errorf("x = %v, report:\n%s", x, dump(r))
	}
}

func TestLoadNoDataset(t *testing.T) {
	x, r := Load(t.TempDir())
	if x != nil || r.OK() || !strings.Contains(dump(r), "not a DockIt dataset") {
		t.Errorf("x = %v, report:\n%s", x, dump(r))
	}
}

func TestLoadIgnoresLockFile(t *testing.T) {
	root := newDataset(t)
	s, err := store.Open(root)
	must(t, err)
	defer s.Close()
	if _, r := Load(root); len(r.Problems) != 0 {
		t.Errorf("problems with a lock held:\n%s", dump(r))
	}
}
