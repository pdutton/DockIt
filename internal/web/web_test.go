package web

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/service"
	"github.com/pdutton/DockIt/internal/store"
)

type fixture struct {
	t   *testing.T
	svc *service.Service
	srv *httptest.Server
	pw  map[string]string
}

// newFixture serves a dataset with users admin (password "admin-password",
// no change required), mem (member) and view (viewer), who have one-time
// passwords, and project WEB.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	now := model.Now()
	admin := &model.User{ID: "admin", Version: 1, Name: "Admin", Email: "admin@example.com",
		Role: model.RoleAdmin, Active: true, Created: now, Modified: now}
	if err := store.Init(root, admin, &model.Auth{User: "admin", Password: auth.HashPassword("admin-password")}); err != nil {
		t.Fatal(err)
	}
	svc, _, err := service.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	f := &fixture{t: t, svc: svc, pw: map[string]string{"admin": "admin-password"}}
	for _, u := range []service.NewUser{
		{ID: "mem", Name: "Mem", Email: "mem@example.com", Role: model.RoleMember},
		{ID: "view", Name: "View", Email: "view@example.com", Role: model.RoleViewer},
	} {
		_, pw, err := svc.CreateUser("admin", u)
		if err != nil {
			t.Fatal(err)
		}
		f.pw[u.ID] = pw
	}
	if _, err := svc.CreateProject("admin", service.NewProject{ID: "WEB", Name: "Website",
		State: model.ProjectActive, Description: "The **site**"}); err != nil {
		t.Fatal(err)
	}
	w, err := New(svc, Options{})
	if err != nil {
		t.Fatal(err)
	}
	f.srv = httptest.NewServer(w)
	t.Cleanup(f.srv.Close)
	return f
}

// browser is a cookie-keeping client that does not follow redirects, and
// remembers the CSRF token of the last page it saw.
type browser struct {
	f    *fixture
	c    *http.Client
	csrf string
}

type page struct {
	status   int
	location string
	header   http.Header
	body     string
}

