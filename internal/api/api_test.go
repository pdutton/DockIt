package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/service"
	"github.com/pdutton/DockIt/internal/store"
)

type fixture struct {
	t      *testing.T
	svc    *service.Service
	srv    *httptest.Server
	tokens map[string]string // user -> API token
}

// newFixture serves a dataset with users admin, mem (member) and view
// (viewer), each with an API token, and project WEB.
func newFixture(t *testing.T, opts Options) *fixture {
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
	f := &fixture{t: t, svc: svc, tokens: map[string]string{}}
	for _, u := range []service.NewUser{
		{ID: "mem", Name: "Mem", Email: "mem@example.com", Role: model.RoleMember},
		{ID: "view", Name: "View", Email: "view@example.com", Role: model.RoleViewer},
	} {
		if _, _, err := svc.CreateUser("admin", u); err != nil {
			t.Fatal(err)
		}
	}
	for _, uid := range []string{"admin", "mem", "view"} {
		_, tok, err := svc.CreateToken(uid, "test")
		if err != nil {
			t.Fatal(err)
		}
		f.tokens[uid] = tok
	}
	if _, err := svc.CreateProject("admin", service.NewProject{ID: "WEB", Name: "Website"}); err != nil {
		t.Fatal(err)
	}
	f.srv = httptest.NewServer(New(svc, opts))
	t.Cleanup(f.srv.Close)
	return f
}

type response struct {
	status int
	header http.Header
	body   []byte
}

// json decodes the body into a generic value.
func (r response) json() any {
	var v any
	json.Unmarshal(r.body, &v)
	return v
}

func (r response) obj() map[string]any {
	m, _ := r.json().(map[string]any)
	return m
}

