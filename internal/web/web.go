// Package web is DockIt's human interface: server-rendered HTML with Go
// templates and plain forms.  Like the REST API, it is a thin layer over the
// service; every rule lives there.
package web

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/ratelimit"
	"github.com/pdutton/DockIt/internal/service"
)

//go:embed templates static
var embedded embed.FS

// maxForm bounds form bodies: a 64 KiB markdown field, URL-encoded, plus
// the rest of the form.
const maxForm = 1 << 20

// Login rate limits.  Per user, to slow guessing one account; per address,
// to slow spraying many.
const (
	loginFailsPerUser = 5
	loginFailsPerAddr = 20
	loginFailWindow   = 5 * time.Minute
)

// Options configures the Web UI.
type Options struct {
	// Secure marks the session cookie Secure.  Set it whenever users reach
	// DockIt over HTTPS, including through a TLS-terminating proxy.
	Secure bool
	// DevUser, if set, logs every request in as this user.  Development
	// only; the caller ensures the server is bound to localhost.
	DevUser string
	Logger  *slog.Logger
	// Version is the build version, shown in the page header.
	Version string
	// Assets overrides the embedded templates and static files (for tests).
	Assets fs.FS
}

// Web serves the Web UI.
type Web struct {
	svc         *service.Service
	opts        Options
	log         *slog.Logger
	sessions    *sessions
	submissions *submissions
	userFails   *ratelimit.Limiter
	addrFails   *ratelimit.Limiter
	pages       map[string]*template.Template
	mux         *http.ServeMux
}

// New returns the Web UI handler.
func New(svc *service.Service, opts Options) (*Web, error) {
	w := &Web{
		svc:         svc,
		opts:        opts,
		log:         opts.Logger,
		sessions:    newSessions(),
		submissions: newSubmissions(),
		userFails:   ratelimit.New(loginFailsPerUser, loginFailWindow),
		addrFails:   ratelimit.New(loginFailsPerAddr, loginFailWindow),
		mux:         http.NewServeMux(),
	}
	if w.log == nil {
		w.log = slog.New(slog.DiscardHandler)
	}
	if w.opts.Version == "" {
		w.opts.Version = "dev"
	}
	assets := opts.Assets
	if assets == nil {
		assets = embedded
	}
	var err error
	if w.pages, err = parseTemplates(assets); err != nil {
		return nil, err
	}
	static, err := fs.Sub(assets, "static")
	if err != nil {
		return nil, err
	}
	w.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(static)))
	w.routes()
	return w, nil
}

// ctx is what an authenticated handler knows about its request.
type ctx struct {
	session *session
	me      *model.User
}

// pageFunc handles an authenticated request.  It returns an error to be
// shown as an error page, or nil once it has responded.
type pageFunc func(w http.ResponseWriter, r *http.Request, c *ctx) error

func (w *Web) routes() {
	w.mux.HandleFunc("GET /login", w.loginPage)
	w.mux.HandleFunc("POST /login", w.login)
	w.mux.HandleFunc("POST /logout", w.logout)

	h := func(pattern string, f pageFunc) { w.mux.Handle(pattern, w.authenticated(f)) }
	h("GET /{$}", w.projectList)
	h("GET /projects/new", w.projectNew)
	h("POST /projects", w.projectCreate)
	h("GET /projects/{pid}", w.projectView)
	h("GET /projects/{pid}/edit", w.projectEdit)
	h("POST /projects/{pid}", w.projectUpdate)

	h("GET /projects/{pid}/tasks/new", w.taskNew)
	h("POST /projects/{pid}/tasks", w.taskCreate)
	h("GET /tasks/{tid}", w.taskView)
	h("POST /tasks/{tid}", w.taskUpdate)
	h("POST /tasks/{tid}/comments", w.commentAdd)
	h("POST /tasks/{tid}/comments/{cid}", w.commentEdit)
	h("POST /tasks/{tid}/comments/{cid}/delete", w.commentDelete)

	h("GET /users", w.userList)
	h("GET /users/new", w.userNew)
	h("POST /users", w.userCreate)
	h("GET /users/{uid}", w.userEdit)
	h("POST /users/{uid}", w.userUpdate)
	h("POST /users/{uid}/password", w.userResetPassword)

	h("GET /account", w.account)
	h("POST /account/password", w.accountPassword)
	h("POST /account/tokens", w.tokenCreate)
	h("POST /account/tokens/{id}/delete", w.tokenRevoke)

	w.mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		w.errorPage(rw, r, nil, http.StatusNotFound, "There is no such page.")
	})
}

