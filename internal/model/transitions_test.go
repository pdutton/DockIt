package model

import "testing"

func TestTransitions(t *testing.T) {
	seen := map[string]bool{}
	for _, tr := range Transitions {
		if seen[tr.ID] || tr.Display == "" {
			t.Errorf("transition %+v: duplicate id or no label", tr)
		}
		seen[tr.ID] = true
		if !TaskStates.Valid(tr.From) || !TaskStates.Valid(tr.To) || tr.From == tr.To {
			t.Errorf("transition %+v: bad states", tr)
		}
		// A task in a state with substates must have one, so a transition
		// into such a state names it.
		if subs, ok := Substates[tr.To]; ok != (tr.Substate != "") || ok && !subs.Valid(tr.Substate) {
			t.Errorf("transition %+v: bad substate", tr)
		}
	}

	var ids []string
	for _, tr := range TransitionsFrom(TaskInProgress) {
		ids = append(ids, tr.ID)
	}
	if len(ids) != 2 || ids[0] != "complete" || ids[1] != "pause" {
		t.Errorf("from in progress: %v", ids)
	}
	if got := TransitionsFrom(TaskComplete); len(got) != 0 {
		t.Errorf("from complete: %v", got)
	}
	if tr, ok := TransitionByID("complete"); !ok || tr.To != TaskComplete || tr.Substate != SubstateDone {
		t.Errorf("complete = %+v, %v", tr, ok)
	}
	if _, ok := TransitionByID("finish"); ok {
		t.Error("unknown transition found")
	}
}
