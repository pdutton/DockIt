package model

// A Transition is a quick state change, offered on a task in state From:
// "Start" takes a new task to in progress.  Transitions are only a shortcut.
// State changes are unrestricted, and any state can still be set directly.
type Transition struct {
	ID       string // stable id, used by the REST API
	Display  string // the button label
	From     string // the state the task must be in
	To       string
	Substate string // set with To, which has substates exactly when this is set
}

// Transitions in display order.
var Transitions = []Transition{
	{"start", "Start", TaskNew, TaskInProgress, ""},
	{"defer", "Defer", TaskNew, TaskDeferred, ""},
	{"complete", "Complete", TaskInProgress, TaskComplete, SubstateDone},
	{"pause", "Pause", TaskInProgress, TaskPaused, ""},
	{"restart", "Restart", TaskPaused, TaskInProgress, ""},
}

// TransitionByID returns the transition with id.
func TransitionByID(id string) (Transition, bool) {
	for _, t := range Transitions {
		if t.ID == id {
			return t, true
		}
	}
	return Transition{}, false
}

// TransitionsFrom returns the transitions offered on a task in state, in
// display order.
func TransitionsFrom(state string) []Transition {
	var out []Transition
	for _, t := range Transitions {
		if t.From == state {
			out = append(out, t)
		}
	}
	return out
}
