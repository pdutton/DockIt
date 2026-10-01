package store

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestThisSystemLinux(t *testing.T) {
	sys := thisSystem()
	if sys.bootID == "" || !strings.HasPrefix(sys.pidNamespace, "pid:[") {
		t.Fatalf("boot ID %q, PID namespace %q", sys.bootID, sys.pidNamespace)
	}
	if !sys.running(os.Getpid()) {
		t.Error("this process is not running")
	}
	if !sys.running(1) { // usually root's, so the check must accept EPERM
		t.Error("pid 1 is not running")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	if sys.running(cmd.Process.Pid) {
		t.Errorf("exited process %d is running", cmd.Process.Pid)
	}
}

// A lock left by a process that has exited, in this boot, is removed and the
// dataset opens again.
func TestRemoveStaleLockLinux(t *testing.T) {
	s := newDataset(t)
	root := s.Root()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	sys := thisSystem()
	writeLock(t, root, LockInfo{
		Host: sys.host, PID: cmd.Process.Pid, Started: ts1, Instance: "dead",
		BootID: sys.bootID, PIDNamespace: sys.pidNamespace,
	})
	if _, err := Open(root); err == nil {
		t.Fatal("Open succeeded over a lock file")
	}
	old, err := RemoveStaleLock(root)
	if err != nil || old == nil || old.Instance != "dead" {
		t.Fatalf("RemoveStaleLock = %+v, %v", old, err)
	}
	s2, err := Open(root)
	if err != nil {
		t.Fatalf("Open after removing the stale lock: %v", err)
	}
	s2.Close()
}
