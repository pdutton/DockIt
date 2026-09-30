// Package upgrade brings a dataset to the current format, in place.  It
// makes no backup: the operator copies the dataset directory first.
// `dockit serve` does this when it opens a dataset, and `dockit upgrade` does
// it offline.  Once a dataset is upgraded, older installs refuse it.
package upgrade

import (
	"errors"
	"fmt"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

// Migration rewrites a dataset from one major format to the next.  It works
// on the files directly, since records may not be readable by this build's
// types until it has run, and it must accept any minor of the major it
// starts from.  It must leave dockit.yaml alone; Migrate updates the format
// after each step.  Writes should go through store.WriteFileAtomic.
type Migration func(root string) error

// Migrations maps a major format n to the migration from n to n+1.  A new
// minor format needs no migration: Migrate only has to update the number.
var Migrations = map[int]Migration{
	1: addTaskType,
}

// ErrCurrent is returned when the dataset is already in the target format.
var ErrCurrent = errors.New("dataset is already in the current format")

// Result describes a completed upgrade.
type Result struct {
	From, To model.Format
}

// Run upgrades the dataset at root to format target using migrations.  It
// takes the dataset lock and then calls Migrate.
func Run(root string, target model.Format, migrations map[int]Migration) (*Result, error) {
	s, err := store.OpenAnyFormat(root)
	if err != nil {
		return nil, err
	}
	defer s.Close()

	from := s.Meta().Version()
	switch {
	case from == target:
		return nil, ErrCurrent
	case from.Compare(target) > 0:
		return nil, &store.FormatError{Format: from}
	case from.Major < model.FormatMin:
		return nil, fmt.Errorf("dataset format %s is too old for this build to upgrade (oldest supported: %d)", from, model.FormatMin)
	}
	if err := Migrate(s, target, migrations); err != nil {
		return nil, err
	}
	return &Result{From: from, To: target}, nil
}

// Migrate upgrades the dataset open as s to target, and does nothing if it
// is already there.  It never downgrades.  It applies the migration for each major format in
// turn, recording the new format after each one, so if a step fails the
// dataset is left in the last completed format.  From an older minor of the
// same major, it only records the new format.
func Migrate(s *store.Store, target model.Format, migrations map[int]Migration) error {
	from := s.Meta().Version()
	if from.Compare(target) > 0 {
		return &store.FormatError{Format: from}
	}
	for n := from.Major; n < target.Major; n++ {
		if _, ok := migrations[n]; !ok {
			return fmt.Errorf("no upgrade step from format %d to %d", n, n+1)
		}
	}

	for n := from.Major; n < target.Major; n++ {
		if m := migrations[n]; m != nil {
			if err := m(s.Root()); err != nil {
				return fmt.Errorf("upgrading from format %d to %d: %w (restore the dataset from your copy)", n, n+1, err)
			}
		}
		next := model.Format{Major: n + 1}
		if next.Major == target.Major {
			next = target
		}
		if err := writeFormat(s, next); err != nil {
			return err
		}
	}
	if s.Meta().Version() != target {
		return writeFormat(s, target)
	}
	return nil
}

func writeFormat(s *store.Store, f model.Format) error {
	meta := s.Meta()
	meta.SetVersion(f)
	return s.WriteMeta(meta)
}
