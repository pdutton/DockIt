package store

import (
	"errors"
	"os"
	"strings"
	"syscall"
)

// bootID returns the kernel's random ID for the current boot, which changes
// on every reboot.
func bootID() string {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// pidNamespace identifies the PID namespace this process runs in, such as
// "pid:[4026531836]".  PIDs mean the same thing only within one namespace.
func pidNamespace() string {
	ns, err := os.Readlink("/proc/self/ns/pid")
	if err != nil {
		return ""
	}
	return ns
}

// processRunning reports whether a process with this PID exists in this PID
// namespace.  Signal 0 checks without sending anything; EPERM means the
// process exists but belongs to another user.
func processRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
