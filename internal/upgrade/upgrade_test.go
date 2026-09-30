package upgrade

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

var now = time.Date(2026, 9, 27, 2, 15, 0, 0, time.UTC)

// newDataset creates a dataset in format 1.0, which these tests upgrade from.
func newDataset(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	u := &model.User{ID: "admin", Version: 1, Name: "Admin", Email: "a@example.com",
		Role: model.RoleAdmin, Active: true, Created: now, Modified: now}
	if err := store.Init(root, u, &model.Auth{User: "admin", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	setFormat(t, root, model.Format{Major: 1})
	return root
}

func setFormat(t *testing.T, root string, f model.Format) {
	t.Helper()
	s, err := store.OpenAnyFormat(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := s.Meta()
	m.SetVersion(f)
	if err := s.WriteMeta(m); err != nil {
		t.Fatal(err)
	}
}

func format(t *testing.T, root string) model.Format {
	t.Helper()
	m, err := store.ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	return m.Version()
}

// siblings lists the directories beside root other than root itself.
func siblings(t *testing.T, root string) []string {
	entries, _ := os.ReadDir(filepath.Dir(root))
	var out []string
	for _, e := range entries {
		if e.Name() != filepath.Base(root) {
			out = append(out, e.Name())
		}
	}
	return out
}

func TestUpgrade(t *testing.T) {
	root := newDataset(t)
	setFormat(t, root, model.Format{Major: 1, Minor: 3}) // a migration takes any minor
	var ran []int
	migrations := map[int]Migration{
		1: func(r string) error {
			ran = append(ran, 1)
			// A migration may not rely on dockit.yaml's format yet.
			if f := format(t, r); f != (model.Format{Major: 1, Minor: 3}) {
				t.Errorf("step 1 saw format %s", f)
			}
			return store.WriteFileAtomic(filepath.Join(r, "users", "admin.yaml"), []byte("upgraded\n"))
		},
		2: nil, // no rewrite needed: only the number moves
		3: func(r string) error {
			ran = append(ran, 3)
			// Each completed step is recorded, as the major's first minor.
			if f := format(t, r); f != (model.Format{Major: 3}) {
				t.Errorf("step 3 saw format %s", f)
			}
			return nil
		},
	}
	target := model.Format{Major: 4, Minor: 2}
	res, err := Run(root, target, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if res.From != (model.Format{Major: 1, Minor: 3}) || res.To != target || !slices.Equal(ran, []int{1, 3}) || format(t, root) != target {
		t.Errorf("result %+v, ran %v, format %s", res, ran, format(t, root))
	}
	// The operator makes the backup; upgrade writes nothing beside the dataset.
	if s := siblings(t, root); len(s) != 0 {
		t.Errorf("wrote beside the dataset: %v", s)
	}
	if _, err := os.Stat(filepath.Join(root, store.LockFile)); !os.IsNotExist(err) {
		t.Error("lock not released")
	}
}

func TestUpgradeMinor(t *testing.T) {
	root := newDataset(t)
	setFormat(t, root, model.Format{Major: 5})
	// A new minor needs no migration at all.
	res, err := Run(root, model.Format{Major: 5, Minor: 2}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := (model.Format{Major: 5, Minor: 2}); res.To != want || format(t, root) != want {
		t.Errorf("result %+v, format %s", res, format(t, root))
	}
}

// A dataset this build can read is loaded first, and one with errors is
// refused, as `dockit serve` refuses it.
func TestUpgradeMinorChecksDataset(t *testing.T) {
	root := newDataset(t)
	setFormat(t, root, model.FormatCurrent)
	next := model.Format{Major: model.FormatCurrent.Major, Minor: model.FormatCurrent.Minor + 1}
	if err := os.WriteFile(filepath.Join(root, "users", "ghost.yaml"), []byte("id: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var le *LoadError
	if _, err := Run(root, next, nil); !errors.As(err, &le) || le.Report.OK() {
		t.Fatalf("err = %v, want LoadError", err)
	}
	if f := format(t, root); f != model.FormatCurrent {
		t.Errorf("format %s, want it unchanged", f)
	}
	if _, err := os.Stat(filepath.Join(root, store.LockFile)); !os.IsNotExist(err) {
		t.Error("lock not released")
	}

	// Once fixed, it is updated.
	if err := os.Remove(filepath.Join(root, "users", "ghost.yaml")); err != nil {
		t.Fatal(err)
	}
	if res, err := Run(root, next, nil); err != nil || res.To != next || format(t, root) != next {
		t.Errorf("Run = %+v, %v; format %s", res, err, format(t, root))
	}
}

func TestUpgradeCurrent(t *testing.T) {
	root := newDataset(t)
	setFormat(t, root, model.FormatCurrent)
	if _, err := Run(root, model.FormatCurrent, Migrations); !errors.Is(err, ErrCurrent) {
		t.Errorf("err = %v, want ErrCurrent", err)
	}
}

func TestUpgradeRefuses(t *testing.T) {
	root := newDataset(t)

	// Newer than the target, by major or minor: never downgrade.
	if _, err := Run(root, model.Format{}, nil); err == nil {
		t.Error("downgrade allowed")
	}
	setFormat(t, root, model.Format{Major: 1, Minor: 3})
	if _, err := Run(root, model.Format{Major: 1, Minor: 1}, nil); err == nil {
		t.Error("minor downgrade allowed")
	}
	// A missing step is found before anything is touched.
	if _, err := Run(root, model.Format{Major: 3}, map[int]Migration{1: nil}); err == nil || !strings.Contains(err.Error(), "no upgrade step from format 2") {
		t.Errorf("err = %v", err)
	}
	if f := format(t, root); f != (model.Format{Major: 1, Minor: 3}) {
		t.Errorf("touched the dataset: format %s", f)
	}
	// A running instance holds the lock.
	s, err := store.OpenAnyFormat(root)
	if err != nil {
		t.Fatal(err)
	}
	var le *store.LockedError
	if _, err := Run(root, model.Format{Major: 2}, map[int]Migration{1: nil}); !errors.As(err, &le) {
		t.Errorf("err = %v, want LockedError", err)
	}
	s.Close()

	// Older than this build can upgrade.
	setFormat(t, root, model.Format{Minor: 4})
	if _, err := Run(root, model.FormatCurrent, Migrations); err == nil || !strings.Contains(err.Error(), "too old") {
		t.Errorf("err = %v", err)
	}
}

func TestMigrateNeverDowngrades(t *testing.T) {
	root := newDataset(t)
	s, err := store.OpenAnyFormat(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var fe *store.FormatError
	if err := Migrate(s, model.Format{}, nil); !errors.As(err, &fe) {
		t.Errorf("err = %v, want FormatError", err)
	}
	// Already there: nothing to do.
	if err := Migrate(s, model.Format{Major: 1}, nil); err != nil {
		t.Error(err)
	}
	if f := format(t, root); f != (model.Format{Major: 1}) {
		t.Errorf("format %s", f)
	}
}

func TestUpgradeFailure(t *testing.T) {
	root := newDataset(t)
	boom := errors.New("boom")
	_, err := Run(root, model.Format{Major: 3, Minor: 1}, map[int]Migration{1: nil, 2: func(string) error { return boom }})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "restore the dataset from your copy") {
		t.Fatalf("err = %v", err)
	}
	// The completed step is recorded; the failed one is not.
	if f := format(t, root); f != (model.Format{Major: 2}) {
		t.Errorf("format %s, want 2.0", f)
	}
}
