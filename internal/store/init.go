package store

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pdutton/DockIt/internal/model"
)

// ErrNotEmpty is returned by Init when the target directory is not empty.
var ErrNotEmpty = errors.New("directory is not empty")

// Init creates a new dataset in root, which must be missing or empty, with
// admin as its first user.  dockit.yaml is written last, so an interrupted
// Init never leaves something that looks like a usable dataset.
func Init(root string, admin *model.User, adminAuth *model.Auth) (err error) {
	if err := checkEmpty(root); err != nil {
		return err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	lock, err := AcquireLock(root)
	if err != nil {
		return err
	}
	defer func() {
		if rerr := lock.Release(); err == nil {
			err = rerr
		}
	}()

	s := &Store{root: root, lock: lock}
	for _, d := range []string{ProjectsDir, UsersDir, AuthDir} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return err
		}
	}
	if err := s.WriteUser(admin); err != nil {
		return err
	}
	if err := s.WriteAuth(adminAuth); err != nil {
		return err
	}
	return s.WriteMeta(model.Meta{
		Format:    model.FormatCurrent,
		DatasetID: newUUID(),
		Created:   model.Now(),
	})
}

// checkEmpty succeeds if dir does not exist or is an empty directory.
func checkEmpty(dir string) error {
	f, err := os.Open(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if _, err := f.Readdirnames(1); err == io.EOF {
		return nil
	} else if err != nil {
		return err
	}
	return fmt.Errorf("%s: %w", dir, ErrNotEmpty)
}

// newUUID returns a random (version 4) UUID.
func newUUID() string {
	h := randomHex(16)
	b := []byte(h)
	b[12] = '4'                     // version 4
	b[16] = "89ab"[hexVal(b[16])&3] // RFC 4122 variant
	return fmt.Sprintf("%s-%s-%s-%s-%s", b[0:8], b[8:12], b[12:16], b[16:20], b[20:32])
}

func hexVal(c byte) byte {
	if c <= '9' {
		return c - '0'
	}
	return c - 'a' + 10
}
