# Testing and developing DockIt

How to run DockIt straight from Go, without a container, for development and
testing. This works on Linux, macOS and Windows. For running DockIt for real,
see [README.md](README.md).

## Build and test

```sh
go test ./...
VERSION=$(git describe --tags --always --dirty)
go run ./internal/buildcheck $VERSION
go build -ldflags "-X main.version=$VERSION" ./cmd/dockit
```

`go test` also runs the Claude Code skill's client script,
`skills/dockit/dockit-api`, against a test server (`internal/api/skill_test.go`).
Those tests need bash, curl and jq, and are skipped without them and on Windows.

`buildcheck` fails a release build whose tag does not match the dataset format:
release vX.Y.Z must write format X.Y (`model.FormatCurrent`). Any change to
what may appear in the dataset bumps the minor, or the major if existing data
has to be rewritten, and the release is tagged to match. Anything else is a
patch release. See [DESIGN.md](DESIGN.md#dataset-metadata-and-versioning).

Or install the latest release with
`go install github.com/pdutton/DockIt/cmd/dockit@latest`.

## Build the .deb packages

`packaging/build-deb.sh` builds `dockit` for Linux and packages it, with the
files in `packaging/`, as `dist/dockit_<version>_amd64.deb` and
`dist/dockit_<version>_arm64.deb`. It needs only a POSIX shell (Git Bash on
Windows) and Go: it runs [nFPM](https://nfpm.goreleaser.com/), whose
configuration is `packaging/nfpm.yaml`, with `go run`. nFPM needs Go 1.26.4 or
later; an older Go fetches a newer toolchain itself.

```sh
packaging/build-deb.sh
```

The version comes from `git describe`, as above, or from the first argument.
Release v2.2.7 makes package version 2.2.7. A build after a tag, or from a
changed tree, makes a version that apt orders between the two releases, such
as 2.2.7+3.gabc1234.dirty. For a release, run it in a clean checkout of the
tag and attach both packages to the GitHub release. OPERATIONS.md describes
what the package does.

## Build the APK packages

`packaging/build-apk.sh` does the same for Alpine Linux, with nFPM
configuration `packaging/nfpm-apk.yaml`, and makes
`dist/dockit_<version>-r0_x86_64.apk` and
`dist/dockit_<version>-r0_aarch64.apk`:

```sh
packaging/build-apk.sh
```

Release v2.2.8 makes package version 2.2.8-r0. An Alpine version cannot hold a
commit hash, so a build 3 commits after v2.2.8 makes 2.2.8_git3-r0, and a
changed tree adds the build time, as in 2.2.8_git3_p20261001190000-r0; apk
orders both between the two releases. The packages are not signed. For a
release, attach both to the GitHub release with the .deb packages.

## Run it locally

Create a dataset in a missing or empty directory, and note the one-time
password it prints:

```sh
./dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com ./scratch
```

Serve it at http://localhost:8080/ and stop it with Ctrl+C, which releases the
lock:

```sh
./dockit serve ./scratch
```

To try changes without logging in, serve as a fixed user. This only listens on
localhost:

```sh
./dockit serve -dev-insecure-user pdutton ./scratch
```

Every command takes the dataset directory as its last argument, or reads it from
`$DOCKIT_DATA`. The other commands and settings are in
[OPERATIONS.md](OPERATIONS.md).

## Code layout

The code follows the architecture in [DESIGN.md](DESIGN.md):

| Package             | Role                                                   |
|---------------------|--------------------------------------------------------|
| `cmd/dockit`        | Command line                                           |
| `internal/web`      | Web interface: server-rendered HTML                    |
| `internal/api`      | REST interface: JSON under `/api/v1`                   |
| `internal/service`  | Every rule: permissions, validation, versions, IDs     |
| `internal/index`    | In-memory copy of the dataset; loading and checking    |
| `internal/store`    | YAML files, atomic writes, lock file                   |
| `internal/model`    | Records, enumerations, field checks                    |
| `internal/upgrade`  | Dataset format migrations                              |
| `internal/buildcheck` | Build step: a release's version matches its dataset format |
| `internal/auth`, `internal/ratelimit` | Password and token hashing; failed-attempt limits |
| `skills/dockit`     | Claude Code skill: `SKILL.md` and the `dockit-api` client script (not Go) |
