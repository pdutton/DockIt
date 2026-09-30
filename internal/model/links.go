package model

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

// Link types.  A link is stored as "a <type> b": "ACME-7 blocks ACME-6".
const (
	LinkRelated    = "related"
	LinkConflicts  = "conflicts"
	LinkDepends    = "depends"
	LinkBlocks     = "blocks"
	LinkDuplicates = "duplicates"
)

// LinkTypes are the link types, displayed as they read from a.
var LinkTypes = newEnum("link type",
	EnumValue{LinkBlocks, "Blocks"},
	EnumValue{LinkDepends, "Depends on"},
	EnumValue{LinkDuplicates, "Duplicates"},
	EnumValue{LinkRelated, "Related to"},
	EnumValue{LinkConflicts, "Conflicts with"},
)

// undirected link types read the same from either end, so a and b are
// stored in order: the lower task ID is a.
var undirected = map[string]bool{LinkRelated: true, LinkConflicts: true}

// A Relation is a link type as seen from one of its tasks, such as
// "blocked_by" for the b of a "blocks" link.  Relations are what the Web UI
// and the REST API work in, so a link can be added or removed from either end.
type Relation struct {
	ID      string // stable id, used by the REST API
	Type    string // the link type
	Reverse bool   // the task is the link's b
	Heading string // the task's links are grouped under this: "Blocked by"
	Phrase  string // reads "<task> <phrase> <other task>": "is blocked by"
}

// Relations in display order.  An undirected type has one relation, which
// is not reversed.
var Relations = []Relation{
	{"blocks", LinkBlocks, false, "Blocks", "blocks"},
	{"blocked_by", LinkBlocks, true, "Blocked by", "is blocked by"},
	{"depends_on", LinkDepends, false, "Depends on", "depends on"},
	{"dependency_of", LinkDepends, true, "Dependency of", "is a dependency of"},
	{"duplicates", LinkDuplicates, false, "Duplicates", "duplicates"},
	{"duplicated_by", LinkDuplicates, true, "Duplicated by", "is duplicated by"},
	{"related", LinkRelated, false, "Related to", "is related to"},
	{"conflicts", LinkConflicts, false, "Conflicts with", "conflicts with"},
}

// RelationByID returns the relation with id.
func RelationByID(id string) (Relation, bool) {
	for _, r := range Relations {
		if r.ID == id {
			return r, true
		}
	}
	return Relation{}, false
}

// RelationOf returns the relation of a link type seen from a (reverse false)
// or b (reverse true).  An unknown type, which should never occur, gets a
// relation whose ID is the type itself, with no heading or phrase.
func RelationOf(typ string, reverse bool) Relation {
	if undirected[typ] {
		reverse = false
	}
	for _, r := range Relations {
		if r.Type == typ && r.Reverse == reverse {
			return r
		}
	}
	return Relation{ID: typ, Type: typ, Reverse: reverse}
}

// Link is a relationship between two tasks, which may be in different
// projects.  All links are kept in links.yaml at the dataset root.
type Link struct {
	A       string    `yaml:"a" json:"a"`
	Type    string    `yaml:"type" json:"type"`
	B       string    `yaml:"b" json:"b"`
	Creator string    `yaml:"creator" json:"creator"`
	Created time.Time `yaml:"created" json:"created"`
}

// NewLink returns the link that relation rel from task tid to task other
// stands for, stored the right way round.
func NewLink(tid string, rel Relation, other string) Link {
	l := Link{A: tid, Type: rel.Type, B: other}
	if rel.Reverse {
		l.A, l.B = other, tid
	}
	return l.Canonical()
}

// Canonical returns the link with a and b in stored order: an undirected
// link has the lower task ID as a.
func (l Link) Canonical() Link {
	if undirected[l.Type] && CompareTaskIDs(l.A, l.B) > 0 {
		l.A, l.B = l.B, l.A
	}
	return l
}

// Same reports whether l and m are the same link, whoever made them and
// whenever.
func (l Link) Same(m Link) bool {
	l, m = l.Canonical(), m.Canonical()
	return l.A == m.A && l.Type == m.Type && l.B == m.B
}

// CompareLinks orders links as they are stored: by a, then type, then b.
func CompareLinks(l, m Link) int {
	if c := CompareTaskIDs(l.A, m.A); c != 0 {
		return c
	}
	if c := strings.Compare(l.Type, m.Type); c != 0 {
		return c
	}
	return CompareTaskIDs(l.B, m.B)
}

// CompareTaskIDs orders task IDs by project, then by number as a number, so
// WEB-2 comes before WEB-10.  Malformed IDs sort after well-formed ones, by
// their text.
func CompareTaskIDs(a, b string) int {
	pa, na, ea := ParseTaskID(a)
	pb, nb, eb := ParseTaskID(b)
	switch {
	case ea != nil && eb != nil:
		return strings.Compare(a, b)
	case ea != nil:
		return 1
	case eb != nil:
		return -1
	}
	if c := strings.Compare(pa, pb); c != 0 {
		return c
	}
	return cmp.Compare(na, nb)
}

// SortLinks puts links in stored order.
func SortLinks(links []Link) {
	slices.SortFunc(links, CompareLinks)
}
