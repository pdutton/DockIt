// Package service holds every rule DockIt enforces: permissions, validation,
// optimistic concurrency and ID assignment.  The Web UI and the REST API are
// peers over this package, so the two can never disagree about the rules.
//
// Reads are served from the in-memory index.  Writes are serialized, go to
// disk first, and update the index only after the file is safely written.
package service

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/pdutton/DockIt/internal/index"
	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

// Errors returned by the service.  Interfaces map them to their own terms,
// for example HTTP status codes.
var (
	// ErrNotFound: the record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrForbidden: the actor may not do this, or is not an active user.
	ErrForbidden = errors.New("permission denied")
	// ErrExists: a record with that ID already exists.
	ErrExists = errors.New("already exists")
	// ErrVersionRequired: an update did not say which version it was based on.
	ErrVersionRequired = errors.New("version required")
	// ErrBadCredentials: a login or token was not accepted.  It deliberately
	// does not say why.
	ErrBadCredentials = errors.New("invalid credentials")
)

// ConflictError is returned when an update was based on an out-of-date
// version.  Current is a copy of the record as it is now.
type ConflictError struct {
	Current any
}

func (e *ConflictError) Error() string {
	return "the record was changed by someone else"
}

// ValidationError lists the fields that failed validation.
type ValidationError struct {
	Fields []*model.FieldError
}

func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		msgs[i] = f.Error()
	}
	return "invalid: " + strings.Join(msgs, "; ")
}

func invalid(field, format string, args ...any) *ValidationError {
	return &ValidationError{[]*model.FieldError{{Field: field, Message: fmt.Sprintf(format, args...)}}}
}

// OpenError is returned by Open when the dataset has errors.
type OpenError struct {
	Report *index.Report
}

func (e *OpenError) Error() string {
	return fmt.Sprintf("dataset has %d errors; run `dockit check` for details", e.Report.Errors())
}

// Service is an open dataset with the rules applied.  It is safe for
// concurrent use.
type Service struct {
	mu     sync.RWMutex // held for writing by every write, for reading by reads
	store  *store.Store
	x      *index.Index
	tokens map[string]tokenRef // token hash -> owner, for token logins
	now    func() time.Time
}

// Open locks the dataset at root and loads it.  It refuses a dataset with
// errors; warnings are returned for the caller to log.
func Open(root string) (*Service, *index.Report, error) {
	st, err := store.Open(root)
	if err != nil {
		return nil, nil, err
	}
	x, report := index.Load(root)
	if x == nil || !report.OK() {
		st.Close()
		return nil, report, &OpenError{report}
	}
	return newService(st, x), report, nil
}

func newService(st *store.Store, x *index.Index) *Service {
	s := &Service{store: st, x: x, now: model.Now}
	s.rebuildTokens()
	return s
}

// Close releases the dataset lock.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.store.Close()
}

// Permissions.  Roles are global; see DESIGN.md "Roles and Permissions".
var (
	anyone    = []string{model.RoleViewer, model.RoleMember, model.RoleAdmin}
	editors   = []string{model.RoleMember, model.RoleAdmin}
	adminOnly = []string{model.RoleAdmin}
)

// authorize returns the acting user if they exist, are active, and hold one
// of roles.  The caller must hold s.mu.
func (s *Service) authorize(actor string, roles []string) (*model.User, error) {
	u := s.x.User(actor)
	if u == nil || !u.Active || !slices.Contains(roles, u.Role) {
		return nil, ErrForbidden
	}
	return u, nil
}

// checkVersion implements optimistic concurrency: an update must name the
// version it was based on, and it must still be current.
func checkVersion(based, current int, record any) error {
	if based == 0 {
		return ErrVersionRequired
	}
	if based != current {
		return &ConflictError{Current: record}
	}
	return nil
}

// validate runs a record's own checks.  Errors always fail.  Warnings, such
// as an unknown enumeration id, fail only for fields this change set: an
// unknown id already on disk is preserved, but a client can never set one.
func validate(errs []*model.FieldError, changed func(field string) bool) error {
	var bad []*model.FieldError
	for _, e := range errs {
		if !e.Warning || changed(e.Field) {
			bad = append(bad, e)
		}
	}
	if len(bad) > 0 {
		return &ValidationError{bad}
	}
	return nil
}

// fieldSet reports whether a field path is covered by one of names, so that
// "urls" covers "urls.code[0]".
type fieldSet map[string]bool

func (f fieldSet) has(field string) bool {
	for name := range f {
		if field == name || strings.HasPrefix(field, name+".") || strings.HasPrefix(field, name+"[") {
			return true
		}
	}
	return false
}

func all(string) bool { return true }
