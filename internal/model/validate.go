package model

import (
	"fmt"
	"net/mail"
	"net/url"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"
)

// Field limits, from DESIGN.md's validation table.
const (
	MaxTitleLen    = 200      // project name, task title (characters)
	MaxUserNameLen = 100      // user name (characters)
	MaxMarkdownLen = 64 << 10 // markdown fields (bytes)
	MaxURLLen      = 2 << 10  // each URL (bytes)
	MinPriority    = 1
	MaxPriority    = 5
)

// FieldError reports a problem with one field of a record.  A warning is a
// problem that does not stop the record being used, such as an unknown
// enumeration id.
type FieldError struct {
	Field   string
	Message string
	Warning bool
}

func (e *FieldError) Error() string {
	return e.Field + ": " + e.Message
}

func fieldErr(field, format string, args ...any) *FieldError {
	return &FieldError{Field: field, Message: fmt.Sprintf(format, args...)}
}

func fieldWarn(field, format string, args ...any) *FieldError {
	return &FieldError{Field: field, Message: fmt.Sprintf(format, args...), Warning: true}
}

// asError converts a *FieldError to an error without the typed-nil trap.
func asError(e *FieldError) error {
	if e == nil {
		return nil
	}
	return e
}

// CheckText checks a required single-line text field: 1 to max characters and
// no control characters.
func CheckText(field, s string, max int) error {
	return asError(checkText(field, s, max))
}

func checkText(field, s string, max int) *FieldError {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return fieldErr(field, "required")
	}
	if n > max {
		return fieldErr(field, "longer than %d characters", max)
	}
	if !utf8.ValidString(s) {
		return fieldErr(field, "not valid UTF-8")
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return fieldErr(field, "contains control characters")
		}
	}
	return nil
}

// CheckUserName checks a user's display name.
func CheckUserName(s string) error {
	return CheckText("name", s, MaxUserNameLen)
}

// CheckEmail checks that s is a single bare email address.
func CheckEmail(s string) error {
	return asError(checkEmail("email", s))
}

func checkEmail(field, s string) *FieldError {
	if s == "" {
		return fieldErr(field, "required")
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" || a.Address != s {
		return fieldErr(field, "not a valid email address")
	}
	return nil
}

// CheckUserID checks a user ID.
func CheckUserID(s string) error {
	return asError(checkUserID("id", s))
}

func checkUserID(field, s string) *FieldError {
	if !ValidUserID(s) {
		return fieldErr(field, "%q is not a valid user ID: 2-32 characters, a-z then a-z, 0-9, '.', '_' or '-'", s)
	}
	return nil
}

func checkProjectID(field, s string) *FieldError {
	if !ValidProjectID(s) {
		return fieldErr(field, "%q is not a valid project ID: 2-10 characters, A-Z then A-Z or 0-9", s)
	}
	return nil
}

// CheckRole checks a role id.
func CheckRole(s string) error {
	if !Roles.Valid(s) {
		return fieldErr("role", "unknown role %q", s)
	}
	return nil
}

// checkEnum checks a required enumerated field as stored on disk.  An unknown
// id is only a warning: it should never occur, but if it does it is
// preserved rather than rejected.
func checkEnum(field string, e *Enum, id string) *FieldError {
	if id == "" {
		return fieldErr(field, "required")
	}
	if !e.Valid(id) {
		return fieldWarn(field, "unknown %s %q", e.Name(), id)
	}
	return nil
}

func checkMarkdown(field, s string) *FieldError {
	if len(s) > MaxMarkdownLen {
		return fieldErr(field, "longer than %d bytes", MaxMarkdownLen)
	}
	if !utf8.ValidString(s) {
		return fieldErr(field, "not valid UTF-8")
	}
	return nil
}

func checkURL(field, s string) *FieldError {
	if len(s) > MaxURLLen {
		return fieldErr(field, "longer than %d bytes", MaxURLLen)
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fieldErr(field, "%q is not an absolute http or https URL", s)
	}
	return nil
}

func checkVersion(field string, v int) *FieldError {
	if v < 1 {
		return fieldErr(field, "must be 1 or more")
	}
	return nil
}

func checkTime(field string, t time.Time) *FieldError {
	if t.IsZero() {
		return fieldErr(field, "required")
	}
	if t.Location() != time.UTC {
		return fieldWarn(field, "not in UTC")
	}
	return nil
}

// problems collects field errors, skipping nils.
type problems []*FieldError

func (p *problems) add(errs ...*FieldError) {
	for _, e := range errs {
		if e != nil {
			*p = append(*p, e)
		}
	}
}