// contentSecurityPolicy allows nothing inline and nothing from elsewhere,
// except images in markdown, which may come from any https site.
const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' https:; " +
	"form-action 'self'; frame-ancestors 'none'; base-uri 'none'"

func (w *Web) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	h := rw.Header()
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Frame-Options", "DENY")
	// Browsers send Origin on cross-site POSTs.  Rejecting a foreign one
	// covers the login form, which has no session and so no CSRF token yet.
	if r.Method == http.MethodPost {
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				http.Error(rw, "cross-origin request refused", http.StatusForbidden)
				return
			}
		}
	}
	w.mux.ServeHTTP(rw, r)
}

// authenticated resolves the session and runs f.  Unauthenticated requests
// are sent to the login page.  Every POST must carry the session's CSRF
// token, and a POST that repeats an earlier one gets its response instead of
// running again (see once).  A user who must change their password can
// reach only the account page until they do.
func (w *Web) authenticated(f pageFunc) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		ss := w.session(r)
		if ss == nil && w.opts.DevUser != "" {
			ss = w.sessions.create(w.opts.DevUser, false)
			w.setSessionCookie(rw, ss)
		}
		if ss == nil {
			if r.Method == http.MethodGet {
				http.Redirect(rw, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			} else {
				http.Redirect(rw, r, "/login", http.StatusSeeOther)
			}
			return
		}
		// The user may have been deactivated since logging in.
		me, err := w.svc.User(ss.user, ss.user)
		if err != nil {
			w.sessions.delete(ss.id)
			w.clearSessionCookie(rw)
			http.Redirect(rw, r, "/login", http.StatusSeeOther)
			return
		}
		c := &ctx{session: ss, me: me}

		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(rw, r.Body, maxForm)
			if err := r.ParseForm(); err != nil {
				w.errorPage(rw, r, c, http.StatusBadRequest, "The form could not be read.")
				return
			}
			if !validCSRF(ss, r.PostForm.Get("csrf")) {
				w.errorPage(rw, r, c, http.StatusForbidden,
					"The form has expired or did not come from DockIt. Go back, reload the page, and try again.")
				return
			}
		}
		if ss.mustChange && !strings.HasPrefix(r.URL.Path, "/account") {
			http.Redirect(rw, r, "/account", http.StatusSeeOther)
			return
		}

		run := func(rw http.ResponseWriter) {
			if err := f(rw, r, c); err != nil {
				w.fail(rw, r, c, err)
			}
		}
		if r.Method == http.MethodPost && r.PostForm.Get("submission") != "" {
			w.once(rw, r, submissionID(ss, r), run)
			return
		}
		run(rw)
	})
}

// fail shows an error page for an error a handler did not deal with itself.
func (w *Web) fail(rw http.ResponseWriter, r *http.Request, c *ctx, err error) {
	switch {
	case errors.Is(err, service.ErrNotFound):
		w.errorPage(rw, r, c, http.StatusNotFound, "There is no such page.")
	case errors.Is(err, service.ErrForbidden):
		w.errorPage(rw, r, c, http.StatusForbidden, "You do not have permission to do that.")
	default:
		w.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		w.errorPage(rw, r, c, http.StatusInternalServerError, "Something went wrong. The error has been logged.")
	}
}

func (w *Web) errorPage(rw http.ResponseWriter, r *http.Request, c *ctx, status int, msg string) {
	w.render(rw, r, c, status, "error", http.StatusText(status), msg)
}

