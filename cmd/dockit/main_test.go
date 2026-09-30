package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/pdutton/DockIt/internal/auth"
	"github.com/pdutton/DockIt/internal/model"
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
	if r.code != 0 || !strings.Contains(r.stdout, "dockit ") || !strings.Contains(r.stdout, fmt.Sprintf("dataset format %s", model.FormatCurrent)) {
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

func TestCheck(t *testing.T) {
	dir := initDataset(t)
	r := dockit(t, "", nil, "check", dir)
	if r.code != 0 || !strings.Contains(r.stdout, "0 errors, 0 warnings.") || !strings.Contains(r.stdout, "1 user.") {
		t.Errorf("clean dataset: %+v", r)
	}

	os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "users", "pdutton.yaml"), []byte("id: [\n"), 0o644)
	r = dockit(t, "", nil, "check", dir)
	if r.code != 1 || !strings.Contains(r.stdout, "error: users/pdutton.yaml: cannot parse") ||
		!strings.Contains(r.stdout, "warning: notes.txt: unexpected file") ||
		!strings.Contains(r.stdout, "1 error, ") || r.stderr != "" {
		t.Errorf("broken dataset: %+v", r)
	}

	r = dockit(t, "", nil, "check", "-q", dir)
	if r.code != 1 || strings.Contains(r.stdout, "warning") || strings.Contains(r.stdout, "errors") ||
		!strings.Contains(r.stdout, "cannot parse") {
		t.Errorf("check -q: %+v", r)
	}
}

func TestCheckLocked(t *testing.T) {
	dir := initDataset(t)
	s, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := dockit(t, "", nil, "check", dir)
	if r.code != 0 || !strings.Contains(r.stdout, "an instance may be running") {
		t.Errorf("check on locked dataset: %+v", r)
	}
}

func TestCheckNotADataset(t *testing.T) {
	r := dockit(t, "", nil, "check", t.TempDir())
	if r.code != 1 || !strings.Contains(r.stdout, "not a DockIt dataset") {
		t.Errorf("%+v", r)
	}
}

