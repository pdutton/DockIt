package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// The dockit skill's script, skills/dockit/dockit-api, is a client of this
// API.  These tests run it against a test server, so a change here that
// breaks it fails here too.

const skillScript = "../../skills/dockit/dockit-api"

// skillFixture is a fixture for running the script, or skips the test where
// the script cannot run.
func skillFixture(t *testing.T) *fixture {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("dockit-api is a bash script")
	}
	for _, prog := range []string{"bash", "curl", "jq"} {
		if _, err := exec.LookPath(prog); err != nil {
			t.Skipf("dockit-api needs %s", prog)
		}
	}
	return newFixture(t, Options{})
}

type skillRun struct {
	stdout, stderr string
	code           int
}

// skill runs the script as user (empty for no token), with stdin as its
// input.  Running it directly checks that it is still executable.
func (f *fixture) skill(user, stdin string, args ...string) skillRun {
	f.t.Helper()
	cmd := exec.Command(skillScript, args...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "DOCKIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, "DOCKIT_URL="+f.srv.URL, "DOCKIT_TOKEN="+f.tokens[user])
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	r := skillRun{}
	var ee *exec.ExitError
	if err := cmd.Run(); errors.As(err, &ee) {
		r.code = ee.ExitCode()
	} else if err != nil {
		f.t.Fatal(err)
	}
	r.stdout, r.stderr = stdout.String(), stderr.String()
	return r
}

// skillOK runs the script and returns its output, failing the test if the
// script fails.
func (f *fixture) skillOK(user, stdin string, args ...string) string {
	f.t.Helper()
	r := f.skill(user, stdin, args...)
	if r.code != 0 {
		f.t.Fatalf("dockit-api %s: exit %d: %s", strings.Join(args, " "), r.code, r.stderr)
	}
	return r.stdout
}

// skillJSON runs the script and decodes its output as an object.
func (f *fixture) skillJSON(user, stdin string, args ...string) map[string]any {
	f.t.Helper()
	var m map[string]any
	if out := f.skillOK(user, stdin, args...); json.Unmarshal([]byte(out), &m) != nil {
		f.t.Fatalf("dockit-api %s: not a JSON object: %s", strings.Join(args, " "), out)
	}
	return m
}

// skillFails runs the script and checks that it exits with code and that its
// error output contains each of want.
func (f *fixture) skillFails(code int, want []string, user, stdin string, args ...string) {
	f.t.Helper()
	r := f.skill(user, stdin, args...)
	if r.code != code {
		f.t.Errorf("dockit-api %s: exit %d, want %d; stderr: %s", strings.Join(args, " "), r.code, code, r.stderr)
	}
	for _, w := range want {
		if !strings.Contains(r.stderr, w) {
			f.t.Errorf("dockit-api %s: stderr %q does not contain %q", strings.Join(args, " "), r.stderr, w)
		}
	}
}

func TestSkillTasks(t *testing.T) {
	f := skillFixture(t)
	desc := "Some *Markdown* with `code`, \"quotes\", a $dollar and a\nsecond line.\n"
	r := f.skillJSON("mem", desc, "create", "WEB", "title=First", "type=bugfix", "priority=2", "description=@-")
	if r["id"] != "WEB-1" || r["priority"] != 2.0 || r["type"] != "bugfix" || r["owner"] != "mem" || r["description"] != desc {
		t.Errorf("created %v", r)
	}
	f.skillJSON("mem", "", "create", "WEB", "title=Second")
	f.skillFails(1, []string{"403 forbidden"}, "view", "", "create", "WEB", "title=Nope")
	f.skillFails(1, []string{"422 invalid", "(field title)"}, "mem", "", "create", "WEB", "type=task")

	lines := "WEB-1\tnew\tP2\tbugfix\tmem\tFirst\nWEB-2\tnew\tP3\ttask\tmem\tSecond\n"
	if got := f.skillOK("mem", "", "tasks", "WEB"); got != lines {
		t.Errorf("tasks: got %q, want %q", got, lines)
	}

	r = f.skillJSON("mem", "", "transition", "WEB-1", "start")
	if r["state"] != "in_progress" || r["version"] != 2.0 {
		t.Errorf("started %v", r)
	}
	f.skillFails(1, []string{"409 wrong_state"}, "mem", "", "transition", "WEB-1", "start")

	// A stale --version is refused, with the current record.
	f.skillFails(1, []string{"412 conflict", `ETag "2"`, `"state": "in_progress"`},
		"mem", "", "update", "WEB-1", "--version", "1", "priority=1")
	r = f.skillJSON("mem", "", "update", "WEB-1", "--version", "2", "priority=1", "description=", "found_in=1.0")
	if r["priority"] != 1.0 || r["found_in"] != "1.0" || (r["description"] != nil && r["description"] != "") {
		t.Errorf("updated %v", r)
	}
	if _, ok := r["comments"]; ok {
		t.Errorf("update printed the comments: %v", r)
	}
	r = f.skillJSON("mem", "", "update", "WEB-1", "state=complete", "substate=done")
	if r["state"] != "complete" || r["substate"] != "done" {
		t.Errorf("completed %v", r)
	}
	f.skillFails(2, []string{"FIELD=VALUE"}, "mem", "", "update", "WEB-1", "priority")
	f.skillFails(2, []string{"priority must be a number"}, "mem", "", "update", "WEB-1", "priority=high")

	// By default, as in the web interface, complete and deferred tasks are
	// left out.
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"WEB"}, "WEB-2\tnew\tP3\ttask\tmem\tSecond\n"},
		{[]string{"WEB", "--state", "complete"}, "WEB-1\tcomplete/done\tP1\tbugfix\tmem\tFirst\n"},
		{[]string{"WEB", "--all", "--sort", "priority"}, "WEB-1\tcomplete/done\tP1\tbugfix\tmem\tFirst\nWEB-2\tnew\tP3\ttask\tmem\tSecond\n"},
		{[]string{"WEB", "--all", "--owner", "admin"}, ""},
	} {
		if got := f.skillOK("mem", "", append([]string{"tasks"}, c.args...)...); got != c.want {
			t.Errorf("tasks %v: got %q, want %q", c.args, got, c.want)
		}
	}

	// mine looks in every project.
	f.skillJSON("admin", "", "api", "POST", "/projects", `{"id":"OPS","name":"Operations"}`)
	f.skillJSON("admin", "", "create", "OPS", "title=Third", "owner=mem")
	f.skillJSON("admin", "", "create", "OPS", "title=Fourth")
	mine := "OPS-1\tnew\tP3\ttask\tmem\tThird\nWEB-2\tnew\tP3\ttask\tmem\tSecond\n"
	if got := f.skillOK("mem", "", "mine"); got != mine {
		t.Errorf("mine: got %q, want %q", got, mine)
	}
	var all []map[string]any
	if err := json.Unmarshal([]byte(f.skillOK("mem", "", "mine", "--all", "--json")), &all); err != nil || len(all) != 3 {
		t.Errorf("mine --all --json: %v, %v", all, err)
	}
	f.skillFails(2, []string{"mine lists your own tasks"}, "mem", "", "mine", "--owner", "admin")
}

