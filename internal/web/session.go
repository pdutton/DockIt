package web

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

// Sessions live in memory only, so a restart logs everyone out.

const (
	cookieName = "dockit_session"
	// sessionIdle is how long a session lasts without use.
	sessionIdle = 7 * 24 * time.Hour
)

type session struct {
	id         string
	user       string
	csrf       string
	mustChange bool // the user must change their password before anything else
	lastSeen   time.Time
}

type sessions struct {
	mu  sync.Mutex
	m   map[string]*session
	now func() time.Time
}

func newSessions() *sessions {
	return &sessions{m: make(map[string]*session), now: time.Now}
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// create starts a session for user.
func (s *sessions) create(user string, mustChange bool) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire()
	ss := &session{id: randomToken(), user: user, csrf: randomToken(), mustChange: mustChange, lastSeen: s.now()}
	s.m[ss.id] = ss
	return ss
}

// get returns the session for a cookie value, touching it, or nil.
func (s *sessions) get(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss := s.m[id]
	if ss == nil {
		return nil
	}
	if s.now().Sub(ss.lastSeen) > sessionIdle {
		delete(s.m, id)
		return nil
	}
	ss.lastSeen = s.now()
	c := *ss
	return &c
}

func (s *sessions) delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, id)
}

// passwordChanged clears the must-change flag on every session of user.
func (s *sessions) passwordChanged(user string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ss := range s.m {
		if ss.user == user {
			ss.mustChange = false
		}
	}
}

// expire drops idle sessions.  Caller holds s.mu.
func (s *sessions) expire() {
	for id, ss := range s.m {
		if s.now().Sub(ss.lastSeen) > sessionIdle {
			delete(s.m, id)
		}
	}
}

func (w *Web) setSessionCookie(rw http.ResponseWriter, ss *session) {
	http.SetCookie(rw, &http.Cookie{
		Name:     cookieName,
		Value:    ss.id,
		Path:     "/",
		HttpOnly: true,
		Secure:   w.opts.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (w *Web) clearSessionCookie(rw http.ResponseWriter) {
	http.SetCookie(rw, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   w.opts.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// session returns the request's session, or nil.
func (w *Web) session(r *http.Request) *session {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	return w.sessions.get(c.Value)
}

// validCSRF checks a form's CSRF token against the session's.
func validCSRF(ss *session, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(ss.csrf), []byte(token)) == 1
}
