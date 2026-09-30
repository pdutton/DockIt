package model

import (
	"slices"
	"testing"
)

func TestCompareTaskIDs(t *testing.T) {
	ids := []string{"WEB-10", "bad", "API-3", "WEB-2", "WEB-1", "API-20"}
	slices.SortFunc(ids, CompareTaskIDs)
	if want := []string{"API-3", "API-20", "WEB-1", "WEB-2", "WEB-10", "bad"}; !slices.Equal(ids, want) {
		t.Errorf("sorted = %v, want %v", ids, want)
	}
}

func TestNewLink(t *testing.T) {
	for _, tc := range []struct {
		tid, rel, other string
		want            string
	}{
		{"WEB-1", "blocks", "WEB-2", "WEB-1 blocks WEB-2"},
		{"WEB-1", "blocked_by", "WEB-2", "WEB-2 blocks WEB-1"},
		{"WEB-1", "dependency_of", "API-9", "API-9 depends WEB-1"},
		{"WEB-1", "duplicated_by", "WEB-3", "WEB-3 duplicates WEB-1"},
		// Undirected links put the lower ID first, whichever end they come from.
		{"WEB-10", "related", "WEB-9", "WEB-9 related WEB-10"},
		{"WEB-9", "related", "WEB-10", "WEB-9 related WEB-10"},
		{"WEB-2", "conflicts", "API-5", "API-5 conflicts WEB-2"},
	} {
		r, ok := RelationByID(tc.rel)
		if !ok {
			t.Fatalf("no relation %q", tc.rel)
		}
		l := NewLink(tc.tid, r, tc.other)
		if got := l.A + " " + l.Type + " " + l.B; got != tc.want {
			t.Errorf("NewLink(%s, %s, %s) = %s, want %s", tc.tid, tc.rel, tc.other, got, tc.want)
		}
	}
}

func TestRelations(t *testing.T) {
	// Every link type has a relation from a, and from b if it is directed.
	for _, v := range LinkTypes.Values() {
		for _, reverse := range []bool{false, true} {
			r := RelationOf(v.ID, reverse)
			if r.Heading == "" || r.Type != v.ID || r.Reverse != (reverse && !undirected[v.ID]) {
				t.Errorf("RelationOf(%s, %v) = %+v", v.ID, reverse, r)
			}
		}
	}
	if r := RelationOf("haunts", true); r.ID != "haunts" || r.Heading != "" {
		t.Errorf("unknown type: %+v", r)
	}
	if _, ok := RelationByID("depends"); ok {
		t.Error("a link type is accepted as a relation")
	}
}

func TestLinkValidate(t *testing.T) {
	l := Link{A: "WEB-1", Type: LinkBlocks, B: "WEB-2", Creator: "bob", Created: Now()}
	if errs := l.Validate(); len(errs) != 0 {
		t.Errorf("valid link: %v", errs)
	}
	l.Type = "haunts"
	if errs := l.Validate(); len(errs) != 1 || !errs[0].Warning {
		t.Errorf("unknown type: %v", errs)
	}
	l = Link{A: "WEB-1", Type: LinkRelated, B: "WEB-1", Creator: "bob", Created: Now()}
	if errs := l.Validate(); len(errs) != 1 || errs[0].Field != "b" || errs[0].Warning {
		t.Errorf("self link: %v", errs)
	}
}