var csrfRe = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func (f *fixture) browser() *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{f: f, c: &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (b *browser) do(req *http.Request) page {
	b.f.t.Helper()
	resp, err := b.c.Do(req)
	if err != nil {
		b.f.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	p := page{resp.StatusCode, resp.Header.Get("Location"), resp.Header, string(body)}
	if m := csrfRe.FindStringSubmatch(p.body); m != nil {
		b.csrf = m[1]
	}
	return p
}

func (b *browser) get(path string) page {
	b.f.t.Helper()
	req, _ := http.NewRequest("GET", b.f.srv.URL+path, nil)
	return b.do(req)
}

// post submits a form, adding the CSRF token unless the form sets one.
// Fields are name, value pairs.
func (b *browser) post(path string, fields ...string) page {
	b.f.t.Helper()
	v := url.Values{}
	for i := 0; i+1 < len(fields); i += 2 {
		v.Add(fields[i], fields[i+1])
	}
	if !v.Has("csrf") {
		v.Set("csrf", b.csrf)
	}
	req, _ := http.NewRequest("POST", b.f.srv.URL+path, strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return b.do(req)
}

// login logs in and, for users with a one-time password, sets a new one.
func (f *fixture) login(user string) *browser {
	f.t.Helper()
	b := f.browser()
	p := b.post("/login", "user", user, "password", f.pw[user])
	if p.status != http.StatusSeeOther {
		f.t.Fatalf("login %s: %d %s", user, p.status, p.body)
	}
	if p.location == "/account" {
		b.get("/account")
		p = b.post("/account/password", "current", f.pw[user], "password", user+"-new-password", "confirm", user+"-new-password")
		if p.status != http.StatusSeeOther {
			f.t.Fatalf("change password %s: %d %s", user, p.status, p.body)
		}
		f.pw[user] = user + "-new-password"
	}
	b.get("/") // pick up a CSRF token
	return b
}

func (f *fixture) want(p page, status int, contains ...string) {
	f.t.Helper()
	if p.status != status {
		f.t.Errorf("status %d, want %d; body:\n%s", p.status, status, p.body)
		return
	}
	for _, s := range contains {
		if !strings.Contains(p.body, s) {
			f.t.Errorf("page does not contain %q:\n%s", s, p.body)
		}
	}
}

func TestSecurityHeaders(t *testing.T) {
	f := newFixture(t)
	p := f.browser().get("/login")
	for h, want := range map[string]string{
		"Content-Security-Policy": "script-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	} {
		if !strings.Contains(p.header.Get(h), want) {
			t.Errorf("%s = %q", h, p.header.Get(h))
		}
	}
	if s := f.browser().get("/static/style.css"); s.status != 200 || !strings.Contains(s.body, "--accent") {
		t.Errorf("static: %d", s.status)
	}
}

func TestLoginFlow(t *testing.T) {
	f := newFixture(t)
	b := f.browser()

	p := b.get("/projects/WEB")
	if p.status != http.StatusSeeOther || p.location != "/login?next=%2Fprojects%2FWEB" {
		t.Fatalf("unauthenticated: %d %s", p.status, p.location)
	}
	f.want(b.post("/login", "user", "admin", "password", "wrong", "next", "/projects/WEB"), 401, "wrong password")

	p = b.post("/login", "user", "admin", "password", "admin-password", "next", "/projects/WEB")
	if p.status != http.StatusSeeOther || p.location != "/projects/WEB" {
		t.Fatalf("login: %d %s", p.status, p.location)
	}
	f.want(b.get("/projects/WEB"), 200, "Website", "<strong>site</strong>")

	// No open redirect after login.
	b2 := f.browser()
	for _, next := range []string{"//evil.example", "https://evil.example", "/\\evil.example"} {
		p = b2.post("/login", "user", "admin", "password", "admin-password", "next", next)
		if p.location != "/" {
			t.Errorf("next=%q redirected to %q", next, p.location)
		}
	}

	b.get("/")
	p = b.post("/logout")
	if p.status != http.StatusSeeOther {
		t.Fatalf("logout: %d", p.status)
	}
	if p := b.get("/"); p.status != http.StatusSeeOther {
		t.Error("still logged in after logout")
	}
}

func TestMustChangePassword(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	p := b.post("/login", "user", "mem", "password", f.pw["mem"], "next", "/projects/WEB")
	if p.location != "/account" {
		t.Fatalf("one-time password login went to %q", p.location)
	}
	// Everything else redirects to the account page until the password changes.
	if p := b.get("/projects/WEB"); p.location != "/account" {
		t.Errorf("before change: %d %s", p.status, p.location)
	}
	f.want(b.get("/account"), 200, "one-time password")
	f.want(b.post("/account/password", "current", f.pw["mem"], "password", "short", "confirm", "short"), 422, "at least 8")
	f.want(b.post("/account/password", "current", f.pw["mem"], "password", "long-enough-1", "confirm", "long-enough-2"), 422, "differ")
	f.want(b.post("/account/password", "current", "wrong", "password", "long-enough", "confirm", "long-enough"), 422, "Current password is wrong")
	p = b.post("/account/password", "current", f.pw["mem"], "password", "long-enough", "confirm", "long-enough")
	if p.status != http.StatusSeeOther {
		t.Fatalf("change: %d %s", p.status, p.body)
	}
	f.want(b.get("/projects/WEB"), 200, "Website")
}

func TestLoginRateLimit(t *testing.T) {
	f := newFixture(t)
	b := f.browser()
	for range loginFailsPerUser {
		b.post("/login", "user", "admin", "password", "wrong")
	}
	f.want(b.post("/login", "user", "admin", "password", "admin-password"), 429, "Too many failed attempts")
	// Other users are unaffected.
	if p := b.post("/login", "user", "view", "password", f.pw["view"]); p.status != http.StatusSeeOther {
		t.Errorf("other user: %d", p.status)
	}
}

func TestCSRF(t *testing.T) {
	f := newFixture(t)
	b := f.login("admin")
	f.want(b.post("/projects", "csrf", "", "id", "API", "name", "API"), 403, "expired")
	f.want(b.post("/projects", "csrf", "forged", "id", "API", "name", "API"), 403)

	v := url.Values{"csrf": {b.csrf}, "id": {"API"}, "name": {"API"}}
	req, _ := http.NewRequest("POST", f.srv.URL+"/projects", strings.NewReader(v.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example")
	f.want(b.do(req), 403)

	if _, err := f.svc.Project("admin", "API"); err == nil {
		t.Error("a forged request created a project")
	}
}

func TestProjects(t *testing.T) {
	f := newFixture(t)
	admin := f.login("admin")

	f.want(admin.get("/projects/new"), 200, "New project")
	f.want(admin.post("/projects", "id", "api", "name", "API", "state", "active"), 422, "not a valid project ID")
	p := admin.post("/projects", "id", "API", "name", "<script>alert(1)</script>", "state", "active",
		"description", "Hello <script>alert(2)</script> **world**",
		"urls_code", "https://example.com/a\r\n\r\nhttps://example.com/b", "urls_web", "")
	if p.status != http.StatusSeeOther || p.location != "/projects/API" {
		t.Fatalf("create: %d %s %s", p.status, p.location, p.body)
	}
	p = admin.get("/projects/API")
	f.want(p, 200, "&lt;script&gt;alert(1)&lt;/script&gt;", "<strong>world</strong>", "https://example.com/b")
	if strings.Contains(p.body, "<script>alert") {
		t.Error("script rendered unescaped")
	}
	if got, _ := f.svc.Project("admin", "API"); len(got.URLs[model.URLCode]) != 2 {
		t.Errorf("urls = %v", got.URLs)
	}

	// Edit, and the second of two edits from the same version conflicts.
	f.want(admin.get("/projects/API/edit"), 200, `name="version" value="1"`)
	p = admin.post("/projects/API", "version", "1", "name", "API v2", "state", "active")
	if p.status != http.StatusSeeOther {
		t.Fatalf("edit: %d %s", p.status, p.body)
	}
	f.want(admin.post("/projects/API", "version", "1", "name", "Mine", "state", "dormant"),
		409, "Someone else changed this project", "API v2", `value="Mine"`, `name="version" value="2"`)

	mem := f.login("mem")
	f.want(mem.get("/projects/new"), 403)
	f.want(mem.post("/projects", "id", "XYZ", "name", "x", "state", "active"), 403)
	if strings.Contains(mem.get("/").body, "New project") {
		t.Error("member sees New project")
	}
}

func TestTasksAndComments(t *testing.T) {
	f := newFixture(t)
	mem := f.login("mem")

	f.want(mem.get("/projects/WEB/tasks/new"), 200, "New task")
	f.want(mem.post("/projects/WEB/tasks", "title", "", "owner", "mem", "state", "new", "priority", "3"), 422, "required")
	f.want(mem.post("/projects/WEB/tasks", "title", "x", "owner", "mem", "state", "complete", "priority", "3"), 422)
	p := mem.post("/projects/WEB/tasks", "title", "Fix the logo", "description", "It is *wrong*",
		"owner", "mem", "state", "new", "substate", "", "priority", "2")
	if p.status != http.StatusSeeOther || p.location != "/tasks/WEB-1" {
		t.Fatalf("create task: %d %s %s", p.status, p.location, p.body)
	}
	f.want(mem.get("/tasks/WEB-1"), 200, "Fix the logo", "<em>wrong</em>")
	f.want(mem.get("/projects/WEB?state=new&sort=priority"), 200, "WEB-1")
	if p := mem.get("/projects/WEB?state=paused"); strings.Contains(p.body, "Fix the logo") {
		t.Error("filter did not filter")
	}

	// Edit, then a conflicting edit from the old version.
	p = mem.post("/tasks/WEB-1", "version", "1", "title", "Fix the logo", "description", "", "owner", "mem",
		"state", "complete", "substate", "done", "priority", "2")
	if p.status != http.StatusSeeOther {
		t.Fatalf("edit task: %d %s", p.status, p.body)
	}
	f.want(mem.post("/tasks/WEB-1", "version", "1", "title", "Other title", "owner", "mem",
		"state", "paused", "priority", "5"), 409, "Someone else changed this task", `value="Other title"`)

	// Comments: add, edit own, others cannot edit, delete.
	p = mem.post("/tasks/WEB-1/comments", "text", "First **comment**")
	if p.status != http.StatusSeeOther || !strings.HasSuffix(p.location, "#comment-1") {
		t.Fatalf("comment: %d %s", p.status, p.location)
	}
	f.want(mem.post("/tasks/WEB-1/comments", "text", ""), 422, "required")
	f.want(mem.get("/tasks/WEB-1"), 200, "<strong>comment</strong>", "Delete comment")

	admin := f.login("admin")
	if strings.Contains(admin.get("/tasks/WEB-1").body, "Delete comment") {
		t.Error("admin sees edit controls on someone else's comment")
	}
	f.want(admin.post("/tasks/WEB-1/comments/1", "version", "1", "text", "hijack"), 403)

	p = mem.post("/tasks/WEB-1/comments/1", "version", "1", "text", "Edited")
	if p.status != http.StatusSeeOther {
		t.Fatalf("edit comment: %d %s", p.status, p.body)
	}
	f.want(mem.post("/tasks/WEB-1/comments/1", "version", "1", "text", "Stale"), 409, "Someone changed this comment")
	p = mem.post("/tasks/WEB-1/comments/1/delete", "version", "2")
	if p.status != http.StatusSeeOther {
		t.Fatalf("delete comment: %d %s", p.status, p.body)
	}
	f.want(mem.get("/tasks/WEB-1"), 200, "No comments yet")

	view := f.login("view")
	p = view.get("/tasks/WEB-1")
	f.want(p, 200, "Fix the logo")
	if strings.Contains(p.body, "Edit task") || strings.Contains(p.body, "Add a comment") {
		t.Error("viewer sees edit controls")
	}
	f.want(view.post("/tasks/WEB-1/comments", "text", "hi"), 403)
	f.want(view.get("/projects/WEB/tasks/new"), 403)
}

func TestUsersAndTokens(t *testing.T) {
	f := newFixture(t)
	admin := f.login("admin")

	f.want(admin.post("/users", "id", "Bad", "name", "x", "email", "x@example.com", "role", "member"), 422, "not a valid user ID")
	p := admin.post("/users", "id", "newbie", "name", "New Bie", "email", "new@example.com", "role", "viewer")
	f.want(p, 201, "one-time password")
	pw := regexp.MustCompile(`<code>([^<]+)</code>`).FindStringSubmatch(p.body)
	if pw == nil {
		t.Fatal("no password shown")
	}
	if _, must, err := f.svc.Login("newbie", pw[1]); err != nil || !must {
		t.Errorf("shown password: must=%v err=%v", must, err)
	}

	f.want(admin.get("/users/newbie"), 200, `value="New Bie"`)
	p = admin.post("/users/newbie", "version", "1", "name", "New Bie", "email", "new@example.com", "role", "member")
	if p.status != http.StatusSeeOther {
		t.Fatalf("edit user: %d %s", p.status, p.body)
	}
	if u, _ := f.svc.User("admin", "newbie"); u.Active || u.Role != model.RoleMember {
		t.Errorf("unchecked Active box should deactivate: %+v", u)
	}
	f.want(admin.post("/users/admin", "version", "1", "name", "Admin", "email", "admin@example.com",
		"role", "member", "active", "on"), 422, "last active admin")
	f.want(admin.post("/users/mem/password"), 200, "was reset")

	// A deactivated user's session ends at their next request.
	view := f.login("view")
	u, _ := f.svc.User("admin", "view")
	active := false
	f.svc.UpdateUser("admin", "view", u.Version, service.UserPatch{Active: &active})
	if p := view.get("/"); p.status != http.StatusSeeOther || p.location != "/login" {
		t.Errorf("deactivated session: %d %s", p.status, p.location)
	}

	// Tokens.
	p = admin.post("/account/tokens", "name", "laptop")
	f.want(p, 201, "shown only once", auth.TokenPrefix)
	secret := regexp.MustCompile(`<code>(` + auth.TokenPrefix + `[^<]+)</code>`).FindStringSubmatch(p.body)
	if secret == nil {
		t.Fatal("no token shown")
	}
	if u, err := f.svc.TokenLogin(secret[1]); err != nil || u.ID != "admin" {
		t.Errorf("token login: %v", err)
	}
	p = admin.get("/account")
	if strings.Contains(p.body, secret[1]) {
		t.Error("token shown again")
	}
	id := regexp.MustCompile(`/account/tokens/(tok_[a-z0-9]+)/delete`).FindStringSubmatch(p.body)
	if id == nil {
		t.Fatal("no revoke form")
	}
	if p := admin.post("/account/tokens/" + id[1] + "/delete"); p.status != http.StatusSeeOther {
		t.Fatalf("revoke: %d", p.status)
	}
	if _, err := f.svc.TokenLogin(secret[1]); err == nil {
		t.Error("revoked token still works")
	}
}

func TestDevUser(t *testing.T) {
	f := newFixture(t)
	w, err := New(f.svc, Options{DevUser: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(w)
	defer srv.Close()
	f.srv = srv
	f.want(f.browser().get("/projects/WEB"), 200, "Website")
}

func TestNotFound(t *testing.T) {
	f := newFixture(t)
	b := f.login("view")
	f.want(b.get("/projects/NOPE"), 404, "no such page")
	f.want(b.get("/tasks/WEB-99"), 404)
	f.want(b.get("/nothing/here"), 404)
}
