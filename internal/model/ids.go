package model

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// IDs are used as file names, and the dataset may sit on a case-insensitive
// filesystem, so each ID type has a fixed case.
var (
	projectIDRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}$`)
	userIDRe    = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,31}$`)
)

// ValidProjectID reports whether id is a well-formed project ID: 2-10
// characters, A-Z then A-Z0-9.
func ValidProjectID(id string) bool {
	return projectIDRe.MatchString(id)
}

// ValidUserID reports whether id is a well-formed user ID: 2-32 characters,
// a-z then a-z0-9._-
func ValidUserID(id string) bool {
	return userIDRe.MatchString(id)
}

// TaskID formats the ID of task number n in project pid.
func TaskID(pid string, n int) string {
	return pid + "-" + strconv.Itoa(n)
}

// ParseTaskID splits a task ID into its project ID and task number.
func ParseTaskID(id string) (pid string, n int, err error) {
	i := strings.LastIndexByte(id, '-')
	if i < 0 {
		return "", 0, fmt.Errorf("task ID %q: missing '-'", id)
	}
	pid, num := id[:i], id[i+1:]
	if !ValidProjectID(pid) {
		return "", 0, fmt.Errorf("task ID %q: invalid project ID", id)
	}
	// Reject signs, leading zeros and anything else that would not round-trip.
	n, err = strconv.Atoi(num)
	if err != nil || n < 1 || strconv.Itoa(n) != num {
		return "", 0, fmt.Errorf("task ID %q: invalid task number", id)
	}
	return pid, n, nil
}