// Validate checks the dataset metadata.
func (m *Meta) Validate() []*FieldError {
	var p problems
	if m.DatasetID == "" {
		p.add(fieldErr("dataset_id", "required"))
	}
	p.add(checkTime("created", m.Created))
	return p
}

// Validate checks a project's own fields.
func (pr *Project) Validate() []*FieldError {
	var p problems
	p.add(
		checkProjectID("id", pr.ID),
		checkVersion("version", pr.Version),
		checkText("name", pr.Name, MaxTitleLen),
		checkEnum("state", ProjectStates, pr.State),
		checkMarkdown("description", pr.Description),
		checkTime("created", pr.Created),
		checkTime("modified", pr.Modified),
	)
	for _, typ := range sortedKeys(pr.URLs) {
		field := "urls." + typ
		if !URLTypes.Valid(typ) {
			p.add(fieldWarn(field, "unknown %s %q", URLTypes.Name(), typ))
		}
		for i, u := range pr.URLs[typ] {
			p.add(checkURL(fmt.Sprintf("%s[%d]", field, i), u))
		}
	}
	return p
}

// Validate checks a user profile's own fields.
func (u *User) Validate() []*FieldError {
	var p problems
	p.add(
		checkUserID("id", u.ID),
		checkVersion("version", u.Version),
		checkText("name", u.Name, MaxUserNameLen),
		checkEmail("email", u.Email),
		checkEnum("role", Roles, u.Role),
		checkTime("created", u.Created),
		checkTime("modified", u.Modified),
	)
	return p
}

// Validate checks a task's own fields and those of its comments.  References
// to users are checked for form only; whether they exist is the caller's
// concern.
func (t *Task) Validate() []*FieldError {
	var p problems
	if _, _, err := ParseTaskID(t.ID); err != nil {
		p.add(fieldErr("id", "%q is not a valid task ID", t.ID))
	}
	p.add(
		checkVersion("version", t.Version),
		checkText("title", t.Title, MaxTitleLen),
		checkEnum("type", TaskTypes, t.Type),
		checkMarkdown("description", t.Description),
		checkUserID("creator", t.Creator),
		checkUserID("owner", t.Owner),
		checkEnum("state", TaskStates, t.State),
		checkTime("created", t.Created),
		checkTime("modified", t.Modified),
	)

	// A substate is required if and only if the state has substates.  With
	// an unknown state there is no rule to apply.
	if TaskStates.Valid(t.State) {
		if subs, ok := Substates[t.State]; ok {
			p.add(checkEnum("substate", subs, t.Substate))
		} else if t.Substate != "" {
			p.add(fieldErr("substate", "state %q has no substates", t.State))
		}
	}

	if t.LastCommentID < 0 {
		p.add(fieldErr("last_comment_id", "must be 0 or more"))
	}
	if t.Priority < MinPriority || t.Priority > MaxPriority {
		p.add(fieldErr("priority", "must be %d to %d", MinPriority, MaxPriority))
	}

	seen := make(map[int]bool, len(t.Comments))
	for i := range t.Comments {
		c := &t.Comments[i]
		field := fmt.Sprintf("comments[%d]", i)
		if c.ID < 1 {
			p.add(fieldErr(field+".id", "must be 1 or more"))
		} else if seen[c.ID] {
			p.add(fieldErr(field+".id", "duplicate comment ID %d", c.ID))
		}
		seen[c.ID] = true
		p.add(
			checkVersion(field+".version", c.Version),
			checkUserID(field+".commenter", c.Commenter),
			checkTime(field+".created", c.Created),
			checkTime(field+".modified", c.Modified),
			checkMarkdown(field+".text", c.Text),
		)
		if c.Text == "" {
			p.add(fieldErr(field+".text", "required"))
		}
	}
	return p
}

// Validate checks a user's secrets.
func (a *Auth) Validate() []*FieldError {
	var p problems
	p.add(checkUserID("user", a.User))
	if a.Password == "" {
		p.add(fieldErr("password", "required"))
	}
	seen := make(map[string]bool, len(a.Tokens))
	for i, tok := range a.Tokens {
		field := fmt.Sprintf("tokens[%d]", i)
		if tok.ID == "" {
			p.add(fieldErr(field+".id", "required"))
		} else if seen[tok.ID] {
			p.add(fieldErr(field+".id", "duplicate token ID %q", tok.ID))
		}
		seen[tok.ID] = true
		if tok.Hash == "" {
			p.add(fieldErr(field+".hash", "required"))
		}
		p.add(checkTime(field+".created", tok.Created))
	}
	return p
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
