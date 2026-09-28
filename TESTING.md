# Testing and developing DockIt

How to run DockIt straight from Go, without a container, for development and
testing. This works on Linux, macOS and Windows. For running DockIt for real,
see [README.md](README.md).

## Build and test

```sh
go test ./...
go build -ldflags "-X main.version=$(git describe --tags --always)" ./cmd/dockit
```

Or install the latest release with
`go install github.com/pdutton/DockIt/cmd/dockit@latest`.

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
| `internal/auth`, `internal/ratelimit` | Password and token hashing; failed-attempt limits |
