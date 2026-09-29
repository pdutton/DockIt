// Package upgrade migrates a dataset to the current format, in place.  It
// makes no backup: the operator copies the dataset directory first.  Upgrades
// are never implicit: `dockit serve` refuses an older dataset and tells the
// operator to run `dockit upgrade`, so a dataset is never silently made
// unreadable to an older install.
package upgrade

import (
	"errors"
	"fmt"

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
var Migrations = map[int]Migration{
	1: addTaskType,
}

// ErrCurrent is returned when the dataset is already in the target format.
var ErrCurrent = errors.New("dataset is already in the current format")

// Result describes a completed upgrade.
type Result struct {
	From, To int
}

// Run upgrades the dataset at root to format target using migrations.
// It takes the dataset lock, then applies each step in turn, recording the new
// format after each one.  If a step fails the dataset is left in the last
// completed format.
func Run(root string, target int, migrations map[int]Migration) (*Result, error) {
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

	for n := from; n < target; n++ {
		if m := migrations[n]; m != nil {
			if err := m(root); err != nil {
				return nil, fmt.Errorf("upgrading from format %d to %d: %w (restore the dataset from your copy)", n, n+1, err)
			}
		}
		meta.Format = n + 1
		if err := s.WriteMeta(meta); err != nil {
			return nil, err
		}
	}
	return &Result{From: from, To: target}, nil
}
