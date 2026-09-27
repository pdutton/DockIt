package model

import (
	"slices"
	"time"

	"go.yaml.in/yaml/v3"
)

// Dataset format versions this build understands.  FormatCurrent is written
// by init and upgrade; anything from FormatMin up to FormatCurrent can be
// upgraded, and only FormatCurrent is served.
const (
	FormatMin     = 1
	FormatCurrent = 1
)

// DefaultPriority is the priority of a task when none is given.
const DefaultPriority = 3

// Struct field order below is the order fields are written to disk, so that
// files diff cleanly.  Do not reorder fields casually.

// Meta is the dataset metadata in dockit.yaml.
type Meta struct {
	Format    int       `yaml:"format" json:"format"`
	DatasetID string    `yaml:"dataset_id" json:"dataset_id"`
	Created   time.Time `yaml:"created" json:"created"`
}

// Project is a project record, projects/<id>/<id>.yaml.
type Project struct {
	ID          string    `yaml:"id" json:"id"`
	Version     int       `yaml:"version" json:"version"`
	Name        string    `yaml:"name" json:"name"`
	State       string    `yaml:"state" json:"state"`
	Description string    `yaml:"description,omitempty" json:"description,omitempty"`
	URLs        URLs      `yaml:"urls,omitempty" json:"urls,omitempty"`
	Created     time.Time `yaml:"created" json:"created"`
	Modified    time.Time `yaml:"modified" json:"modified"`
}

// URLs maps a URL type to an ordered list of URLs; the first URL of a type is
// its primary.  Keys are written in the URL type enumeration's built-in order
// and empty lists are omitted.
type URLs map[string][]string

// MarshalYAML writes keys in built-in order.  Unknown keys, which should never
// occur but are preserved if they do, follow in lexical order.
func (u URLs) MarshalYAML() (any, error) {
	keys := make([]string, 0, len(u))
	for k, v := range u {
		if len(v) > 0 {
			keys = append(keys, k)
		}
	}
	slices.SortFunc(keys, func(a, b string) int {
		ia, ib := URLTypes.Index(a), URLTypes.Index(b)
		switch {
		case ia >= 0 && ib >= 0:
			return ia - ib
		case ia >= 0:
			return -1
		case ib >= 0:
			return 1
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	})

	n := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		var list yaml.Node
		if err := list.Encode(u[k]); err != nil {
			return nil, err
		}
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
			&list)
	}
	return n, nil
}

// User is a user profile, users/<id>.yaml.  It holds no secrets.
type User struct {
	ID       string    `yaml:"id" json:"id"`
	Version  int       `yaml:"version" json:"version"`
	Name     string    `yaml:"name" json:"name"`
	Email    string    `yaml:"email" json:"email"`
	Role     string    `yaml:"role" json:"role"`
	Active   bool      `yaml:"active" json:"active"`
	Created  time.Time `yaml:"created" json:"created"`
	Modified time.Time `yaml:"modified" json:"modified"`
}

// Task is a task record, including its comments,
// projects/<pid>/tasks/<id>.yaml.
type Task struct {
	ID            string    `yaml:"id" json:"id"`
	Version       int       `yaml:"version" json:"version"`
	Title         string    `yaml:"title" json:"title"`
	Description   string    `yaml:"description,omitempty" json:"description,omitempty"`
	Creator       string    `yaml:"creator" json:"creator"`
	Owner         string    `yaml:"owner" json:"owner"`
	State         string    `yaml:"state" json:"state"`
	Substate      string    `yaml:"substate,omitempty" json:"substate,omitempty"`
	Priority      int       `yaml:"priority" json:"priority"`
	Created       time.Time `yaml:"created" json:"created"`
	Modified      time.Time `yaml:"modified" json:"modified"`
	LastCommentID int       `yaml:"last_comment_id,omitempty" json:"last_comment_id,omitempty"` // highest comment ID ever used
	Comments      []Comment `yaml:"comments,omitempty" json:"comments,omitempty"`
}

// Comment is a comment on a task.  Its ID is a per-task sequence that is
// never reused.
type Comment struct {
	ID        int       `yaml:"id" json:"id"`
	Version   int       `yaml:"version" json:"version"`
	Commenter string    `yaml:"commenter" json:"commenter"`
	Created   time.Time `yaml:"created" json:"created"`
	Modified  time.Time `yaml:"modified" json:"modified"`
	Text      string    `yaml:"text" json:"text"`
}

// Auth holds a user's secrets, auth/<user id>.yaml.  Nothing in it is
// reversible.
type Auth struct {
	User               string  `yaml:"user" json:"user"`
	Password           string  `yaml:"password" json:"-"`
	MustChangePassword bool    `yaml:"must_change_password,omitempty" json:"must_change_password,omitempty"`
	Tokens             []Token `yaml:"tokens,omitempty" json:"tokens,omitempty"`
}

// Token is an API token.  Only its hash is stored.
type Token struct {
	ID      string    `yaml:"id" json:"id"`
	Name    string    `yaml:"name" json:"name"`
	Hash    string    `yaml:"hash" json:"hash,omitempty"`
	Created time.Time `yaml:"created" json:"created"`
}

// Now returns the current time as stored in the dataset: UTC, whole seconds.
func Now() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

// Clone returns a deep copy of the project.
func (p *Project) Clone() *Project {
	c := *p
	if p.URLs != nil {
		c.URLs = make(URLs, len(p.URLs))
		for k, v := range p.URLs {
			c.URLs[k] = slices.Clone(v)
		}
	}
	return &c
}

// Clone returns a copy of the user.
func (u *User) Clone() *User {
	c := *u
	return &c
}

// Clone returns a deep copy of the task, including its comments.
func (t *Task) Clone() *Task {
	c := *t
	c.Comments = slices.Clone(t.Comments)
	return &c
}

// Clone returns a deep copy of the user's secrets.
func (a *Auth) Clone() *Auth {
	c := *a
	c.Tokens = slices.Clone(a.Tokens)
	return &c
}
