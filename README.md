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

This locks the dataset and serves the REST API at `http://localhost:8080/api/v1/`.
The web interface is not implemented yet. Only one instance may run against a
dataset at a time; stop it with Ctrl+C, which releases the lock.

| Flag                 | Environment       | Default          | Purpose                           |
|----------------------|-------------------|------------------|-----------------------------------|
| `-listen`            | `DOCKIT_LISTEN`   | `localhost:8080` | Address to listen on. Use `:8080` to accept connections from other machines. |
| `-tls-cert`, `-tls-key` | `DOCKIT_TLS_CERT`, `DOCKIT_TLS_KEY` | | Serve HTTPS directly. In the cloud, terminate TLS at a reverse proxy instead. |
| `-dev-insecure-user` |                   |                  | Development only: no authentication; every request acts as this user. Refuses to start unless listening on localhost. |

### Use the REST API

Requests authenticate with an API token: `Authorization: Bearer <token>`.
Request bodies are JSON and must be sent as `Content-Type: application/json`.
To update a record, send the `ETag` you read back as `If-Match`; if someone else
changed the record in the meantime, the update is rejected with `412` and the
current record. The endpoints are listed in [DESIGN.md](DESIGN.md#rest-api).

Tokens are created with `POST /api/v1/me/tokens`. Until the web interface
exists, the way to get your first token is to run DockIt briefly in development
mode on your own machine:

```sh
dockit serve -dev-insecure-user pdutton /path/to/data
curl -H 'Content-Type: application/json' -d '{"name":"my script"}' http://localhost:8080/api/v1/me/tokens
```

The token is shown only once. Stop DockIt and start it again without
`-dev-insecure-user`.

### Other commands

| Command          | Purpose                                                          |
|------------------|------------------------------------------------------------------|
| `dockit unlock`  | Remove a stale lock left by an instance that is no longer running. Shows who holds the lock and asks first; `-yes` skips the question. |
| `dockit check`   | Validate a dataset: file names match IDs, required fields are present, and references between records resolve. Safe to run while DockIt is running. `-q` prints errors only. Exits 1 if there are errors. |
| `dockit upgrade` | Back up a dataset, then migrate it to the current format. *(Not implemented yet.)* |
| `dockit version` | Print the build version and the dataset format it supports.      |

Run `dockit <command> -h` for a command's flags.
