# DockIt

DockIt is a simple to-do list tracker.

It keeps track of tasks across multiple projects: who owns each task, what state
it is in, how important it is, and the discussion around it. People use it
through a web page; scripts and other tools use it through a REST API.

DockIt runs on your own machine or in the cloud, and your task data stays yours:
it is easy to back up, restore, move to another install, and read from other
systems.

DockIt is in early development. It is licensed under the AGPL (see [LICENSE](LICENSE)).

## Usage

DockIt is a single program, `dockit`, with subcommands. Build it with Go:

```sh
go install github.com/pdutton/DockIt/cmd/dockit@latest
```

Every command that works on a dataset takes the dataset directory as its last
argument, or reads it from `$DOCKIT_DATA` if none is given.

### Create a dataset

```sh
dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com /path/to/data
```

The directory must be missing or empty. This creates the dataset and its first
admin user, and prints a one-time password for that user. The password is shown
only once and must be changed at first login.

### Start DockIt

```sh
dockit serve /path/to/data
```

This locks the dataset and serves the web interface at `http://localhost:8080/`
and the REST API under `/api/v1/`. Log in with the admin user and one-time
password from `dockit init`; you will be asked to choose your own password. Only
one instance may run against a dataset at a time; stop it with Ctrl+C, which
releases the lock.

| Flag                 | Environment       | Default          | Purpose                           |
|----------------------|-------------------|------------------|-----------------------------------|
| `-listen`            | `DOCKIT_LISTEN`   | `localhost:8080` | Address to listen on. Use `:8080` to accept connections from other machines. |
| `-tls-cert`, `-tls-key` | `DOCKIT_TLS_CERT`, `DOCKIT_TLS_KEY` | | Serve HTTPS directly. In the cloud, terminate TLS at a reverse proxy instead. |
| `-base-url`          | `DOCKIT_BASE_URL` |                  | The URL people use to reach DockIt, such as `https://dockit.example.com`. An `https` URL marks the login cookie Secure; set it when a proxy terminates TLS. |
| `-dev-insecure-user` |                   |                  | Development only: no authentication; every request acts as this user. Refuses to start unless listening on localhost. |

### Use the REST API

Requests authenticate with an API token: `Authorization: Bearer <token>`.
Request bodies are JSON and must be sent as `Content-Type: application/json`.
To update a record, send the `ETag` you read back as `If-Match`; if someone else
changed the record in the meantime, the update is rejected with `412` and the
current record. The endpoints are listed in [DESIGN.md](DESIGN.md#rest-api).

Create a token on the **My account** page of the web interface (or with
`POST /api/v1/me/tokens`). A token acts as you, with your role, and is shown
only once.

### Other commands

| Command          | Purpose                                                          |
|------------------|------------------------------------------------------------------|
| `dockit unlock`  | Remove a stale lock left by an instance that is no longer running. Shows who holds the lock and asks first; `-yes` skips the question. |
| `dockit check`   | Validate a dataset: file names match IDs, required fields are present, and references between records resolve. Safe to run while DockIt is running. `-q` prints errors only. Exits 1 if there are errors. |
| `dockit upgrade` | Migrate a dataset to the format this DockIt uses, after copying the whole dataset to a backup directory beside it (for example `data.format-1.20260927T021500Z`). `serve` never upgrades on its own; it asks you to run this. |
| `dockit version` | Print the build version and the dataset format it supports.      |

Run `dockit <command> -h` for a command's flags.

## Your data

A dataset is a directory of YAML files, one per project, task and user, so any
tool can read it. See [DESIGN.md](DESIGN.md#dataset) for the layout and formats.

- **Back up** by copying the dataset directory. Every file is written
  atomically, so a copy taken while DockIt runs holds only complete files. Leave
  out `dockit.lock`. Keeping the dataset in git works well too.
- **Restore** by stopping DockIt, replacing the directory, and starting DockIt.
- **Move** a dataset to another install by copying it; that install must be the
  same version or newer.
- **Edit by hand** only while DockIt is stopped, then run `dockit check` before
  starting it again.
- **Share for reading** without secrets by leaving out `auth/`, which holds only
  one-way password and token hashes anyway.
- Keep the dataset on a local disk, or a network filesystem with reliable
  exclusive file creation, which the lock file depends on.

## Development

```sh
go test ./...
go build -ldflags "-X main.version=$(git describe --tags --always)" ./cmd/dockit
```

To try changes without logging in, run
`dockit serve -dev-insecure-user <user id> <dir>`, which only listens on
localhost. The code follows the architecture in [DESIGN.md](DESIGN.md):

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
