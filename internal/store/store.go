// Package store reads and writes the DockIt dataset: a tree of YAML files
// written atomically, guarded by a lock file.
package store

import (
	"bytes"
	"errors"
	"fmt"
	"io"
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
	TasksDir    = "tasks"
	Ext         = ".yaml"
)

// ErrNoDataset is returned when a directory has no dockit.yaml.
var ErrNoDataset = errors.New("not a DockIt dataset: " + MetaFile + " not found")

// FormatError is returned when a dataset's format is not one this build can
// read as it is.
type FormatError struct {
	Format model.Format
}

// Upgradeable reports whether this build can upgrade the dataset: its major
// format is older than the current one, but not older than FormatMin.
func (e *FormatError) Upgradeable() bool {
	return e.Format.Major >= model.FormatMin && e.Format.Major < model.FormatCurrent.Major
}

func (e *FormatError) Error() string {
	switch {
	case e.Upgradeable():
		return fmt.Sprintf("dataset format %s is older than %s; `dockit serve` or `dockit upgrade` will upgrade it", e.Format, model.FormatCurrent)
	case e.Format.Compare(model.FormatCurrent) > 0:
		return fmt.Sprintf("dataset format %s is newer than this build supports (%s); use a newer DockIt", e.Format, model.FormatCurrent)
	default:
		return fmt.Sprintf("dataset format %s is too old for this build to upgrade (oldest supported: %d)", e.Format, model.FormatMin)
	}
}

// CheckFormat returns nil if this build can read a dataset in format f as it
// is: the same major format as the current one, and the same or an older
// minor.  Otherwise it returns a *FormatError.
func CheckFormat(f model.Format) error {
	if f.Major != model.FormatCurrent.Major || f.Minor > model.FormatCurrent.Minor {
		return &FormatError{f}
	}
	return nil
}

// Store is an open, locked dataset.
type Store struct {
	root string
	lock *Lock
	meta model.Meta
}

// Open locks the dataset at root for writing.  The dataset must exist and be
// in a format this build can read (see CheckFormat); Open does not change its
// format.  Leftover temporary files are removed, which is safe because the
// lock is held.
func Open(root string) (*Store, error) {
	return open(root, CheckFormat)
}

// OpenUpgradeable is like Open, but it also accepts a dataset in an older
// major format that this build can upgrade.  The caller must upgrade it
// before reading any records.
func OpenUpgradeable(root string) (*Store, error) {
	return open(root, func(f model.Format) error {
		err := CheckFormat(f)
		if fe, ok := err.(*FormatError); ok && fe.Upgradeable() {
			return nil
		}
		return err
	})
}

// OpenAnyFormat locks the dataset at root whatever its format, for `dockit
// upgrade`, which checks the format itself.  Records must not be read or
// written through the returned store unless they are in a format this build
// understands.
func OpenAnyFormat(root string) (*Store, error) {
	return open(root, func(model.Format) error { return nil })
}

func open(root string, checkFormat func(model.Format) error) (*Store, error) {
	meta, err := ReadMeta(root)
	if err != nil {
		return nil, err
	}
	if err := checkFormat(meta.Version()); err != nil {
		return nil, err
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
		if !d.IsDir() && IsTmpName(d.Name()) {
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
	return filepath.Join(projectDir(root, pid), pid+Ext)
}

// TaskPath returns the path of task tid's file.  tid must be well-formed.
func TaskPath(root, tid string) string {
	pid, _, _ := model.ParseTaskID(tid)
	return filepath.Join(projectDir(root, pid), TasksDir, tid+Ext)
}

// UserPath returns the path of user uid's profile.
func UserPath(root, uid string) string {
	return filepath.Join(root, UsersDir, uid+Ext)
}

// AuthPath returns the path of user uid's secrets.
func AuthPath(root, uid string) string {
	return filepath.Join(root, AuthDir, uid+Ext)
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
	return WriteFileAtomic(path, data)
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
	return ReadPath[model.Project](ProjectPath(root, pid))
}

// ReadTask reads task tid.
func ReadTask(root, tid string) (*model.Task, error) {
	if err := checkTaskID(tid); err != nil {
		return nil, err
	}
	return ReadPath[model.Task](TaskPath(root, tid))
}

// ReadUser reads user uid's profile.
func ReadUser(root, uid string) (*model.User, error) {
	if err := checkUserID(uid); err != nil {
		return nil, err
	}
	return ReadPath[model.User](UserPath(root, uid))
}

// ReadAuth reads user uid's secrets.
func ReadAuth(root, uid string) (*model.Auth, error) {
	if err := checkUserID(uid); err != nil {
		return nil, err
	}
	return ReadPath[model.Auth](AuthPath(root, uid))
}

// ReadPath decodes the YAML file at path into a new T, strictly (see readFile).
func ReadPath[T any](path string) (*T, error) {
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
		if errors.Is(err, io.EOF) {
			err = errors.New("empty file")
		}
		return &ParseError{Path: path, Err: err}
	}
	return nil
}

// ParseError is returned when a file exists but is not a valid record.
type ParseError struct {
	Path string
	Err  error
}

func (e *ParseError) Error() string { return e.Path + ": " + e.Err.Error() }

func (e *ParseError) Unwrap() error { return e.Err }