func (r response) errCode() string {
	e, _ := r.obj()["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func (r response) errField() string {
	e, _ := r.obj()["error"].(map[string]any)
	s, _ := e["field"].(string)
	return s
}

// do sends a request as user (empty for none).  body may be a string of
// raw JSON or any value to encode.  hdr is name, value pairs.
func (f *fixture) do(method, path, user string, body any, hdr ...string) response {
	f.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		buf, _ := json.Marshal(b)
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, f.srv.URL+Prefix+path, rd)
	if err != nil {
		f.t.Fatal(err)
	}
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if user != "" {
		req.Header.Set("Authorization", "Bearer "+f.tokens[user])
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return response{resp.StatusCode, resp.Header, b}
}

func (f *fixture) want(r response, status int, code string) {
	f.t.Helper()
	if r.status != status || (code != "" && r.errCode() != code) {
		f.t.Errorf("got %d %s, want %d %s; body: %s", r.status, r.errCode(), status, code, r.body)
	}
}

func TestAuthentication(t *testing.T) {
	f := newFixture(t, Options{})

	r := f.do("GET", "/projects", "", nil)
	f.want(r, 401, "unauthorized")
	if !strings.HasPrefix(r.header.Get("WWW-Authenticate"), "Bearer") {
		t.Error("no WWW-Authenticate header")
	}
	f.want(f.do("GET", "/projects", "", nil, "Authorization", "Bearer dockit_nope"), 401, "unauthorized")
	f.want(f.do("GET", "/projects", "", nil, "Authorization", "Basic YWRtaW46eA=="), 401, "unauthorized")
	f.want(f.do("GET", "/projects", "view", nil), 200, "")

	// Deactivated users' tokens stop working.
	u, _ := f.svc.User("admin", "view")
	active := false
	if _, err := f.svc.UpdateUser("admin", "view", u.Version, service.UserPatch{Active: &active}); err != nil {
		t.Fatal(err)
	}
	f.want(f.do("GET", "/projects", "view", nil), 401, "unauthorized")
}

func TestRateLimit(t *testing.T) {
	f := newFixture(t, Options{})
	for range failMax {
		f.want(f.do("GET", "/me", "", nil, "Authorization", "Bearer wrong"), 401, "")
	}
	// Now even a good token is refused from this address.
	f.want(f.do("GET", "/me", "mem", nil), 429, "rate_limited")
}

func TestDevUser(t *testing.T) {
	f := newFixture(t, Options{DevUser: "mem"})
	r := f.do("GET", "/me", "", nil)
	f.want(r, 200, "")
	if r.obj()["id"] != "mem" {
		t.Errorf("me = %s", r.body)
	}
}

func TestProjects(t *testing.T) {
	f := newFixture(t, Options{})

	body := map[string]any{"id": "API", "name": "API", "state": "active",
		"urls": map[string]any{"code": []string{"https://example.com/api"}}}
	f.want(f.do("POST", "/projects", "mem", body), 403, "forbidden")
	r := f.do("POST", "/projects", "admin", body)
	f.want(r, 201, "")
	if r.header.Get("Location") != Prefix+"/projects/API" || r.header.Get("ETag") != `"1"` {
		t.Errorf("headers = %v", r.header)
	}
	f.want(f.do("POST", "/projects", "admin", body), 409, "exists")
	r = f.do("POST", "/projects", "admin", map[string]any{"id": "NEW", "name": ""})
	f.want(r, 422, "invalid")
	if r.errField() != "name" {
		t.Errorf("field = %q", r.errField())
	}
	f.want(f.do("POST", "/projects", "admin", `{"id":"NEW","name":"x","colour":"red"}`), 400, "bad_request")
	f.want(f.do("POST", "/projects", "admin", `{"id":"NEW",`), 400, "bad_request")
	f.want(f.do("POST", "/projects", "admin", `{"id":"NEW","name":"x"} {}`), 400, "bad_request")

	r = f.do("GET", "/projects", "view", nil)
	if list, _ := r.json().([]any); len(list) != 2 {
		t.Errorf("projects = %s", r.body)
	}
	r = f.do("GET", "/projects/API", "view", nil)
	f.want(r, 200, "")
	if r.header.Get("ETag") != `"1"` {
		t.Errorf("ETag = %q", r.header.Get("ETag"))
	}
	f.want(f.do("GET", "/projects/NOPE", "view", nil), 404, "not_found")
}

func TestPatchProject(t *testing.T) {
	f := newFixture(t, Options{})
	f.want(f.do("POST", "/projects", "admin", map[string]any{"id": "API", "name": "API",
		"description": "About", "urls": map[string]any{"code": []string{"https://example.com/code"}}}), 201, "")

	f.want(f.do("PATCH", "/projects/API", "admin", `{"name":"New"}`), 428, "precondition_required")
	f.want(f.do("PATCH", "/projects/API", "admin", `{"name":"New"}`, "If-Match", "7"), 400, "bad_request")
	f.want(f.do("PATCH", "/projects/API", "admin", `{"id":"XYZ"}`, "If-Match", `"1"`), 400, "bad_request")
	f.want(f.do("PATCH", "/projects/API", "admin", `{"name":null}`, "If-Match", `"1"`), 400, "bad_request")
	f.want(f.do("PATCH", "/projects/API", "admin", `{"name":"x"}`, "If-Match", `"1"`,
		"Content-Type", "text/plain"), 415, "unsupported_media_type")

	// Merge patch: add a URL type, remove another, clear the description.
	r := f.do("PATCH", "/projects/API", "admin",
		`{"name":"New","description":null,"urls":{"web":["https://example.com"],"code":null}}`, "If-Match", `"1"`)
	f.want(r, 200, "")
	p := r.obj()
	urls, _ := p["urls"].(map[string]any)
	if p["name"] != "New" || p["description"] != nil || urls["code"] != nil || urls["web"] == nil ||
		r.header.Get("ETag") != `"2"` {
		t.Errorf("patched = %s, ETag %s", r.body, r.header.Get("ETag"))
	}

	// The second edit based on version 1 fails, with the current record.
	r = f.do("PATCH", "/projects/API", "admin", `{"state":"dormant"}`, "If-Match", `"1"`)
	f.want(r, 412, "conflict")
	if cur, _ := r.obj()["current"].(map[string]any); cur["name"] != "New" || r.header.Get("ETag") != `"2"` {
		t.Errorf("412 body = %s", r.body)
	}
	r = f.do("PATCH", "/projects/API", "admin", `{"urls":{"web":null}}`, "If-Match", `"1"`)
	f.want(r, 412, "conflict")

	r = f.do("PATCH", "/projects/API", "admin", `{"urls":null}`, "If-Match", `"2"`)
	f.want(r, 200, "")
	if _, ok := r.obj()["urls"]; ok {
		t.Errorf("urls not removed: %s", r.body)
	}
	f.want(f.do("PATCH", "/projects/API", "admin", `{"urls":{"web":["javascript:x"]}}`, "If-Match", `"3"`), 422, "invalid")
	f.want(f.do("PATCH", "/projects/API", "mem", `{"name":"x"}`, "If-Match", `"3"`), 403, "forbidden")
}

func TestTasks(t *testing.T) {
	f := newFixture(t, Options{})
	f.want(f.do("POST", "/projects/WEB/tasks", "view", `{"title":"x"}`), 403, "forbidden")
	r := f.do("POST", "/projects/WEB/tasks", "mem", `{"title":"First","priority":2}`)
	f.want(r, 201, "")
	if r.header.Get("Location") != Prefix+"/tasks/WEB-1" || r.obj()["owner"] != "mem" {
		t.Errorf("created %s %v", r.body, r.header)
	}
	f.want(f.do("POST", "/projects/WEB/tasks", "mem", `{"title":"Second","owner":"admin","state":"paused"}`), 201, "")
	f.want(f.do("POST", "/projects/NOPE/tasks", "mem", `{"title":"x"}`), 404, "not_found")
	r = f.do("POST", "/projects/WEB/tasks", "mem", `{"title":"x","state":"complete"}`)
	f.want(r, 422, "invalid")
	if r.errField() != "substate" {
		t.Errorf("field = %q", r.errField())
	}

	ids := func(r response) string {
		var out []string
		list, _ := r.json().([]any)
		for _, v := range list {
			out = append(out, v.(map[string]any)["id"].(string))
		}
		return strings.Join(out, ",")
	}
	for q, want := range map[string]string{
		"":                      "WEB-1,WEB-2",
		"?owner=admin":          "WEB-2",
		"?state=paused":         "WEB-2",
		"?priority=2":           "WEB-1",
		"?sort=priority":        "WEB-1,WEB-2",
		"?owner=mem&priority=3": "",
	} {
		r := f.do("GET", "/projects/WEB/tasks"+q, "view", nil)
		if r.status != 200 || ids(r) != want {
			t.Errorf("%s: %d %s, want %s", q, r.status, ids(r), want)
		}
	}
	f.want(f.do("GET", "/projects/WEB/tasks?priority=9", "view", nil), 400, "bad_request")
	f.want(f.do("GET", "/projects/WEB/tasks?sort=title", "view", nil), 400, "bad_request")

	r = f.do("PATCH", "/tasks/WEB-1", "mem", `{"state":"complete","substate":"done"}`, "If-Match", `"1"`)
	f.want(r, 200, "")
	r = f.do("PATCH", "/tasks/WEB-1", "mem", `{"state":"new","substate":null,"description":"Now **bold**"}`, "If-Match", `"2"`)
	f.want(r, 200, "")
	if r.obj()["substate"] != nil || r.obj()["description"] != "Now **bold**" {
		t.Errorf("patched %s", r.body)
	}
	f.want(f.do("PATCH", "/tasks/WEB-1", "mem", `{"creator":"admin"}`, "If-Match", `"3"`), 400, "bad_request")
	f.want(f.do("PATCH", "/tasks/WEB-1", "mem", `{"priority":"high"}`, "If-Match", `"3"`), 400, "bad_request")
	f.want(f.do("PATCH", "/tasks/WEB-1", "mem", `["title"]`, "If-Match", `"3"`), 400, "bad_request")
	f.want(f.do("GET", "/tasks/WEB-9", "view", nil), 404, "not_found")

	r = f.do("GET", "/tasks/WEB-1", "view", nil)
	if r.header.Get("ETag") != `"3"` || r.obj()["title"] != "First" {
		t.Errorf("task %s ETag %s", r.body, r.header.Get("ETag"))
	}
}

func TestComments(t *testing.T) {
	f := newFixture(t, Options{})
	f.want(f.do("POST", "/projects/WEB/tasks", "mem", `{"title":"T"}`), 201, "")

	r := f.do("POST", "/tasks/WEB-1/comments", "mem", `{"text":"hello"}`)
	f.want(r, 201, "")
	if r.header.Get("Location") != Prefix+"/tasks/WEB-1/comments/1" || r.header.Get("ETag") != `"1"` {
		t.Errorf("headers %v", r.header)
	}
	f.want(f.do("POST", "/tasks/WEB-1/comments", "view", `{"text":"hi"}`), 403, "forbidden")
	f.want(f.do("POST", "/tasks/WEB-1/comments", "mem", `{"text":""}`), 422, "invalid")

	f.want(f.do("PATCH", "/tasks/WEB-1/comments/1", "admin", `{"text":"x"}`, "If-Match", `"1"`), 403, "forbidden")
	f.want(f.do("PATCH", "/tasks/WEB-1/comments/1", "mem", `{"text":"x"}`), 428, "precondition_required")
	r = f.do("PATCH", "/tasks/WEB-1/comments/1", "mem", `{"text":"edited"}`, "If-Match", `"1"`)
	f.want(r, 200, "")
	if r.obj()["text"] != "edited" || r.header.Get("ETag") != `"2"` {
		t.Errorf("edited %s", r.body)
	}
	r = f.do("DELETE", "/tasks/WEB-1/comments/1", "mem", nil, "If-Match", `"1"`)
	f.want(r, 412, "conflict")
	if cur, _ := r.obj()["current"].(map[string]any); cur["text"] != "edited" {
		t.Errorf("412 body %s", r.body)
	}
	f.want(f.do("DELETE", "/tasks/WEB-1/comments/1", "mem", nil, "If-Match", `"2"`), 204, "")
	f.want(f.do("DELETE", "/tasks/WEB-1/comments/1", "mem", nil, "If-Match", `"2"`), 404, "not_found")
	f.want(f.do("DELETE", "/tasks/WEB-1/comments/abc", "mem", nil, "If-Match", `"2"`), 404, "not_found")

	// The task itself still has version 1: comments never conflict with it.
	if r := f.do("GET", "/tasks/WEB-1", "view", nil); r.header.Get("ETag") != `"1"` {
		t.Errorf("task ETag = %s", r.header.Get("ETag"))
	}
}

func TestUsers(t *testing.T) {
	f := newFixture(t, Options{})
	body := `{"id":"newbie","name":"New Bie","email":"new@example.com","role":"member"}`
	f.want(f.do("POST", "/users", "mem", body), 403, "forbidden")
	r := f.do("POST", "/users", "admin", body)
	f.want(r, 201, "")
	pw, _ := r.obj()["password"].(string)
	if len(pw) < 16 || r.header.Get("Location") != Prefix+"/users/newbie" {
		t.Errorf("created %s", r.body)
	}
	if _, must, err := f.svc.Login("newbie", pw); err != nil || !must {
		t.Errorf("one-time password: must=%v err=%v", must, err)
	}
	f.want(f.do("POST", "/users", "admin", body), 409, "exists")

	r = f.do("GET", "/users/newbie", "view", nil)
	f.want(r, 200, "")
	if strings.Contains(string(r.body), "argon2") || strings.Contains(string(r.body), "password") {
		t.Errorf("user body leaks secrets: %s", r.body)
	}
	r = f.do("PATCH", "/users/newbie", "admin", `{"role":"viewer","active":false}`, "If-Match", `"1"`)
	f.want(r, 200, "")
	if r.obj()["active"] != false {
		t.Errorf("patched %s", r.body)
	}
	f.want(f.do("PATCH", "/users/admin", "admin", `{"active":false}`, "If-Match", `"1"`), 422, "invalid")

	r = f.do("POST", "/users/mem/password", "admin", nil)
	f.want(r, 200, "")
	pw, _ = r.obj()["password"].(string)
	if _, must, err := f.svc.Login("mem", pw); err != nil || !must {
		t.Errorf("reset password: must=%v err=%v", must, err)
	}
	f.want(f.do("POST", "/users/ghost/password", "admin", nil), 404, "not_found")

	if list, _ := f.do("GET", "/users", "view", nil).json().([]any); len(list) != 4 {
		t.Errorf("users = %d", len(list))
	}
}

func TestMeAndTokens(t *testing.T) {
	f := newFixture(t, Options{})
	r := f.do("GET", "/me", "view", nil)
	if r.obj()["id"] != "view" || r.obj()["role"] != "viewer" {
		t.Errorf("me = %s", r.body)
	}

	r = f.do("POST", "/me/tokens", "view", `{"name":"ci"}`)
	f.want(r, 201, "")
	secret, _ := r.obj()["token"].(string)
	id, _ := r.obj()["id"].(string)
	if !strings.HasPrefix(secret, auth.TokenPrefix) || id == "" {
		t.Fatalf("token %s", r.body)
	}

	r = f.do("GET", "/me/tokens", "view", nil)
	list, _ := r.json().([]any)
	if len(list) != 2 || strings.Contains(string(r.body), "sha256") || strings.Contains(string(r.body), secret) {
		t.Errorf("tokens = %s", r.body)
	}

	f.tokens["ci"] = secret
	if r := f.do("GET", "/me", "ci", nil); r.obj()["id"] != "view" {
		t.Errorf("new token: %s", r.body)
	}
	f.want(f.do("DELETE", "/me/tokens/"+id, "mem", nil), 404, "not_found")
	f.want(f.do("DELETE", "/me/tokens/"+id, "view", nil), 204, "")
	f.want(f.do("GET", "/me", "ci", nil), 401, "unauthorized")
}

func TestEnums(t *testing.T) {
	f := newFixture(t, Options{})
	r := f.do("GET", "/enums", "view", nil)
	f.want(r, 200, "")
	subs, _ := r.obj()["substates"].(map[string]any)
	states, _ := r.obj()["task_states"].([]any)
	if len(subs["complete"].([]any)) != 2 || len(states) != 5 ||
		states[1].(map[string]any)["display"] != "In Progress" {
		t.Errorf("enums = %s", r.body)
	}
}

func TestMisc(t *testing.T) {
	f := newFixture(t, Options{})
	r := f.do("GET", "/nothing", "view", nil)
	f.want(r, 404, "not_found")
	if !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") ||
		r.header.Get("X-Content-Type-Options") != "nosniff" {
		t.Errorf("headers %v", r.header)
	}
	big := `{"text":"` + strings.Repeat("x", maxBody) + `"}`
	f.want(f.do("POST", "/projects/WEB/tasks", "mem", `{"title":"T"}`), 201, "")
	f.want(f.do("POST", "/tasks/WEB-1/comments", "mem", big), 413, "too_large")
}
