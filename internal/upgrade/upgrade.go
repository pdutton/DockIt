// Package upgrade migrates a dataset to the current format, in place, after
// writing a full backup beside it.  Upgrades are never implicit: `dockit
// serve` refuses an older dataset and tells the operator to run `dockit
// upgrade`, so a dataset is never silently made unreadable to an older
// install.
package upgrade

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

// Migration rewrites a dataset from one format to the next.  It works on the
// files directly, since records may not be readable by this build's types
// until it has run.  It must leave dockit.yaml alone; Run updates the format
// after each step.  Writes should go through store.WriteFileAtomic.
type Migration func(root string) error

// Migrations maps a format n to the migration from n to n+1.  An additive
// change (a new optional field or enumeration value) needs no migration at
// all: Run only has to bump the format number, so its entry is nil.
//
// Format 1 is the first format, so there are none yet.
var Migrations = map[int]Migration{}

// ErrCurrent is returned when the dataset is already in the target format.
var ErrCurrent = errors.New("dataset is already in the current format")

// Result describes a completed upgrade.
type Result struct {
	From, To int
	Backup   string // the backup directory
}

// Run upgrades the dataset at root to format target using migrations.
// It takes the dataset lock, copies the whole dataset to a new sibling
// directory, then applies each step in turn, recording the new format after
// each one.  If a step fails the dataset is left in the last completed
// format, and the backup holds the original.
func Run(root string, target int, migrations map[int]Migration, now time.Time) (*Result, error) {
	s, err := store.OpenAnyFormat(root)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	meta := s.Meta()
	from := meta.Format
	switch {
	case from == target:
		return nil, ErrCurrent
	case from > target:
		return nil, &store.FormatError{Format: from}
	case from < model.FormatMin:
		return nil, fmt.Errorf("dataset format %d is too old for this build to upgrade (oldest supported: %d)", from, model.FormatMin)
	}
	for n := from; n < target; n++ {
		if _, ok := migrations[n]; !ok {
			return nil, fmt.Errorf("no upgrade step from format %d to %d", n, n+1)
		}
	}

	backup, err := backupDir(root, from, now)
	if err != nil {
		return nil, err
	}
	if err := copyTree(root, backup); err != nil {
		return nil, fmt.Errorf("backing up to %s: %w", backup, err)
	}

	for n := from; n < target; n++ {
		if m := migrations[n]; m != nil {
			if err := m(root); err != nil {
				return nil, fmt.Errorf("upgrading from format %d to %d: %w (the original is in %s)", n, n+1, err, backup)
			}
		}
		meta.Format = n + 1
		if err := s.WriteMeta(meta); err != nil {
			return nil, err
		}
	}
	return &Result{From: from, To: target, Backup: backup}, nil
}

// backupDir creates and returns a new, empty directory beside root, named
// for the format and time, such as data.format-1.20260927T021500Z.
func backupDir(root string, format int, now time.Time) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s.format-%d.%s", filepath.Base(abs), format, now.UTC().Format("20060102T150405Z"))
	dir := filepath.Join(filepath.Dir(abs), name)
	// Mkdir, not MkdirAll: fail rather than mix into an existing directory.
	if err := os.Mkdir(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// copyTree copies the dataset at src into the empty directory dst, leaving
// out the lock file and leftover temporary files.  Every file is synced, so
// the backup is on disk before the dataset is touched.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil || rel == "." {
			return err
		}
		if rel == store.LockFile || store.IsTmpName(d.Name()) {
			return nil
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.Mkdir(target, 0o755)
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
