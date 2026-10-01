package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestStale(t *testing.T) {
	here := system{host: "box", bootID: "boot-2", pidNamespace: "pid:[1]"}
	held := LockInfo{Host: "box", PID: 42, BootID: "boot-2", PIDNamespace: "pid:[1]"}
	tests := []struct {
		name  string
		edit  func(sys *system, l *LockInfo)
		alive bool
		want  bool
	}{
		{"earlier boot", func(_ *system, l *LockInfo) { l.BootID = "boot-1" }, true, true},
		{"process gone", nil, false, true},
		{"process running", nil, true, false},
		{"other host", func(_ *system, l *LockInfo) { l.Host, l.BootID = "other", "boot-1" }, false, false},
		{"host name unknown", func(sys *system, l *LockInfo) { sys.host, l.Host, l.BootID = "", "", "boot-1" }, false, false},
		{"lock without boot ID", func(_ *system, l *LockInfo) { l.BootID, l.PIDNamespace = "", "" }, false, false},
		{"system without boot ID", func(sys *system, _ *LockInfo) { sys.bootID = "" }, false, false},
		{"other PID namespace", func(_ *system, l *LockInfo) { l.PIDNamespace = "pid:[2]" }, false, false},
		{"lock without PID namespace", func(_ *system, l *LockInfo) { l.PIDNamespace = "" }, false, false},
		{"system without PID namespace", func(sys *system, l *LockInfo) { sys.pidNamespace, l.PIDNamespace = "", "" }, false, false},
		{"no PID", func(_ *system, l *LockInfo) { l.PID = 0 }, false, false},
	}
	for _, tc := range tests {
		sys, l := here, held
		if tc.edit != nil {
			tc.edit(&sys, &l)
		}
		sys.running = func(pid int) bool {
			if pid != 42 {
				t.Errorf("%s: checked whether pid %d is running", tc.name, pid)
			}
			return tc.alive
		}
		if got := sys.stale(&l); got != tc.want {
			t.Errorf("%s: stale = %v, want %v", tc.name, got, tc.want)
		}
	}
	if here.stale(nil) {
		t.Error("an unreadable lock is stale")
	}
}

func writeLock(t *testing.T, root string, info LockInfo) {
	t.Helper()
	b, err := yaml.Marshal(&info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, LockFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveStaleLock(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, LockFile)
	sys := system{host: "box", bootID: "boot-2", pidNamespace: "pid:[1]", running: func(int) bool { return false }}

	if info, err := sys.removeStaleLock(root); info != nil || err != nil {
		t.Errorf("no lock: got %+v, %v", info, err)
	}

	// Locks that may be live stay, including one that does not parse.
	writeLock(t, root, LockInfo{Host: "other", PID: 42, Started: ts1, Instance: "a", BootID: "boot-1"})
	if info, err := sys.removeStaleLock(root); info != nil || err != nil {
		t.Errorf("lock from another host: got %+v, %v", info, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("lock from another host removed: %v", err)
	}
	if err := os.WriteFile(path, []byte("host: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if info, err := sys.removeStaleLock(root); info != nil || err != nil {
		t.Errorf("unreadable lock: got %+v, %v", info, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("unreadable lock removed: %v", err)
	}

	// A stale lock is removed, and what it held is returned.
	writeLock(t, root, LockInfo{Host: "box", PID: 42, Started: ts1, Instance: "b", BootID: "boot-1", PIDNamespace: "pid:[1]"})
	info, err := sys.removeStaleLock(root)
	if err != nil || info == nil || info.Instance != "b" || info.PID != 42 || !info.Started.Equal(ts1) {
		t.Errorf("stale lock: got %+v, %v", info, err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("stale lock not removed: %v", err)
	}
}

func TestLockRecordsSystem(t *testing.T) {
	root := t.TempDir()
	l, err := AcquireLock(root)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	info, _, err := ReadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	sys := thisSystem()
	if info.Host != sys.host || info.BootID != sys.bootID || info.PIDNamespace != sys.pidNamespace {
		t.Errorf("lock holds %+v, want the host, boot ID and PID namespace of %+v", info, sys)
	}

	// A live instance's lock is never stale.
	if old, err := RemoveStaleLock(root); old != nil || err != nil {
		t.Errorf("RemoveStaleLock on a held lock: got %+v, %v", old, err)
	}
	if err := l.Verify(); err != nil {
		t.Errorf("held lock disturbed: %v", err)
	}
}
