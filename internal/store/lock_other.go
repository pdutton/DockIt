//go:build !linux

package store

// Without a boot ID no lock is ever found stale, so the PID namespace and
// process checks are never reached.
func bootID() string       { return "" }
func pidNamespace() string { return "" }

func processRunning(int) bool { return true }
