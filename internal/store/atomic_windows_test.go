package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Go opens files on Windows without FILE_SHARE_DELETE, so an open reader
// blocks the rename, as an outside reader or antivirus would.

func TestWriteFileAtomicRetriesWhileTargetOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.yaml")
	WriteFileAtomic(path, []byte("old\n"))
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		f.Close()
	}()
	if err := WriteFileAtomic(path, []byte("new\n")); err != nil {
		t.Fatalf("write did not succeed once the reader closed: %v", err)
	}
	if got := readString(t, path); got != "new\n" {
		t.Errorf("content = %q", got)
	}
}

func TestWriteFileAtomicGivesUpCleanly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.yaml")
	WriteFileAtomic(path, []byte("old\n"))
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := WriteFileAtomic(path, []byte("new\n")); err == nil {
		t.Fatal("write succeeded while the target was held open")
	}
	if got := readString(t, path); got != "old\n" {
		t.Errorf("content = %q after failed write", got)
	}
	if _, err := os.Stat(tmpName(path)); !os.IsNotExist(err) {
		t.Error("temp file left behind")
	}
}
