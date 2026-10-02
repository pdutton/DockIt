package web

import (
	"strings"
	"testing"

	"github.com/pdutton/DockIt/internal/model"
)

// Every task type needs a short name for task lists.
func TestTaskTypeAbbrevs(t *testing.T) {
	seen := map[string]string{}
	for _, v := range model.TaskTypes.Values() {
		short, ok := taskTypeAbbrevs[v.ID]
		if !ok {
			t.Errorf("task type %q has no short name", v.ID)
			continue
		}
		if other, dup := seen[short]; dup {
			t.Errorf("task types %q and %q share the short name %q", other, v.ID, short)
		}
		seen[short] = v.ID
	}
	if got := string(taskTypeShort("nonesuch")); !strings.Contains(got, `class="unknown"`) {
		t.Errorf("unknown type = %s, want the unknown marker", got)
	}
}

// Every priority needs a name, and the names must differ.
func TestPriorityNames(t *testing.T) {
	seen := map[string]int{}
	for _, p := range priorities() {
		name, ok := priorityNames[p]
		if !ok {
			t.Errorf("priority %d has no name", p)
			continue
		}
		if other, dup := seen[name]; dup {
			t.Errorf("priorities %d and %d share the name %q", other, p, name)
		}
		seen[name] = p
	}
	if got := string(priorityName(0)); !strings.Contains(got, `class="unknown"`) || !strings.Contains(got, "0") {
		t.Errorf("unknown priority = %s, want 0 with the unknown marker", got)
	}
}
