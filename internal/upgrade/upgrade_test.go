package upgrade

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdutton/DockIt/internal/model"
	"github.com/pdutton/DockIt/internal/store"
)

var now = time.Date(2026, 9, 27, 2, 15, 0, 0, time.UTC)

// newDataset creates a dataset in format 1, which these tests upgrade from.
func newDataset(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "data")
	u := &model.User{ID: "admin", Version: 1, Name: "Admin", Email: "a@example.com",
		Role: model.RoleAdmin, Active: true, Created: now, Modified: now}
	if err := store.Init(root, u, &model.Auth{User: "admin", Password: "x"}); err != nil {
		t.Fatal(err)
	}
	setFormat(t, root, 1)
	return root
}

func setFormat(t *testing.T, root string, n int) {
	t.Helper()
	s, err := store.OpenAnyFormat(root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	m := s.Meta()
	m.Format = n
	if err := s.WriteMeta(m); err != nil {
		t.Fatal(err)
	}
}

func format(t *testing.T, root string) int {
	t.Helper()
	m, err := store.ReadMeta(root)
	if err != nil {
		t.Fatal(err)
	}
	return m.Format
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
	var ran []int
	migrations := map[int]Migration{
		1: func(r string) error {
			ran = append(ran, 1)
			// A migration may not rely on dockit.yaml's format yet.
			if f := format(t, r); f != 1 {
				t.Errorf("step 1 saw format %d", f)
			}
			return store.WriteFileAtomic(filepath.Join(r, "users", "admin.yaml"), []byte("upgraded\n"))
		},
		2: nil, // an additive change: only the number moves
	}
	res, err := Run(root, 3, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if res.From != 1 || res.To != 3 || len(ran) != 1 || format(t, root) != 3 {
		t.Errorf("result %+v, ran %v, format %d", res, ran, format(t, root))
	}
	// The operator makes the backup; upgrade writes nothing beside the dataset.
	if s := siblings(t, root); len(s) != 0 {
		t.Errorf("wrote beside the dataset: %v", s)
	}
	if _, err := os.Stat(filepath.Join(root, store.LockFile)); !os.IsNotExist(err) {
		t.Error("lock not released")
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

	// Newer than the target: never downgrade.
	if _, err := Run(root, 0, nil); err == nil {
		t.Error("downgrade allowed")
	}
	// A missing step is found before anything is touched.
	if _, err := Run(root, 3, map[int]Migration{1: nil}); err == nil || !strings.Contains(err.Error(), "no upgrade step from format 2") {
		t.Errorf("err = %v", err)
	}
	if f := format(t, root); f != 1 {
		t.Errorf("touched the dataset: format %d", f)
	}
	// A running instance holds the lock.
	s, err := store.OpenAnyFormat(root)
	if err != nil {
		t.Fatal(err)
	}
	var le *store.LockedError
	if _, err := Run(root, 2, map[int]Migration{1: nil}); !errors.As(err, &le) {
		t.Errorf("err = %v, want LockedError", err)
	}
	s.Close()
}

func TestUpgradeFailure(t *testing.T) {
	root := newDataset(t)
	boom := errors.New("boom")
	_, err := Run(root, 3, map[int]Migration{1: nil, 2: func(string) error { return boom }})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "restore the dataset from your copy") {
		t.Fatalf("err = %v", err)
	}
	// The completed step is recorded; the failed one is not.
	if f := format(t, root); f != 2 {
		t.Errorf("format %d, want 2", f)
	}
}