func TestSkillComments(t *testing.T) {
	f := skillFixture(t)
	f.skillJSON("mem", "", "create", "WEB", "title=T")

	text := "Line one with `code` and \"quotes\".\n\n- a list item\n"
	c := f.skillJSON("mem", text, "comment", "WEB-1")
	if c["id"] != 1.0 || c["text"] != text || c["commenter"] != "mem" {
		t.Errorf("commented %v", c)
	}
	file := filepath.Join(t.TempDir(), "comment.md")
	if err := os.WriteFile(file, []byte("From a file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c = f.skillJSON("mem", "", "comment", "WEB-1", file); c["id"] != 2.0 || c["text"] != "From a file\n" {
		t.Errorf("commented from a file %v", c)
	}
	f.skillFails(2, []string{"the comment is empty"}, "mem", "", "comment", "WEB-1")

	if c = f.skillJSON("mem", "Edited\n", "edit-comment", "WEB-1", "1"); c["text"] != "Edited\n" || c["version"] != 2.0 {
		t.Errorf("edited %v", c)
	}
	f.skillFails(1, []string{"403 forbidden"}, "admin", "Not mine\n", "edit-comment", "WEB-1", "1")
	if out := f.skillOK("mem", "", "delete-comment", "WEB-1", "2"); out != "" {
		t.Errorf("delete-comment printed %q", out)
	}

	task := f.skillJSON("mem", "", "task", "WEB-1")
	comments, _ := task["comments"].([]any)
	links, ok := task["links"].([]any)
	if len(comments) != 1 || !ok || len(links) != 0 {
		t.Errorf("task %v", task)
	}
}

func TestSkillURLsAndLinks(t *testing.T) {
	f := skillFixture(t)
	f.skillJSON("mem", "", "create", "WEB", "title=One")
	f.skillJSON("mem", "", "create", "WEB", "title=Two")

	prs := func(r map[string]any) any {
		urls, _ := r["urls"].(map[string]any)
		return urls["pr"]
	}
	pr1, pr2 := "https://example.com/pull/1", "https://example.com/pull/2"
	f.skillJSON("mem", "", "add-url", "WEB-1", "pr", pr1)
	f.skillJSON("mem", "", "add-url", "WEB-1", "pr", pr2)
	r := f.skillJSON("mem", "", "add-url", "WEB-1", "pr", pr1) // already there: no write
	if got := prs(r); !reflect.DeepEqual(got, []any{pr1, pr2}) || r["version"] != 3.0 {
		t.Errorf("after adding: %v", r)
	}
	r = f.skillJSON("mem", "", "remove-url", "WEB-1", "pr", pr1)
	if got := prs(r); !reflect.DeepEqual(got, []any{pr2}) {
		t.Errorf("after removing one: %v", r)
	}
	f.skillFails(1, []string{"WEB-1 has no pr URL"}, "mem", "", "remove-url", "WEB-1", "pr", pr1)
	if r = f.skillJSON("mem", "", "remove-url", "WEB-1", "pr", pr2); prs(r) != nil {
		t.Errorf("after removing both: %v", r)
	}

	f.skillJSON("mem", "", "link", "WEB-2", "blocked_by", "WEB-1")
	f.skillFails(1, []string{"WEB-9"}, "mem", "", "link", "WEB-2", "blocked_by", "WEB-9")
	links, _ := f.skillJSON("mem", "", "task", "WEB-1")["links"].([]any)
	if len(links) != 1 {
		t.Fatalf("links %v", links)
	}
	if l, _ := links[0].(map[string]any); l["type"] != "blocks" || l["task"] != "WEB-2" {
		t.Errorf("link %v", l)
	}
	if out := f.skillOK("mem", "", "unlink", "WEB-2", "blocked_by", "WEB-1"); out != "" {
		t.Errorf("unlink printed %q", out)
	}
	if links, _ := f.skillJSON("mem", "", "task", "WEB-1")["links"].([]any); len(links) != 0 {
		t.Errorf("links after unlink %v", links)
	}
}

func TestSkillAPIAndSetup(t *testing.T) {
	f := skillFixture(t)

	// api sends If-Match itself, or the version it is given.
	if p := f.skillJSON("admin", "", "api", "PATCH", "/projects/WEB", `{"name":"Site"}`); p["name"] != "Site" {
		t.Errorf("patched %v", p)
	}
	f.skillFails(1, []string{"412 conflict"}, "admin", "", "api", "patch", "/api/v1/projects/WEB", "--version", "1", `{"name":"Old"}`)

	// A DELETE of something with no GET still works: revoke a token.
	var toks []map[string]any
	if err := json.Unmarshal([]byte(f.skillOK("view", "", "api", "GET", "/me/tokens")), &toks); err != nil || len(toks) != 1 {
		t.Fatalf("tokens %v, %v", toks, err)
	}
	f.skillOK("view", "", "api", "DELETE", "/me/tokens/"+toks[0]["id"].(string))
	f.skillFails(1, []string{"401 unauthorized"}, "view", "", "me")

	if me := f.skillJSON("mem", "", "me"); me["id"] != "mem" {
		t.Errorf("me %v", me)
	}
	f.skillFails(1, []string{"404 not_found"}, "mem", "", "task", "WEB-9")
	f.skillFails(2, []string{"DOCKIT_TOKEN is not set"}, "", "", "me")
	f.skillFails(2, []string{"usage:"}, "mem", "")
	f.skillFails(2, []string{"unknown command"}, "mem", "", "frob")
	if out := f.skillOK("mem", "", "help"); !strings.Contains(out, "usage:") {
		t.Errorf("help printed %q", out)
	}
}
