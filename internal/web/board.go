package web

import (
	"cmp"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/service"
)

// The board is the home page: a user's current tasks at a glance, as cards
// in three lanes.

// boardDoneFor is how long a done task stays on the board after it was last
// changed.
const boardDoneFor = 36 * time.Hour

// boardLane is a column of the board.
type boardLane struct {
	Title string
	Hint  string // which tasks it holds, when the title does not say
	Tasks []*model.Task
}

type boardData struct {
	Projects []*model.Project
	Project  string // the one project shown, or "" for every project
	All      bool   // everyone's tasks, not just the user's; admins only
	Lanes    []boardLane
}

// Color returns the color of task tid's project, which tints its card.
func (d boardData) Color(tid string) string {
	pid, _, _ := model.ParseTaskID(tid)
	for _, p := range d.Projects {
		if p.ID == pid {
			return p.Color
		}
	}
	return model.DefaultProjectColor
}

func (w *Web) board(rw http.ResponseWriter, r *http.Request, c *ctx) error {
	q := r.URL.Query()
	d := boardData{Project: q.Get("project"), All: c.me.Role == model.RoleAdmin && q.Get("all") == "1"}
	var err error
	if d.Projects, err = w.svc.Projects(c.me.ID); err != nil {
		return err
	}
	f := service.TaskFilter{States: []string{model.TaskNew, model.TaskPaused, model.TaskInProgress, model.TaskComplete}}
	if !d.All {
		f.Owner = c.me.ID
	}
	var tasks []*model.Task
	if d.Project != "" {
		tasks, err = w.svc.Tasks(c.me.ID, d.Project, f, service.TaskSort{})
	} else {
		tasks, err = w.svc.AllTasks(c.me.ID, f)
	}
	if err != nil {
		return err
	}

	todo := boardLane{Title: "To do", Hint: "New or paused"}
	doing := boardLane{Title: "In progress"}
	done := boardLane{Title: "Done", Hint: fmt.Sprintf("In the last %d hours", int(boardDoneFor.Hours()))}
	since := w.now().Add(-boardDoneFor)
	for _, t := range tasks {
		switch {
		case t.State == model.TaskNew || t.State == model.TaskPaused:
			todo.Tasks = append(todo.Tasks, t)
		case t.State == model.TaskInProgress:
			doing.Tasks = append(doing.Tasks, t)
		case t.Substate == model.SubstateDone && !t.Modified.Before(since):
			done.Tasks = append(done.Tasks, t)
		}
	}
	// Tasks come in ID order, which a stable sort keeps for ties.
	byPriority := func(a, b *model.Task) int { return cmp.Compare(a.Priority, b.Priority) }
	slices.SortStableFunc(todo.Tasks, byPriority)
	slices.SortStableFunc(doing.Tasks, byPriority)
	slices.SortStableFunc(done.Tasks, func(a, b *model.Task) int { return b.Modified.Compare(a.Modified) })
	d.Lanes = []boardLane{todo, doing, done}

	w.render(rw, r, c, http.StatusOK, "board", "Board", d)
	return nil
}
