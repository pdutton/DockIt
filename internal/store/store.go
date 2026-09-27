// Package store reads and writes the DockIt dataset: a tree of YAML files
// written atomically, guarded by a lock file.
package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"

	"github.com/pdutton/DockIt/internal/model"
)

// Names of files and directories in the dataset root.
const (
	MetaFile    = "dockit.yaml"
	ProjectsDir = "projects"
	UsersDir    = "users"
	AuthDir     = "auth"
	tasksDir    = "tasks"
	ext         = ".yaml"
)

// ErrNoDataset is returned when a directory has no dockit.yaml.
var ErrNoDataset = errors.New("not a DockIt dataset: " + MetaFile + " not found")

// FormatError is returned when a dataset's format is not the one this build
// serves.
type FormatError struct {
	Format int
}

// Upgradeable reports whether `dockit upgrade` can bring the dataset to the
// current format.
func (e *FormatError) Upgradeable() bool {
	return e.Format >= model.FormatMin && e.Format < model.FormatCurrent
}

func (e *FormatError) Error() string {
	switch {
	case e.Upgradeable():
		return fmt.Sprintf("dataset format %d is older than %d; run `dockit upgrade`", e.Format, model.FormatCurrent)
	case e.Format > model.FormatCurrent:
		return fmt.Sprintf("dataset format %d is newer than this build supports (%d); use a newer DockIt", e.Format, model.FormatCurrent)
	default:
		return fmt.Sprintf("dataset format %d is not supported", e.Format)
	}
}

// Store is an open, locked dataset.
type Store struct {
	root string
	lock *Lock
	meta model.Meta
}

// Open locks the dataset at root for writing.  The dataset must exist and be
// in the current format.  Leftover temporary files are removed, which is safe
// because the lock is held.
func Open(root string) (*Store, error) {
	meta, err := ReadMeta(root)
	if err != nil {
		return nil, err
	}
	if meta.Format != model.FormatCurrent {
		return nil, &FormatError{meta.Format}
	}
	lock, err := AcquireLock(root)
	if err != nil {
		return nil, err
	}
	s := &Store{root: root, lock: lock, meta: meta}
	if err := s.removeTmpFiles(); err != nil {
		lock.Release()
		return nil, err
	}
	return s, nil
}

// Close releases the lock.
func (s *Store) Close() error {
	return s.lock.Release()
}

// Root returns the dataset root directory.
func (s *Store) Root() string { return s.root }

// Meta returns the dataset metadata read when the store was opened.
func (s *Store) Meta() model.Meta { return s.meta }

func (s *Store) removeTmpFiles() error {
	return filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && isTmpName(d.Name()) {
			return os.Remove(path)
		}
		return nil
	})
}

// Paths.  IDs become file names, so every path helper is only reached after
// the ID has been validated; see checkProjectID and friends.

func projectDir(root, pid string) string {
	return filepath.Join(root, ProjectsDir, pid)
}

// ProjectPath returns the path of project pid's file.
func ProjectPath(root, pid string) string {
	return filepath.Join(projectDir(root, pid), pid+ext)
}

// TaskPath returns the path of task tid's file.  tid must be well-formed.
func TaskPath(root, tid string) string {
	pid, _, _ := model.ParseTaskID(tid)
	return filepath.Join(projectDir(root, pid), tasksDir, tid+ext)
}

// UserPath returns the path of user uid's profile.
func UserPath(root, uid string) string {
	return filepath.Join(root, UsersDir, uid+ext)
}

// AuthPath returns the path of user uid's secrets.
func AuthPath(root, uid string) string {
	return filepath.Join(root, AuthDir, uid+ext)
}

func checkProjectID(pid string) error {
	if !model.ValidProjectID(pid) {
		return fmt.Errorf("invalid project ID %q", pid)
	}
	return nil
}

func checkTaskID(tid string) error {
	_, _, err := model.ParseTaskID(tid)
	return err
}

func checkUserID(uid string) error {
	if !model.ValidUserID(uid) {
		return fmt.Errorf("invalid user ID %q", uid)
	}
	return nil
}

// Writes.  Each confirms the lock is still ours first.

func (s *Store) write(path string, v any) error {
	if err := s.lock.Verify(); err != nil {
		return err
	}
	data, err := marshal(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// WriteMeta writes dockit.yaml.
func (s *Store) WriteMeta(m model.Meta) error {
	if err := s.write(filepath.Join(s.root, MetaFile), &m); err != nil {
		return err
	}
	s.meta = m
	return nil
}

// WriteProject writes a project file, creating its directory if needed.
func (s *Store) WriteProject(p *model.Project) error {
	if err := checkProjectID(p.ID); err != nil {
		return err
	}
	return s.write(ProjectPath(s.root, p.ID), p)
}

// WriteTask writes a task file.
func (s *Store) WriteTask(t *model.Task) error {
	if err := checkTaskID(t.ID); err != nil {
		return err
	}
	return s.write(TaskPath(s.root, t.ID), t)
}

// WriteUser writes a user profile.
func (s *Store) WriteUser(u *model.User) error {
	if err := checkUserID(u.ID); err != nil {
		return err
	}
	return s.write(UserPath(s.root, u.ID), u)
}

// WriteAuth writes a user's secrets.
func (s *Store) WriteAuth(a *model.Auth) error {
	if err := checkUserID(a.User); err != nil {
		return err
	}
	return s.write(AuthPath(s.root, a.User), a)
}

// Reads.  These need no lock, so outside tools such as `dockit check` can use
// them on a dataset that is in use.

// ReadMeta reads dockit.yaml in root.
func ReadMeta(root string) (model.Meta, error) {
	var m model.Meta
	err := readFile(filepath.Join(root, MetaFile), &m)
	if errors.Is(err, fs.ErrNotExist) {
		return m, ErrNoDataset
	}
	return m, err
}

// ReadProject reads project pid.
func ReadProject(root, pid string) (*model.Project, error) {
	if err := checkProjectID(pid); err != nil {
		return nil, err
	}
	return read[model.Project](ProjectPath(root, pid))
}

// ReadTask reads task tid.
func ReadTask(root, tid string) (*model.Task, error) {
	if err := checkTaskID(tid); err != nil {
		return nil, err
	}
	return read[model.Task](TaskPath(root, tid))
}

// ReadUser reads user uid's profile.
func ReadUser(root, uid string) (*model.User, error) {
	if err := checkUserID(uid); err != nil {
		return nil, err
	}
	return read[model.User](UserPath(root, uid))
}

// ReadAuth reads user uid's secrets.
func ReadAuth(root, uid string) (*model.Auth, error) {
	if err := checkUserID(uid); err != nil {
		return nil, err
	}
	return read[model.Auth](AuthPath(root, uid))
}

// read decodes the file at path into a new T.
func read[T any](path string) (*T, error) {
	var v T
	if err := readFile(path, &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// readFile decodes a YAML file strictly: an unknown field is an error, since
// a file this build does not fully understand must never be rewritten by it.
func readFile(path string, v any) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}
