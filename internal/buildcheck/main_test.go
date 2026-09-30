package main

import (
	"fmt"
	"testing"

	"github.com/pdutton/DockIt/internal/model"
)

func TestCheck(t *testing.T) {
	cur := model.FormatCurrent
	for _, tc := range []struct {
		version       string
		release, fail bool
	}{
		{fmt.Sprintf("v%d.%d.0", cur.Major, cur.Minor), true, false},
		{fmt.Sprintf("v%d.%d.17", cur.Major, cur.Minor), true, false},
		{fmt.Sprintf("v%d.%d.0", cur.Major, cur.Minor+1), true, true},
		{fmt.Sprintf("v%d.%d.0", cur.Major+1, cur.Minor), true, true},
		{fmt.Sprintf("v%d.%d.0", cur.Major-1, cur.Minor), true, true},
		// Not releases: never checked.
		{fmt.Sprintf("v%d.%d.0-dirty", cur.Major+1, cur.Minor), false, false},
		{fmt.Sprintf("v%d.%d.0-3-gabc1234", cur.Major+1, cur.Minor), false, false},
		{"abc1234", false, false},
		{"dev", false, false},
		{"", false, false},
	} {
		release, err := check(tc.version)
		if release != tc.release || (err != nil) != tc.fail {
			t.Errorf("%q: release %v, err %v", tc.version, release, err)
		}
	}
}
