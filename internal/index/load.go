package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

// Problem is something wrong with a dataset.  Errors make a dataset unfit to
// serve; warnings do not.
type Problem struct {
	Path    string // relative to the dataset root, with forward slashes
	Field   string // empty if the problem is with the file as a whole
	Message string
	Warning bool
}

func (p Problem) String() string {
	kind := "error"
	if p.Warning {
		kind = "warning"
	}
	loc := p.Path
	if p.Field != "" {
		loc += ": " + p.Field
	}
	return fmt.Sprintf("%s: %s: %s", kind, loc, p.Message)
}

// Report lists the problems found while loading a dataset, in the order they
// were found.
type Report struct {
	Problems []Problem
}

// Errors returns the number of errors.
func (r *Report) Errors() int {
	n := 0
	for _, p := range r.Problems {
		if !p.Warning {
			n++
		}
	}
	return n
}

// Warnings returns the number of warnings.
func (r *Report) Warnings() int {
	return len(r.Problems) - r.Errors()
}

// OK reports whether the dataset has no errors.
func (r *Report) OK() bool { return r.Errors() == 0 }

// loader walks a dataset, filling an index and a report.
type loader struct {
	root   string
	x      *Index
	report *Report
}

func (l *loader) rel(path string) string {
	r, err := filepath.Rel(l.root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(r)
}

func (l *loader) errorf(path, field, format string, args ...any) {
	l.report.Problems = append(l.report.Problems,
		Problem{Path: l.rel(path), Field: field, Message: fmt.Sprintf(format, args...)})
}

func (l *loader) warnf(path, field, format string, args ...any) {
	l.report.Problems = append(l.report.Problems,
		Problem{Path: l.rel(path), Field: field, Message: fmt.Sprintf(format, args...), Warning: true})
}

func (l *loader) fieldErrors(path string, errs []*model.FieldError) {
	for _, e := range errs {
		l.report.Problems = append(l.report.Problems,
			Problem{Path: l.rel(path), Field: e.Field, Message: e.Message, Warning: e.Warning})
	}
}

// readErr reports a file that could not be read or parsed.
func (l *loader) readErr(path string, err error) {
	var pe *store.ParseError
	if errors.As(err, &pe) {
		l.errorf(path, "", "cannot parse: %v", pe.Err)
		return
	}
	l.errorf(path, "", "cannot read: %v", err)
}

// Load reads the whole dataset at root into a new index.  It does not stop
// at the first problem: everything that can be read is loaded and every
// problem found is reported.  The index is nil only if the dataset metadata
// itself is missing, unreadable, or in a format this build cannot read (see
// store.CheckFormat).  Load never changes the dataset, so an older minor
// format is loaded but left as it is.
//
// Load takes no lock, so it is safe to run on a dataset in use.
func Load(root string) (*Index, *Report) {
	l := &loader{root: root, report: &Report{}}
	metaPath := filepath.Join(root, store.MetaFile)

	meta, err := store.ReadMeta(root)
	if errors.Is(err, store.ErrNoDataset) {
		l.errorf(metaPath, "", "not found; %s is not a DockIt dataset", root)
		return nil, l.report
	}
	if err != nil {
		l.readErr(metaPath, err)
		return nil, l.report
	}
	if err := store.CheckFormat(meta.Version()); err != nil {
		l.errorf(metaPath, "format", "%v", err)
		return nil, l.report
	}
	l.fieldErrors(metaPath, meta.Validate())

	l.x = New(meta)
	l.checkRoot()
	l.loadUsers()
	l.loadAuth()
	l.loadProjects()
	l.checkReferences()
	return l.x, l.report
}

// readDir lists dir, reporting a problem if it cannot.  A missing directory
// is an error when required.
func (l *loader) readDir(dir string, required bool) []fs.DirEntry {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if required {
			l.errorf(dir, "", "directory missing")
		}
		return nil
	}
	if err != nil {
		l.errorf(dir, "", "cannot read directory: %v", err)
	}
	return entries
}

// unexpected reports a stray entry.  DockIt ignores them, so they are only
// warnings, but they usually mean a hand edit went wrong.
func (l *loader) unexpected(path string, e fs.DirEntry) {
	switch {
	case !e.IsDir() && store.IsTmpName(e.Name()):
		l.warnf(path, "", "leftover temporary file from an interrupted write; removed when DockIt next starts")
	case e.IsDir():
		l.warnf(path, "", "unexpected directory")
	default:
		l.warnf(path, "", "unexpected file")
	}
}

func (l *loader) checkRoot() {
	known := map[string]bool{
		store.MetaFile:    false,
		store.LockFile:    false,
		store.ProjectsDir: true,
		store.UsersDir:    true,
		store.AuthDir:     true,
	}
	for _, e := range l.readDir(l.root, true) {
		isDir, ok := known[e.Name()]
		if !ok || isDir != e.IsDir() {
			l.unexpected(filepath.Join(l.root, e.Name()), e)
		}
	}
}

// yamlFiles returns the ID stems of the *.yaml files in dir, reporting
// anything else as unexpected.
func (l *loader) yamlFiles(dir string, required bool) []string {
	var stems []string
	for _, e := range l.readDir(dir, required) {
		path := filepath.Join(dir, e.Name())
		stem, ok := strings.CutSuffix(e.Name(), store.Ext)
		if e.IsDir() || !ok || strings.HasPrefix(e.Name(), ".") {
			l.unexpected(path, e)
			continue
		}
		stems = append(stems, stem)
	}
	return stems
}

