package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"go.yaml.in/yaml/v3"

	"github.com/pdutton/DockIt/internal/model"
)

// LockFile is the name of the lock file in the dataset root.
const LockFile = "dockit.lock"

// LockInfo is the content of the lock file.  BootID and PIDNamespace are
// empty on platforms without them, and in locks written by older builds.
type LockInfo struct {
	Host         string    `yaml:"host"`
	PID          int       `yaml:"pid"`
	Started      time.Time `yaml:"started"`
	Instance     string    `yaml:"instance"`
	BootID       string    `yaml:"boot_id,omitempty"`
	PIDNamespace string    `yaml:"pid_namespace,omitempty"`
}

// LockedError is returned when a dataset is already locked.  Info is nil if
// the lock file could not be read or parsed; Raw holds whatever was read.
type LockedError struct {
	Path string
	Info *LockInfo
	Raw  []byte
}

func (e *LockedError) Error() string {
	return fmt.Sprintf("dataset is locked by another instance (%s)", e.Path)
}

// ErrLockLost is returned when the lock file no longer holds this instance's
// ID, typically because an operator ran `dockit unlock` on a live dataset.
var ErrLockLost = errors.New("dataset lock lost: lock file missing or held by another instance")

// Lock is a held dataset lock.  It relies only on create-exclusive, not on
// any OS locking API.
type Lock struct {
	path string
	info LockInfo
}

// AcquireLock creates the lock file in root.  It fails with *LockedError if
// the file already exists.
func AcquireLock(root string) (*Lock, error) {
	sys := thisSystem()
	info := LockInfo{
		Host:         sys.host,
		PID:          os.Getpid(),
		Started:      model.Now(),
		Instance:     randomHex(16),
		BootID:       sys.bootID,
		PIDNamespace: sys.pidNamespace,
	}
	data, err := yaml.Marshal(&info)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(root, LockFile)
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		le := &LockedError{Path: path}
		le.Info, le.Raw, _ = ReadLock(root)
		return nil, le
	}
	if err != nil {
		return nil, err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	return &Lock{path: path, info: info}, nil
}

// Info returns the lock's content.
func (l *Lock) Info() LockInfo { return l.info }

// Verify confirms the lock file still holds this instance's ID.
func (l *Lock) Verify() error {
	info, _, err := ReadLock(filepath.Dir(l.path))
	if err != nil || info.Instance != l.info.Instance {
		return ErrLockLost
	}
	return nil
}

// Release removes the lock file, but only if it is still ours.
func (l *Lock) Release() error {
	if err := l.Verify(); err != nil {
		return err
	}
	return os.Remove(l.path)
}

// ReadLock reads the lock file in root.  It returns the raw content even when
// it does not parse, so an operator can be shown what is there.
func ReadLock(root string) (*LockInfo, []byte, error) {
	raw, err := os.ReadFile(filepath.Join(root, LockFile))
	if err != nil {
		return nil, nil, err
	}
	var info LockInfo
	if err := yaml.Unmarshal(raw, &info); err != nil {
		return nil, raw, err
	}
	return &info, raw, nil
}

// Unlock removes a stale lock file unconditionally.  The caller is
// responsible for confirming with the operator that no instance is running.
func Unlock(root string) error {
	return os.Remove(filepath.Join(root, LockFile))
}

// RemoveStaleLock removes the lock in root if the instance that took it can
// no longer be running (see system.stale), and returns what the lock held.
// It returns nil if there is no lock, or if DockIt cannot tell that it is
// stale, in which case taking the lock fails as usual.
//
// Two instances starting at once could both find the same stale lock, and
// the slower one could then remove the lock the faster one has just taken.
// The per-write lock check catches that: the faster instance's writes fail
// with ErrLockLost, and it never removes the other's lock.
func RemoveStaleLock(root string) (*LockInfo, error) {
	return thisSystem().removeStaleLock(root)
}

func (sys system) removeStaleLock(root string) (*LockInfo, error) {
	info, _, err := ReadLock(root)
	if err != nil || !sys.stale(info) {
		return nil, nil
	}
	if err := Unlock(root); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	return info, nil
}

// system describes the running system, for telling whether a lock is stale.
type system struct {
	host         string
	bootID       string
	pidNamespace string
	running      func(pid int) bool
}

func thisSystem() system {
	host, _ := os.Hostname()
	return system{host: host, bootID: bootID(), pidNamespace: pidNamespace(), running: processRunning}
}

// stale reports whether the instance that took a lock can no longer be
// running: it took the lock on this host either before the current boot, or
// in this boot and PID namespace from a process that has since gone.
// Whatever cannot be told for sure is not stale: a lock from another host,
// or without a boot ID (from Windows or an older build), or from another PID
// namespace, such as another container or WSL distro.  A PID reused by an
// unrelated process makes a lock look live, which errs on the safe side.
func (sys system) stale(l *LockInfo) bool {
	switch {
	case l == nil || sys.bootID == "" || l.BootID == "" || l.Host == "" || l.Host != sys.host:
		return false
	case l.BootID != sys.bootID:
		return true
	default:
		return sys.pidNamespace != "" && l.PIDNamespace == sys.pidNamespace && l.PID > 0 && !sys.running(l.PID)
	}
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand does not fail on supported platforms
	}
	return hex.EncodeToString(b)
}
