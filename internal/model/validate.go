package model

import (
	"fmt"
	"net/mail"
	"unicode"
	"unicode/utf8"
)

// FieldError reports an invalid field value.
type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string {
	return e.Field + ": " + e.Message
}

// CheckText checks a required single-line text field: 1 to max characters and
// no control characters.
func CheckText(field, s string, max int) error {
	n := utf8.RuneCountInString(s)
	if n == 0 {
		return &FieldError{field, "required"}
	}
	if n > max {
		return &FieldError{field, fmt.Sprintf("longer than %d characters", max)}
	}
	if !utf8.ValidString(s) {
		return &FieldError{field, "not valid UTF-8"}
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return &FieldError{field, "contains control characters"}
		}
	}
	return nil
}

// CheckUserName checks a user's display name.
func CheckUserName(s string) error {
	return CheckText("name", s, 100)
}

// CheckEmail checks that s is a single bare email address.
func CheckEmail(s string) error {
	if s == "" {
		return &FieldError{"email", "required"}
	}
	a, err := mail.ParseAddress(s)
	if err != nil || a.Name != "" || a.Address != s {
		return &FieldError{"email", "not a valid email address"}
	}
	return nil
}

// CheckUserID checks a user ID.
func CheckUserID(s string) error {
	if !ValidUserID(s) {
		return &FieldError{"id", "must be 2-32 characters: a-z, then a-z, 0-9, '.', '_' or '-'"}
	}
	return nil
}

// CheckRole checks a role id.
func CheckRole(s string) error {
	if !Roles.Valid(s) {
		return &FieldError{"role", fmt.Sprintf("unknown role %q", s)}
	}
	return nil
}
