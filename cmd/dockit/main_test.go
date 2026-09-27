package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/store"
)

type result struct {
	code           int
	stdout, stderr string
}

func dockit(t *testing.T, stdin string, environ map[string]string, args ...string) result {
	t.Helper()
	var out, errOut bytes.Buffer
	e := &env{
		stdin:  strings.NewReader(stdin),
		stdout: &out,
		stderr: &errOut,
		getenv: func(k string) string { return environ[k] },
	}
	code := run(e, args)
	return result{code, out.String(), errOut.String()}
}

var adminFlags = []string{"-admin", "pdutton", "-name", "Peter Dutton", "-email", "peter@example.com"}

func initDataset(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	r := dockit(t, "", nil, append(append([]string{"init"}, adminFlags...), dir)...)
	if r.code != 0 {
		t.Fatalf("init failed: %+v", r)
	}
	return dir
}

func TestInit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	r := dockit(t, "", nil, append(append([]string{"init"}, adminFlags...), dir)...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	m := regexp.MustCompile(`One-time password: (\S+)`).FindStringSubmatch(r.stdout)
	if m == nil {
		t.Fatalf("no password printed:\n%s", r.stdout)
	}

	a, err := store.ReadAuth(dir, "pdutton")
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := auth.VerifyPassword(m[1], a.Password); !ok || err != nil {
		t.Errorf("printed password does not match stored hash (%v)", err)
	}
	if !a.MustChangePassword {
		t.Error("one-time password not marked for change")
	}
	u, err := store.ReadUser(dir, "pdutton")
	if err != nil || u.Role != "admin" || !u.Active || u.Name != "Peter Dutton" {
		t.Errorf("admin user = %+v, %v", u, err)
	}
}

func TestInitUsesDOCKIT_DATA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	r := dockit(t, "", map[string]string{"DOCKIT_DATA": dir}, append([]string{"init"}, adminFlags...)...)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if _, err := store.ReadMeta(dir); err != nil {
		t.Error(err)
	}
}

func TestInitErrors(t *testing.T) {
	existing := initDataset(t)
	for _, tc := range []struct {
		name string
		args []string
		code int
		msg  string
	}{
		{"no dir", append([]string{"init"}, adminFlags...), 2, "DOCKIT_DATA is not set"},
		{"not empty", append(append([]string{"init"}, adminFlags...), existing), 1, "not empty"},
		{"bad id", []string{"init", "-admin", "Peter", "-name", "P", "-email", "p@example.com", t.TempDir()}, 1, "-admin:"},
		{"no email", []string{"init", "-admin", "peter", "-name", "P", t.TempDir()}, 1, "-email: required"},
		{"no name", []string{"init", "-admin", "peter", "-email", "p@example.com", t.TempDir()}, 1, "-name: required"},
	} {
		r := dockit(t, "", nil, tc.args...)
		if r.code != tc.code || !strings.Contains(r.stderr, tc.msg) {
			t.Errorf("%s: exit %d, stderr %q; want exit %d containing %q", tc.name, r.code, r.stderr, tc.code, tc.msg)
		}
	}
}

func TestUnlock(t *testing.T) {
	dir := initDataset(t)

	r := dockit(t, "", nil, "unlock", dir)
	if r.code != 0 || !strings.Contains(r.stdout, "not locked") {
		t.Errorf("unlock of unlocked dataset: %+v", r)
	}

	if _, err := store.Open(dir); err != nil {
		t.Fatal(err)
	}

	r = dockit(t, "n\n", nil, "unlock", dir)
	if r.code != 0 || !strings.Contains(r.stdout, "Lock left in place") {
		t.Errorf("declined unlock: %+v", r)
	}
	if !strings.Contains(r.stdout, "pid:") {
		t.Errorf("lock contents not shown:\n%s", r.stdout)
	}
	if _, err := os.Stat(filepath.Join(dir, store.LockFile)); err != nil {
		t.Fatal("lock removed despite 'n'")
	}

	r = dockit(t, "y\n", nil, "unlock", dir)
	if r.code != 0 || !strings.Contains(r.stdout, "Lock removed") {
		t.Errorf("confirmed unlock: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, store.LockFile)); !os.IsNotExist(err) {
		t.Error("lock still present")
	}
}

func TestUnlockYes(t *testing.T) {
	dir := initDataset(t)
	if _, err := store.Open(dir); err != nil {
		t.Fatal(err)
	}
	r := dockit(t, "", nil, "unlock", "-yes", dir)
	if r.code != 0 || !strings.Contains(r.stdout, "Lock removed") {
		t.Errorf("unlock -yes: %+v", r)
	}
}

func TestUnlockNotADataset(t *testing.T) {
	r := dockit(t, "y\n", nil, "unlock", t.TempDir())
	if r.code != 1 || !strings.Contains(r.stderr, "not a DockIt dataset") {
		t.Errorf("unlock on non-dataset: %+v", r)
	}
}

func TestVersion(t *testing.T) {
	r := dockit(t, "", nil, "version")
	if r.code != 0 || !strings.Contains(r.stdout, "dockit ") || !strings.Contains(r.stdout, "dataset format 1") {
		t.Errorf("version: %+v", r)
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"help"}, {"bogus"}} {
		r := dockit(t, "", nil, args...)
		if r.code != 2 || !strings.Contains(r.stderr, "Commands:") {
			t.Errorf("%v: %+v", args, r)
		}
	}
}