// view is the data every page template receives.
type view struct {
	Title      string
	Me         *model.User
	CSRF       string
	Submission string // the submission key for this page's forms
	Path       string
	Data       any
	Version    string       // the build version
	Format     model.Format // the dataset format; serve brings every dataset to the current one
}

// render executes a page into a buffer first, so a template error becomes a
// clean error page rather than half a page.
func (w *Web) render(rw http.ResponseWriter, r *http.Request, c *ctx, status int, page, title string, data any) {
	v := view{Title: title, Path: r.URL.Path, Data: data, Version: w.opts.Version, Format: model.FormatCurrent}
	if c != nil {
		v.Me, v.CSRF, v.Submission = c.me, c.session.csrf, randomToken()
	}
	t := w.pages[page]
	var buf bytes.Buffer
	if t == nil {
		w.log.Error("no such template", "page", page)
		http.Error(rw, "internal error", http.StatusInternalServerError)
		return
	}
	if err := t.ExecuteTemplate(&buf, "layout", v); err != nil {
		w.log.Error("rendering page", "page", page, "err", err)
		http.Error(rw, "internal error", http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.WriteHeader(status)
	buf.WriteTo(rw)
}

// redirect answers a successful POST (post/redirect/get).
func redirect(rw http.ResponseWriter, r *http.Request, path string) error {
	http.Redirect(rw, r, path, http.StatusSeeOther)
	return nil
}

func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// safeNext returns a local path to go to after login, never another site.
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/\\") {
		return "/"
	}
	return next
}

// Login and logout.

type loginData struct {
	User  string
	Next  string
	Error string
}

func (w *Web) loginPage(rw http.ResponseWriter, r *http.Request) {
	if w.session(r) != nil {
		http.Redirect(rw, r, "/", http.StatusSeeOther)
		return
	}
	w.render(rw, r, nil, http.StatusOK, "login", "Log in", loginData{Next: safeNext(r.URL.Query().Get("next"))})
}

func (w *Web) login(rw http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(rw, r.Body, maxForm)
	if err := r.ParseForm(); err != nil {
		w.errorPage(rw, r, nil, http.StatusBadRequest, "The form could not be read.")
		return
	}
	user := strings.TrimSpace(r.PostForm.Get("user"))
	data := loginData{User: user, Next: safeNext(r.PostForm.Get("next"))}
	addr := clientAddr(r)

	blockedU, waitU := w.userFails.Blocked(user)
	blockedA, waitA := w.addrFails.Blocked(addr)
	if blockedU || blockedA {
		wait := max(waitU, waitA)
		data.Error = fmt.Sprintf("Too many failed attempts. Try again in %d minutes.", int(wait.Minutes())+1)
		w.render(rw, r, nil, http.StatusTooManyRequests, "login", "Log in", data)
		return
	}

	u, mustChange, err := w.svc.Login(user, r.PostForm.Get("password"))
	if err != nil {
		w.userFails.Fail(user)
		w.addrFails.Fail(addr)
		data.Error = "Unknown user ID or wrong password, or the account is deactivated."
		w.render(rw, r, nil, http.StatusUnauthorized, "login", "Log in", data)
		return
	}
	w.userFails.Reset(user)

	// A fresh session ID on every login prevents session fixation.
	if old := w.session(r); old != nil {
		w.sessions.delete(old.id)
	}
	ss := w.sessions.create(u.ID, mustChange)
	w.setSessionCookie(rw, ss)
	if mustChange {
		data.Next = "/account"
	}
	http.Redirect(rw, r, data.Next, http.StatusSeeOther)
}

func (w *Web) logout(rw http.ResponseWriter, r *http.Request) {
	ss := w.session(r)
	if ss != nil {
		r.Body = http.MaxBytesReader(rw, r.Body, maxForm)
		if r.ParseForm() != nil || !validCSRF(ss, r.PostForm.Get("csrf")) {
			w.errorPage(rw, r, nil, http.StatusForbidden, "The form has expired. Go back, reload the page, and try again.")
			return
		}
		w.sessions.delete(ss.id)
	}
	w.clearSessionCookie(rw)
	http.Redirect(rw, r, "/login", http.StatusSeeOther)
}
