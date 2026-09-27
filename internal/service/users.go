package service

import (
	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/model"
)

// oneTimePasswordLen is the length of passwords generated for new users and
// resets.
const oneTimePasswordLen = 20

// NewUser is the input for CreateUser.
type NewUser struct {
	ID    string
	Name  string
	Email string
	Role  string
}

// UserPatch is the input for UpdateUser.  Nil fields are left as they are.
type UserPatch struct {
	Name   *string
	Email  *string
	Role   *string
	Active *bool
}

// Users returns all users, ordered by ID.
func (s *Service) Users(actor string) ([]*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	return s.x.Users(), nil
}

// User returns user uid.
func (s *Service) User(actor, uid string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	u := s.x.User(uid)
	if u == nil {
		return nil, ErrNotFound
	}
	return u, nil
}

// CreateUser creates an active user with a one-time password, which is
// returned and must be changed at first login.  Admin only.
func (s *Service) CreateUser(actor string, in NewUser) (*model.User, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, adminOnly); err != nil {
		return nil, "", err
	}
	if s.x.User(in.ID) != nil {
		return nil, "", ErrExists
	}
	now := s.now()
	u := &model.User{
		ID:       in.ID,
		Version:  1,
		Name:     in.Name,
		Email:    in.Email,
		Role:     in.Role,
		Active:   true,
		Created:  now,
		Modified: now,
	}
	if err := validate(u.Validate(), all); err != nil {
		return nil, "", err
	}
	password := auth.GeneratePassword(oneTimePasswordLen)
	a := &model.Auth{User: u.ID, Password: auth.HashPassword(password), MustChangePassword: true}

	// Profile first: if the secrets then fail to write, the user exists but
	// cannot log in, and an admin can reset the password.
	if err := s.store.WriteUser(u); err != nil {
		return nil, "", err
	}
	s.x.PutUser(u)
	if err := s.store.WriteAuth(a); err != nil {
		return nil, "", err
	}
	s.x.PutAuth(a)
	return u.Clone(), password, nil
}

// UpdateUser changes a user's profile, role or active flag.  version is the
// version the change is based on.  Admin only.  The last active admin cannot
// be deactivated or demoted.
func (s *Service) UpdateUser(actor, uid string, version int, patch UserPatch) (*model.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, adminOnly); err != nil {
		return nil, err
	}
	u := s.x.User(uid)
	if u == nil {
		return nil, ErrNotFound
	}
	if err := checkVersion(version, u.Version, u); err != nil {
		return nil, err
	}

	changed := fieldSet{}
	set(&u.Name, patch.Name, "name", changed)
	set(&u.Email, patch.Email, "email", changed)
	set(&u.Role, patch.Role, "role", changed)
	set(&u.Active, patch.Active, "active", changed)
	if len(changed) == 0 {
		return u, nil
	}
	if err := validate(u.Validate(), changed.has); err != nil {
		return nil, err
	}
	if !(u.Active && u.Role == model.RoleAdmin) && s.isLastActiveAdmin(uid) {
		field := "role"
		if !u.Active {
			field = "active"
		}
		return nil, invalid(field, "cannot deactivate or demote the last active admin")
	}

	u.Version++
	u.Modified = s.now()
	if err := s.store.WriteUser(u); err != nil {
		return nil, err
	}
	s.x.PutUser(u)
	return u.Clone(), nil
}

// isLastActiveAdmin reports whether uid is the only active admin.
func (s *Service) isLastActiveAdmin(uid string) bool {
	for _, u := range s.x.Users() {
		if u.Active && u.Role == model.RoleAdmin && u.ID != uid {
			return false
		}
	}
	return true
}
