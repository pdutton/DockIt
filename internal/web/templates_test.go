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
