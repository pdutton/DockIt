package upgrade

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdutton/DockIt/internal/index"
	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

func TestFormat1To2(t *testing.T) {
	root := newDataset(t)
	tasks := []*model.Task{
		{ID: "WEB-1", Version: 1, Title: "Plain", Creator: "admin", Owner: "admin",
			State: model.TaskNew, Priority: 3, Created: now, Modified: now},
		// Values YAML would read as something else unless quoted, long
		// lines, blank lines, trailing spaces, and comments.
		{ID: "WEB-2", Version: 4, Title: "yes: 123 # not a comment " + strings.Repeat("long ", 30),
			Description: "Line one:  \n\n\t- tabbed\n'quoted' \"double\" ünïcode\n",
			Creator:     "admin", Owner: "admin", State: model.TaskComplete, Substate: model.SubstateDone,
			Priority: 1, Created: now, Modified: now, LastCommentID: 3,
			Comments: []model.Comment{{ID: 2, Version: 2, Commenter: "admin", Created: now, Modified: now, Text: "no"}}},
	}

	// Write each task as this build would, then take its type out to make
	// the format 1 file.
	s, err := store.OpenAnyFormat(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteProject(&model.Project{ID: "WEB", Version: 1, Name: "Web", State: model.ProjectActive,
		Created: now, Modified: now}); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{}
	for _, task := range tasks {
		task.Type = model.TypeTask
		if err := s.WriteTask(task); err != nil {
			t.Fatal(err)
		}
		path := store.TaskPath(root, task.ID)
		b, _ := os.ReadFile(path)
		want[path] = string(b)
		old := strings.Replace(string(b), "\ntype: task\n", "\n", 1)
		if old == string(b) {
			t.Fatalf("%s: no type line to remove:\n%s", task.ID, b)
		}
		if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	if _, err := Run(root, "", 2, Migrations, now); err != nil {
		t.Fatal(err)
	}
	for path, w := range want {
		b, _ := os.ReadFile(path)
		if string(b) != w {
			t.Errorf("%s:\ngot:\n%s\nwant:\n%s", filepath.Base(path), b, w)
		}
	}

	// A step that failed part way through can be run again.
	if err := addTaskType(root); err != nil {
		t.Fatal(err)
	}
	for path, w := range want {
		if b, _ := os.ReadFile(path); string(b) != w {
			t.Errorf("%s changed on a second run:\n%s", filepath.Base(path), b)
		}
	}

	if _, r := index.Load(root); len(r.Problems) != 0 {
		t.Errorf("problems after upgrade: %v", r.Problems)
	}
}
