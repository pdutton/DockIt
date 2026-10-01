package main

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// leaveStaleLock writes a lock that this host took before the current boot,
// as a power cut would leave it.
func leaveStaleLock(t *testing.T, dir string) {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	lock := fmt.Sprintf("host: %s\npid: 4242\nstarted: 2026-09-26T21:54:43Z\ninstance: dead\nboot_id: an-earlier-boot\npid_namespace: pid:[1]\n", host)
	if err := os.WriteFile(filepath.Join(dir, "dockit.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestServeRemovesStaleLock(t *testing.T) {
	dir := initDataset(t)
	leaveStaleLock(t, dir)

	var logBuf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, slog.New(slog.NewTextHandler(&logBuf, nil)), serveConfig{dir: dir, listen: "127.0.0.1:0"}, ready)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve returned %v", err)
	}

	log := logBuf.String()
	want := fmt.Sprintf(`level=WARN msg="removed a stale lock" dataset=%s host=`, dir)
	if i := strings.Index(log, want); i < 0 || i > strings.Index(log, "DockIt serving") ||
		!strings.Contains(log, "pid=4242 started=2026-09-26T21:54:43Z") {
		t.Errorf("stale lock removal not logged before serving:\n%s", log)
	}
}

func TestUpgradeRemovesStaleLock(t *testing.T) {
	dir := initDataset(t)
	leaveStaleLock(t, dir)
	r := dockit(t, "", nil, "upgrade", dir)
	if r.code != 0 || !strings.Contains(r.stdout, "Removed a stale lock left by host ") ||
		!strings.Contains(r.stdout, "pid 4242, started 2026-09-26T21:54:43Z.") || !strings.Contains(r.stdout, "nothing to do") {
		t.Errorf("upgrade over a stale lock: %+v", r)
	}
}

func TestServeNotifiesSystemd(t *testing.T) {
	dir := initDataset(t)
	sock := filepath.Join(t.TempDir(), "notify")
	conn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: sock, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, slog.New(slog.DiscardHandler), serveConfig{dir: dir, listen: "127.0.0.1:0", notifySocket: sock}, ready)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve returned %v", err)
	}

	var got []string
	buf := make([]byte, 64)
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for range 2 {
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("after %q: %v", got, err)
		}
		got = append(got, string(buf[:n]))
	}
	if want := []string{"READY=1", "STOPPING=1"}; !slices.Equal(got, want) {
		t.Errorf("sent %q, want %q", got, want)
	}
}

// A socket that cannot be reached is logged, and serve carries on.
func TestServeNotifyFails(t *testing.T) {
	dir := initDataset(t)
	var logBuf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- serve(ctx, slog.New(slog.NewTextHandler(&logBuf, nil)),
			serveConfig{dir: dir, listen: "127.0.0.1:0", notifySocket: filepath.Join(t.TempDir(), "missing")}, ready)
	}()
	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("serve exited: %v", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("serve returned %v", err)
	}
	if log := logBuf.String(); !strings.Contains(log, `level=WARN msg="telling systemd DockIt is ready"`) {
		t.Errorf("failure to notify not logged:\n%s", log)
	}
}
