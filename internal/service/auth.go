package service

import (
	"slices"
	"unicode/utf8"

	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/model"
)

// MinPasswordLen is the shortest password a user may choose.
const MinPasswordLen = 8

// maxPasswordLen bounds the work an attacker can make argon2 do per attempt.
const maxPasswordLen = 256

// dummyHash is verified against when a login names no usable account, so
// that the response takes as long as a real attempt and does not reveal
// which user IDs exist.
var dummyHash = auth.HashPassword("dockit-dummy-password")

// tokenRef records whose a token is.
type tokenRef struct {
	user string
	id   string
}

// TokenInfo describes an API token without revealing it.
type TokenInfo = model.Token

// Login checks a user ID and password.  It returns the user and whether they
// must change their password before doing anything else.  Deactivated users
// cannot log in.  Rate limiting is the interface's job.
func (s *Service) Login(uid, password string) (*model.User, bool, error) {
	s.mu.RLock()
	u, a := s.x.User(uid), s.x.Auth(uid)
	s.mu.RUnlock()

	hash := dummyHash
	if a != nil {
		hash = a.Password
	}
	ok, err := auth.VerifyPassword(password, hash)
	if err != nil || !ok || a == nil || u == nil || !u.Active {
		return nil, false, ErrBadCredentials
	}
	return u, a.MustChangePassword, nil
}

// ChangePassword sets the actor's own password, given the current one.  It
// clears the must-change flag.  Anyone may change their own password.
func (s *Service) ChangePassword(actor, current, password string) error {
	if _, _, err := s.Login(actor, current); err != nil {
		return err
	}
	if err := checkPassword(password); err != nil {
		return err
	}
	if password == current {
		return invalid("password", "must differ from the current password")
	}
	hash := auth.HashPassword(password) // slow, so outside the lock

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return err
	}
	a := s.x.Auth(actor)
	if a == nil {
		return ErrBadCredentials
	}
	a.Password = hash
	a.MustChangePassword = false
	return s.writeAuth(a)
}

// ResetPassword gives user uid a new one-time password, which is returned and
// must be changed at next login.  Admin only.  This is how an admin helps a
// user who has forgotten their password.
func (s *Service) ResetPassword(actor, uid string) (string, error) {
	password := auth.GeneratePassword(oneTimePasswordLen)
	hash := auth.HashPassword(password)

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, adminOnly); err != nil {
		return "", err
	}
	if s.x.User(uid) == nil {
		return "", ErrNotFound
	}
	a := s.x.Auth(uid)
	if a == nil {
		a = &model.Auth{User: uid}
	}
	a.Password = hash
	a.MustChangePassword = true
	if err := s.writeAuth(a); err != nil {
		return "", err
	}
	return password, nil
}

func checkPassword(p string) error {
	n := utf8.RuneCountInString(p)
	switch {
	case n < MinPasswordLen:
		return invalid("password", "must be at least %d characters", MinPasswordLen)
	case n > maxPasswordLen:
		return invalid("password", "must be at most %d characters", maxPasswordLen)
	}
	return nil
}

// Tokens returns the actor's API tokens, without their secrets, oldest first.
func (s *Service) Tokens(actor string) ([]TokenInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return nil, err
	}
	a := s.x.Auth(actor)
	if a == nil {
		return nil, nil
	}
	out := make([]TokenInfo, len(a.Tokens))
	for i, t := range a.Tokens {
		t.Hash = ""
		out[i] = t
	}
	return out, nil
}

// CreateToken creates an API token for the actor.  The token is returned
// once and never stored; only its hash is kept.  Tokens carry the user's
// role.
func (s *Service) CreateToken(actor, name string) (TokenInfo, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return TokenInfo{}, "", err
	}
	if err := model.CheckText("name", name, model.MaxTitleLen); err != nil {
		return TokenInfo{}, "", &ValidationError{[]*model.FieldError{err.(*model.FieldError)}}
	}
	a := s.x.Auth(actor)
	if a == nil {
		return TokenInfo{}, "", ErrForbidden // no secrets at all: cannot log in either
	}
	secret, hash := auth.NewToken()
	t := model.Token{ID: s.newTokenID(a), Name: name, Hash: hash, Created: s.now()}
	a.Tokens = append(a.Tokens, t)
	if err := s.writeAuth(a); err != nil {
		return TokenInfo{}, "", err
	}
	t.Hash = ""
	return t, secret, nil
}

func (s *Service) newTokenID(a *model.Auth) string {
	for {
		id := auth.NewTokenID()
		if !slices.ContainsFunc(a.Tokens, func(t model.Token) bool { return t.ID == id }) {
			return id
		}
	}
}

// RevokeToken deletes one of the actor's API tokens.  It stops working
// immediately.
func (s *Service) RevokeToken(actor, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.authorize(actor, anyone); err != nil {
		return err
	}
	a := s.x.Auth(actor)
	if a == nil {
		return ErrNotFound
	}
	i := slices.IndexFunc(a.Tokens, func(t model.Token) bool { return t.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	a.Tokens = slices.Delete(a.Tokens, i, i+1)
	return s.writeAuth(a)
}

// TokenLogin returns the user an API token belongs to.  Tokens of
// deactivated users do not work.
func (s *Service) TokenLogin(token string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ref, ok := s.tokens[auth.HashToken(token)]
	if !ok {
		return nil, ErrBadCredentials
	}
	u := s.x.User(ref.user)
	if u == nil || !u.Active {
		return nil, ErrBadCredentials
	}
	return u, nil
}

// writeAuth writes a user's secrets and refreshes the token lookup.  The
// caller must hold s.mu for writing.
func (s *Service) writeAuth(a *model.Auth) error {
	if err := validate(a.Validate(), all); err != nil {
		return err
	}
	if err := s.store.WriteAuth(a); err != nil {
		return err
	}
	s.x.PutAuth(a)
	s.rebuildTokens()
	return nil
}

// rebuildTokens rebuilds the token hash lookup from the index.  Token counts
// are tiny, so rebuilding on every change is simpler than patching.
func (s *Service) rebuildTokens() {
	s.tokens = make(map[string]tokenRef)
	for _, u := range s.x.Users() {
		if a := s.x.Auth(u.ID); a != nil {
			for _, t := range a.Tokens {
				s.tokens[t.Hash] = tokenRef{user: u.ID, id: t.ID}
			}
		}
	}
}
