package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pdutton/DockIt/internal/model"
)

var ts1 = time.Date(2026, 9, 26, 21, 54, 43, 0, time.UTC)
var ts2 = time.Date(2026, 9, 27, 8, 10, 0, 0, time.UTC)

func testAdmin() (*model.User, *model.Auth) {
	return &model.User{
		ID: "pdutton", Version: 1, Name: "Peter Dutton", Email: "peter@example.com",
		Role: model.RoleAdmin, Active: true, Created: ts1, Modified: ts1,
	}, &model.Auth{
		User: "pdutton", Password: "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$a2V5", MustChangePassword: true,
	}
}

// newDataset creates a dataset in a temp dir and opens it.
func newDataset(t *testing.T) *Store {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	u, a := testAdmin()
	if err := Init(root, u, a); err != nil {
		t.Fatal(err)
	}
	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInitLayout(t *testing.T) {
	root := filepath.Join(t.TempDir(), "data")
	u, a := testAdmin()
	if err := Init(root, u, a); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{ProjectsDir, UsersDir, AuthDir} {
		if fi, err := os.Stat(filepath.Join(root, p)); err != nil || !fi.IsDir() {
			t.Errorf("%s: missing directory (%v)", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, LockFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lock file left behind after init: %v", err)
	}

	m, err := ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Format != model.FormatCurrent || len(m.DatasetID) != 36 || m.DatasetID[14] != '4' || m.Created.IsZero() {
		t.Errorf("bad metadata: %+v", m)
	}

	gotU, err := ReadUser(root, "pdutton")
	if err != nil || !reflect.DeepEqual(gotU, u) {
		t.Errorf("user = %+v, %v; want %+v", gotU, err, u)
	}
	gotA, err := ReadAuth(root, "pdutton")
	if err != nil || !reflect.DeepEqual(gotA, a) {
		t.Errorf("auth = %+v, %v; want %+v", gotA, err, a)
	}
	// The profile must hold no secrets.
	if s := readString(t, UserPath(root, "pdutton")); strings.Contains(s, "argon2") {
		t.Errorf("user profile contains a password hash:\n%s", s)
	}
}

func TestInitRefusesNonEmpty(t *testing.T) {
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "something"), nil, 0o644)
	u, a := testAdmin()
	if err := Init(root, u, a); !errors.Is(err, ErrNotEmpty) {
		t.Errorf("Init on non-empty dir: err = %v, want ErrNotEmpty", err)
	}

	file := filepath.Join(t.TempDir(), "file")
	os.WriteFile(file, nil, 0o644)
	if err := Init(file, u, a); err == nil {
		t.Error("Init on a regular file succeeded")
	}
}

func TestInitEmptyExistingDir(t *testing.T) {
	u, a := testAdmin()
	if err := Init(t.TempDir(), u, a); err != nil {
		t.Errorf("Init on an empty existing dir: %v", err)
	}
}

func TestOpenNoDataset(t *testing.T) {
	if _, err := Open(t.TempDir()); !errors.Is(err, ErrNoDataset) {
		t.Errorf("err = %v, want ErrNoDataset", err)
	}
}

func TestOpenFormat(t *testing.T) {
	s := newDataset(t)
	root := s.Root()
	s.Close()

	for _, tc := range []struct {
		format      int
		upgradeable bool
	}{
		{model.FormatCurrent - 1, true},
		{model.FormatCurrent + 1, false},
		{0, false},
	} {
		os.WriteFile(filepath.Join(root, MetaFile),
			[]byte("format: "+strconv.Itoa(tc.format)+"\ndataset_id: x\ncreated: 2026-09-26T21:54:43Z\n"), 0o644)
		_, err := Open(root)
		var fe *FormatError
		if !errors.As(err, &fe) || fe.Format != tc.format || fe.Upgradeable() != tc.upgradeable {
			t.Errorf("format %d: err = %v", tc.format, err)
		}
		if _, err := os.Stat(filepath.Join(root, LockFile)); err == nil {
			t.Errorf("format %d: lock taken on a dataset that was refused", tc.format)
		}
	}
}

func TestOpenTwiceIsLocked(t *testing.T) {
	s := newDataset(t)
	_, err := Open(s.Root())
	var le *LockedError
	if !errors.As(err, &le) {
		t.Fatalf("second Open: err = %v, want *LockedError", err)
	}
	if le.Info == nil || le.Info.Instance != s.lock.Info().Instance || le.Info.PID != os.Getpid() {
		t.Errorf("LockedError.Info = %+v, want the first instance's lock", le.Info)
	}
}

func TestCloseReleasesLock(t *testing.T) {
	s := newDataset(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(s.Root())
	if err != nil {
		t.Fatalf("reopen after Close: %v", err)
	}
	s2.Close()
}

func TestWriteAfterUnlockFails(t *testing.T) {
	s := newDataset(t)
	if err := Unlock(s.Root()); err != nil {
		t.Fatal(err)
	}
	u, _ := testAdmin()
	if err := s.WriteUser(u); !errors.Is(err, ErrLockLost) {
		t.Errorf("write after unlock: err = %v, want ErrLockLost", err)
	}

	// Someone else takes the lock; we must still refuse, and must not
	// remove their lock on Close.
	other, err := AcquireLock(s.Root())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.WriteUser(u); !errors.Is(err, ErrLockLost) {
		t.Errorf("write under another lock: err = %v, want ErrLockLost", err)
	}
	if err := s.Close(); !errors.Is(err, ErrLockLost) {
		t.Errorf("Close under another lock: err = %v, want ErrLockLost", err)
	}
	if err := other.Verify(); err != nil {
		t.Errorf("other instance's lock was disturbed: %v", err)
	}
	other.Release()
}

func TestOpenRemovesTmpFiles(t *testing.T) {
	s := newDataset(t)
	root := s.Root()
	s.Close()
	stray := filepath.Join(root, UsersDir, ".pdutton.yaml.tmp")
	os.WriteFile(stray, []byte("partial"), 0o644)
	keep := filepath.Join(root, UsersDir, "notes.tmp")
	os.WriteFile(keep, nil, 0o644)

	s, err := Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(stray); !errors.Is(err, fs.ErrNotExist) {
		t.Error("leftover temp file not removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("removed a file that is not one of ours")
	}
}

func TestWriteFileAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.yaml")
	for _, content := range []string{"one\n", "two\n"} {
		if err := WriteFileAtomic(path, []byte(content)); err != nil {
			t.Fatal(err)
		}
		if got := readString(t, path); got != content {
			t.Errorf("content = %q, want %q", got, content)
		}
	}
	if _, err := os.Stat(tmpName(path)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("temp file left behind")
	}
}

func TestWriteFileAtomicFailureKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.yaml")
	WriteFileAtomic(path, []byte("old\n"))
	// A directory where the temp file should go makes the write fail.
	os.Mkdir(tmpName(path), 0o755)
	if err := WriteFileAtomic(path, []byte("new\n")); err == nil {
		t.Fatal("write succeeded")
	}
	if got := readString(t, path); got != "old\n" {
		t.Errorf("content = %q after failed write", got)
	}
}

func TestRejectsBadIDs(t *testing.T) {
	s := newDataset(t)
	for _, err := range []error{
		s.WriteProject(&model.Project{ID: "../EVIL"}),
		s.WriteProject(&model.Project{ID: "web"}),
		s.WriteTask(&model.Task{ID: "WEB-0"}),
		s.WriteTask(&model.Task{ID: "../../X-1"}),
		s.WriteUser(&model.User{ID: "../auth/pdutton"}),
		s.WriteAuth(&model.Auth{User: "Peter"}),
	} {
		if err == nil {
			t.Error("write with a bad ID succeeded")
		}
	}
	if _, err := ReadUser(s.Root(), "../dockit"); err == nil {
		t.Error("read with a bad ID succeeded")
	}
}

// The on-disk format is part of the contract: outside readers depend on it,
// and fixed field order keeps diffs clean.  These match DESIGN.md.

func TestProjectFormat(t *testing.T) {
	s := newDataset(t)
	p := &model.Project{
		ID: "WEB", Version: 4, Name: "Website Redesign", State: model.ProjectActive,
		Description: "Markdown text.\n",
		URLs: model.URLs{
			model.URLWeb:  {"https://example.com"},
			model.URLCode: {"https://github.com/example/site", "https://github.com/example/site-infra"},
		},
		Created: ts1, Modified: ts2,
	}
	if err := s.WriteProject(p); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root(), "projects", "WEB", "WEB.yaml")
	want := `id: WEB
version: 4
name: Website Redesign
state: active
description: |
  Markdown text.
urls:
  code:
    - https://github.com/example/site
    - https://github.com/example/site-infra
  web:
    - https://example.com
created: 2026-09-26T21:54:43Z
modified: 2026-09-27T08:10:00Z
`
	if got := readString(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	back, err := ReadProject(s.Root(), "WEB")
	if err != nil || !reflect.DeepEqual(back, p) {
		t.Errorf("round trip = %+v, %v", back, err)
	}
}

func TestTaskFormat(t *testing.T) {
	s := newDataset(t)
	task := &model.Task{
		ID: "WEB-12", Version: 7, Title: "Replace The Header Logo", Type: model.TypeBugfix, FoundIn: "1.0.3", ResolvedIn: "1.1.0", Description: "Markdown text.\n",
		Creator: "pdutton", Owner: "pdutton", State: model.TaskComplete, Substate: model.SubstateDone,
		Priority: 3, Created: ts1, Modified: ts2,
		Comments: []model.Comment{{
			ID: 1, Version: 1, Commenter: "pdutton", Created: ts1, Modified: ts1, Text: "Markdown text.\n",
		}},
	}
	if err := s.WriteTask(task); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Root(), "projects", "WEB", "tasks", "WEB-12.yaml")
	want := `id: WEB-12
version: 7
title: Replace The Header Logo
type: bugfix
description: |
  Markdown text.
creator: pdutton
owner: pdutton
state: complete
substate: done
priority: 3
found_in: 1.0.3
resolved_in: 1.1.0
created: 2026-09-26T21:54:43Z
modified: 2026-09-27T08:10:00Z
comments:
  - id: 1
    version: 1
    commenter: pdutton
    created: 2026-09-26T21:54:43Z
    modified: 2026-09-26T21:54:43Z
    text: |
      Markdown text.
`
	if got := readString(t, path); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	back, err := ReadTask(s.Root(), "WEB-12")
	if err != nil || !reflect.DeepEqual(back, task) {
		t.Errorf("round trip = %+v, %v", back, err)
	}
}

func TestUserFormat(t *testing.T) {
	s := newDataset(t)
	want := `id: pdutton
version: 1
name: Peter Dutton
email: peter@example.com
role: admin
active: true
created: 2026-09-26T21:54:43Z
modified: 2026-09-26T21:54:43Z
`
	if got := readString(t, UserPath(s.Root(), "pdutton")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestReadRejectsUnknownFields(t *testing.T) {
	s := newDataset(t)
	path := UserPath(s.Root(), "pdutton")
	b, _ := os.ReadFile(path)
	os.WriteFile(path, append(b, []byte("nickname: pete\n")...), 0o644)
	if _, err := ReadUser(s.Root(), "pdutton"); err == nil {
		t.Error("read a file with an unknown field")
	}
}