func TestServe(t *testing.T) {
	dir := initDataset(t)
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, slog.New(slog.DiscardHandler), serveConfig{dir: dir, listen: "127.0.0.1:0", devUser: "pdutton"}, ready)
	}()
	var addr string
	select {
	case addr = <-ready:
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	}

	resp, err := http.Get("http://" + addr + "/api/v1/me")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"pdutton"`) {
		t.Errorf("GET /me = %d %s", resp.StatusCode, body)
	}

	// While serving, the dataset is locked.
	if r := dockit(t, "", nil, "serve", "-listen", "127.0.0.1:0", dir); r.code != 1 || !strings.Contains(r.stderr, "locked") {
		t.Errorf("second serve: %+v", r)
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve returned %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, store.LockFile)); !os.IsNotExist(err) {
		t.Error("lock not released on shutdown")
	}
}

// setFormat rewrites the dataset's format, as an older or newer build would
// have left it.
func setFormat(t *testing.T, dir string, f model.Format) {
	t.Helper()
	s, err := store.OpenAnyFormat(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	meta := s.Meta()
	meta.SetVersion(f)
	if err := s.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}
}

func TestServeUpdatesFormat(t *testing.T) {
	if model.FormatCurrent.Minor == 0 {
		t.Skip("the current format has no older minor")
	}
	dir := initDataset(t)
	older := model.Format{Major: model.FormatCurrent.Major, Minor: model.FormatCurrent.Minor - 1}
	setFormat(t, dir, older)

	var logBuf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, slog.New(slog.NewTextHandler(&logBuf, nil)), serveConfig{dir: dir, listen: "127.0.0.1:0"}, ready)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve returned %v", err)
	}

	// Logged before "DockIt serving", so it sits beside the dataset path.
	log := logBuf.String()
	want := fmt.Sprintf("msg=\"dataset format updated\" dataset=%s from=%s to=%s", dir, older, model.FormatCurrent)
	if i := strings.Index(log, want); i < 0 || i > strings.Index(log, "DockIt serving") {
		t.Errorf("log does not start with %q:\n%s", want, log)
	}
	if m, _ := store.ReadMeta(dir); m.Version() != model.FormatCurrent {
		t.Errorf("format on disk = %s", m.Version())
	}
}

func TestCheckFormat(t *testing.T) {
	dir := initDataset(t)
	if model.FormatCurrent.Minor > 0 {
		// An older minor passes, and check leaves it alone.
		older := model.Format{Major: model.FormatCurrent.Major, Minor: model.FormatCurrent.Minor - 1}
		setFormat(t, dir, older)
		r := dockit(t, "", nil, "check", dir)
		if r.code != 0 || !strings.Contains(r.stdout, fmt.Sprintf("in format %s; `dockit serve` or `dockit upgrade` will update it to %s", older, model.FormatCurrent)) {
			t.Errorf("check of format %s: %+v", older, r)
		}
		if m, _ := store.ReadMeta(dir); m.Version() != older {
			t.Errorf("check changed the format to %s", m.Version())
		}
	}
	// An older major cannot be checked until it is upgraded.
	setFormat(t, dir, model.Format{Major: model.FormatCurrent.Major - 1})
	if r := dockit(t, "", nil, "check", dir); r.code != 1 || !strings.Contains(r.stdout, "`dockit serve` or `dockit upgrade` will upgrade it") {
		t.Errorf("check of an older major: %+v", r)
	}
}

func TestServeFlags(t *testing.T) {
	dir := initDataset(t)
	for _, tc := range []struct {
		args []string
		msg  string
	}{
		{[]string{"-dev-insecure-user", "pdutton", "-listen", ":0"}, "localhost"},
		{[]string{"-dev-insecure-user", "pdutton", "-listen", "0.0.0.0:0"}, "localhost"},
		{[]string{"-tls-cert", "c.pem"}, "together"},
		{[]string{"-dev-insecure-user", "ghost", "-listen", "localhost:0"}, "not an active user"},
	} {
		r := dockit(t, "", nil, append(append([]string{"serve"}, tc.args...), dir)...)
		if r.code != 1 || !strings.Contains(r.stderr, tc.msg) {
			t.Errorf("%v: %+v", tc.args, r)
		}
	}
	if r := dockit(t, "", nil, "serve", t.TempDir()); r.code != 1 || !strings.Contains(r.stderr, "not a DockIt dataset") {
		t.Errorf("serve on non-dataset: %+v", r)
	}
}

func TestUpgradeCommand(t *testing.T) {
	dir := initDataset(t)
	r := dockit(t, "", nil, "upgrade", dir)
	if r.code != 0 || !strings.Contains(r.stdout, fmt.Sprintf("already in format %s", model.FormatCurrent)) {
		t.Errorf("upgrade of current dataset: %+v", r)
	}
	if r := dockit(t, "", nil, "upgrade", t.TempDir()); r.code != 1 || !strings.Contains(r.stderr, "not a DockIt dataset") {
		t.Errorf("upgrade of non-dataset: %+v", r)
	}
}

func TestUpgradeCommandMinor(t *testing.T) {
	if model.FormatCurrent.Minor == 0 {
		t.Skip("no older minor of the current format")
	}
	older := model.Format{Major: model.FormatCurrent.Major, Minor: model.FormatCurrent.Minor - 1}
	dir := initDataset(t)
	setFormat(t, dir, older)

	// A dataset with errors is refused and keeps its format, as serve does.
	ghost := filepath.Join(dir, "users", "ghost.yaml")
	if err := os.WriteFile(ghost, []byte("id: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := dockit(t, "", nil, "upgrade", dir)
	if r.code != 1 || !strings.Contains(r.stdout, "ghost.yaml") || !strings.Contains(r.stdout, "was not upgraded") {
		t.Errorf("upgrade of a broken dataset: %+v", r)
	}
	if m, _ := store.ReadMeta(dir); m.Version() != older {
		t.Errorf("format on disk = %s, want %s", m.Version(), older)
	}

	// A clean one is updated.
	if err := os.Remove(ghost); err != nil {
		t.Fatal(err)
	}
	r = dockit(t, "", nil, "upgrade", dir)
	if r.code != 0 || !strings.Contains(r.stdout, fmt.Sprintf("from format %s to %s", older, model.FormatCurrent)) {
		t.Errorf("upgrade of a clean dataset: %+v", r)
	}
	if m, _ := store.ReadMeta(dir); m.Version() != model.FormatCurrent {
		t.Errorf("format on disk = %s", m.Version())
	}
}
