// Command buildcheck fails a release build whose version does not match the
// dataset format it writes: release vX.Y.Z must write dataset format X.Y.
// Builds that are not releases, such as untagged or dirty ones, are not
// checked.  Every scripted build runs it before building, for example
//
//	go run ./internal/buildcheck "$VERSION"
package main

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"github.com/pdutton/DockIt/internal/model"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: buildcheck <version>")
		os.Exit(2)
	}
	release, err := check(os.Args[1])
	switch {
	case err != nil:
		fmt.Fprintln(os.Stderr, "buildcheck:", err)
		os.Exit(1)
	case !release:
		fmt.Fprintf(os.Stderr, "buildcheck: %q is not a release version; not checked\n", os.Args[1])
	}
}

// releaseVersion matches a release: a clean tag, not `git describe` output
// for a later commit ("v2.1.0-3-gabc1234") or a dirty tree ("v2.1.0-dirty").
var releaseVersion = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

// check reports whether version is a release, and if it is, whether its
// major and minor match model.FormatCurrent.
func check(version string) (release bool, err error) {
	m := releaseVersion.FindStringSubmatch(version)
	if m == nil {
		return false, nil
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	if f := (model.Format{Major: major, Minor: minor}); f != model.FormatCurrent {
		return true, fmt.Errorf("release %s writes dataset format %s, but a release's major.minor must equal its dataset format; tag it v%s.N or change model.FormatCurrent",
			version, model.FormatCurrent, model.FormatCurrent)
	}
	return true, nil
}