func (l *loader) loadUsers() {
	dir := filepath.Join(l.root, store.UsersDir)
	for _, uid := range l.yamlFiles(dir, true) {
		path := filepath.Join(dir, uid+store.Ext)
		if !model.ValidUserID(uid) {
			l.errorf(path, "", "file name is not a valid user ID")
			continue
		}
		u, err := store.ReadPath[model.User](path)
		if err != nil {
			l.readErr(path, err)
			continue
		}
		l.fieldErrors(path, u.Validate())
		if u.ID != uid {
			l.errorf(path, "id", "%q does not match the file name", u.ID)
			continue
		}
		l.x.PutUser(u)
	}
}

func (l *loader) loadAuth() {
	dir := filepath.Join(l.root, store.AuthDir)
	for _, uid := range l.yamlFiles(dir, true) {
		path := filepath.Join(dir, uid+store.Ext)
		if !model.ValidUserID(uid) {
			l.errorf(path, "", "file name is not a valid user ID")
			continue
		}
		a, err := store.ReadPath[model.Auth](path)
		if err != nil {
			l.readErr(path, err)
			continue
		}
		l.fieldErrors(path, a.Validate())
		if a.User != uid {
			l.errorf(path, "user", "%q does not match the file name", a.User)
			continue
		}
		if l.x.users[uid] == nil {
			l.warnf(path, "", "secrets for user %q, who has no profile in %s/", uid, store.UsersDir)
		}
		l.x.PutAuth(a)
	}
}

func (l *loader) loadProjects() {
	dir := filepath.Join(l.root, store.ProjectsDir)
	for _, e := range l.readDir(dir, true) {
		pdir := filepath.Join(dir, e.Name())
		if !e.IsDir() {
			l.unexpected(pdir, e)
			continue
		}
		pid := e.Name()
		if !model.ValidProjectID(pid) {
			l.errorf(pdir, "", "directory name is not a valid project ID")
			continue
		}
		l.loadProject(pdir, pid)
	}
}

func (l *loader) loadProject(pdir, pid string) {
	for _, e := range l.readDir(pdir, true) {
		if e.Name() == pid+store.Ext && !e.IsDir() || e.Name() == store.TasksDir && e.IsDir() {
			continue
		}
		l.unexpected(filepath.Join(pdir, e.Name()), e)
	}

	path := filepath.Join(pdir, pid+store.Ext)
	p, err := store.ReadPath[model.Project](path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		l.errorf(pdir, "", "project file %s missing", pid+store.Ext)
	case err != nil:
		l.readErr(path, err)
	default:
		l.fieldErrors(path, p.Validate())
		if p.ID != pid {
			l.errorf(path, "id", "%q does not match the directory name", p.ID)
		} else {
			l.x.PutProject(p)
		}
	}

	tdir := filepath.Join(pdir, store.TasksDir)
	for _, tid := range l.yamlFiles(tdir, false) {
		path := filepath.Join(tdir, tid+store.Ext)
		tpid, _, err := model.ParseTaskID(tid)
		if err != nil {
			l.errorf(path, "", "file name is not a valid task ID")
			continue
		}
		if tpid != pid {
			l.errorf(path, "", "task of project %s is in the directory of project %s", tpid, pid)
			continue
		}
		t, err := store.ReadPath[model.Task](path)
		if err != nil {
			l.readErr(path, err)
			continue
		}
		l.fieldErrors(path, t.Validate())
		if t.ID != tid {
			l.errorf(path, "id", "%q does not match the file name", t.ID)
			continue
		}
		l.x.PutTask(t)
	}
}

// checkReferences checks what spans files: every user a task refers to must
// exist.  (Users are never deleted, so this holds for any consistent copy.)
func (l *loader) checkReferences() {
	tids := make([]string, 0, len(l.x.tasks))
	for tid := range l.x.tasks {
		tids = append(tids, tid)
	}
	slices.Sort(tids)

	for _, tid := range tids {
		t := l.x.tasks[tid]
		path := store.TaskPath(l.root, tid)
		l.checkUserRef(path, "creator", t.Creator)
		l.checkUserRef(path, "owner", t.Owner)
		for i, c := range t.Comments {
			l.checkUserRef(path, fmt.Sprintf("comments[%d].commenter", i), c.Commenter)
		}
	}

	activeAdmin := false
	for _, u := range l.x.Users() {
		activeAdmin = activeAdmin || (u.Active && u.Role == model.RoleAdmin)
		if l.x.auth[u.ID] == nil {
			l.warnf(store.UserPath(l.root, u.ID), "", "no secrets in %s/%s%s, so this user cannot log in",
				store.AuthDir, u.ID, store.Ext)
		}
	}
	if !activeAdmin {
		l.warnf(filepath.Join(l.root, store.UsersDir), "", "no active admin, so no one can manage users or projects")
	}
}

func (l *loader) checkUserRef(path, field, uid string) {
	// A malformed ID has already been reported by Validate.
	if model.ValidUserID(uid) && l.x.users[uid] == nil {
		l.errorf(path, field, "user %q does not exist", uid)
	}
}
