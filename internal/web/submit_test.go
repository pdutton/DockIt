package web

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/service"
)

var submissionRe = regexp.MustCompile(`name="submission" value="([^"]+)"`)

// submissionKey returns the submission key of the form on a page.
func (b *browser) submissionKey(path string) string {
	b.f.t.Helper()
	m := submissionRe.FindStringSubmatch(b.get(path).body)
	if m == nil {
		b.f.t.Fatalf("%s has no submission key", path)
	}
	return m[1]
}

func (f *fixture) taskCount() int {
	f.t.Helper()
	ts, err := f.svc.Tasks("admin", "WEB", service.TaskFilter{}, service.SortByID)
	if err != nil {
		f.t.Fatal(err)
	}
	return len(ts)
}

func TestRepeatedSubmission(t *testing.T) {
	f := newFixture(t)
	mem := f.login("mem")

	// The same form sent twice creates one task, and both answers lead to it.
	key := mem.submissionKey("/projects/WEB/tasks/new")
	create := func(key, title string) page {
		return mem.post("/projects/WEB/tasks", "submission", key, "title", title,
			"owner", "mem", "state", "new", "priority", "3")
	}
	for i := range 2 {
		if p := create(key, "Once"); p.status != http.StatusSeeOther || p.location != "/tasks/WEB-1" {
			t.Fatalf("send %d: %d %s", i+1, p.status, p.location)
		}
	}
	if n := f.taskCount(); n != 1 {
		t.Fatalf("%d tasks after a repeated submission, want 1", n)
	}

	// A form changed and sent again is a new submission, as is the same
	// content from a newly rendered form.
	if p := create(key, "Changed"); p.location != "/tasks/WEB-2" {
		t.Errorf("changed form: %d %s", p.status, p.location)
	}
	if p := create(mem.submissionKey("/projects/WEB/tasks/new"), "Once"); p.location != "/tasks/WEB-3" {
		t.Errorf("new form: %d %s", p.status, p.location)
	}

	// Saving an edit twice does not report a conflict with the first save.
	key = mem.submissionKey("/tasks/WEB-1")
	for i := range 2 {
		p := mem.post("/tasks/WEB-1", "submission", key, "version", "1", "title", "Once more",
			"type", "task", "owner", "mem", "state", "new", "priority", "3")
		if p.status != http.StatusSeeOther {
			t.Fatalf("edit send %d: %d %s", i+1, p.status, p.body)
		}
	}

	// A repeated token request creates one token and shows it both times.
	admin := f.login("admin")
	key = admin.submissionKey("/account")
	first := admin.post("/account/tokens", "submission", key, "name", "laptop")
	f.want(first, http.StatusCreated, auth.TokenPrefix)
	if again := admin.post("/account/tokens", "submission", key, "name", "laptop"); again.body != first.body {
		t.Error("repeated token request got a different page")
	}
	if tokens, _ := f.svc.Tokens("admin"); len(tokens) != 1 {
		t.Errorf("%d tokens after a repeated request, want 1", len(tokens))
	}
}

// redirectTo is a submission handler that answers with a redirect.
func redirectTo(path string, runs *int) func(http.ResponseWriter) {
	return func(rw http.ResponseWriter) {
		*runs++
		rw.Header().Set("Location", path)
		rw.WriteHeader(http.StatusSeeOther)
	}
}

func TestOnce(t *testing.T) {
	w := &Web{submissions: newSubmissions()}
	r := httptest.NewRequest("POST", "/projects/WEB/tasks", nil)

	// A repeat that arrives while the first is running waits for it.
	sub, first := w.submissions.claim("a")
	if !first {
		t.Fatal("first claim is not first")
	}
	if again, first := w.submissions.claim("a"); first || again != sub {
		t.Fatal("repeat claimed a new submission")
	}
	runs := 0
	waited := make(chan *httptest.ResponseRecorder)
	go func() {
		rw := httptest.NewRecorder()
		w.once(rw, r, "a", redirectTo("/elsewhere", &runs))
		waited <- rw
	}()
	select {
	case <-waited:
		t.Fatal("repeat did not wait for the first")
	case <-time.After(20 * time.Millisecond):
	}
	w.submissions.run("a", sub, redirectTo("/tasks/WEB-1", &runs))
	if rw := <-waited; rw.Code != http.StatusSeeOther || rw.Header().Get("Location") != "/tasks/WEB-1" {
		t.Errorf("repeat got %d %s", rw.Code, rw.Header().Get("Location"))
	}
	if runs != 1 {
		t.Errorf("ran %d times", runs)
	}

	// A first request that panics is forgotten, so a repeat runs.
	func() {
		defer func() { recover() }()
		w.once(httptest.NewRecorder(), r, "b", func(http.ResponseWriter) { panic("boom") })
	}()
	runs = 0
	w.once(httptest.NewRecorder(), r, "b", redirectTo("/", &runs))
	if runs != 1 {
		t.Errorf("after a panic, repeat ran %d times", runs)
	}

	// Finished submissions are forgotten after submissionKeep.
	now := time.Now()
	w.submissions.now = func() time.Time { return now }
	runs = 0
	w.once(httptest.NewRecorder(), r, "c", redirectTo("/", &runs))
	now = now.Add(submissionKeep + time.Second)
	w.once(httptest.NewRecorder(), r, "c", redirectTo("/", &runs))
	if runs != 2 {
		t.Errorf("expired submission ran %d times, want 2", runs)
	}
}
