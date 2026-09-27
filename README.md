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
| `dockit upgrade` | Migrate a dataset to the format this DockIt uses, after copying the whole dataset to a backup directory beside it (for example `data.format-1.20260927T021500Z`), or inside the directory given with `-backup`. `serve` never upgrades on its own; it asks you to run this. |
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

## Run DockIt in a container

The image holds the static `dockit` binary and nothing else, on
`gcr.io/distroless/static-debian12:nonroot`. It runs as the non-root user
65532, keeps its dataset in the volume `/data`, and listens on port 8080. The
entrypoint is `dockit` and the default command is `serve`, so any other command
goes after the image name. These examples use rootless podman; docker works the
same way.

### Build the image

```sh
VERSION=$(git describe --tags --always --dirty)
podman build --build-arg VERSION=$VERSION -t dockit:$VERSION .
podman tag dockit:$VERSION dockit:latest
```

`VERSION` is what `dockit version` prints. The build compiles with
`CGO_ENABLED=0` in a Go container, so the host needs no Go install.

### Create a dataset

Use a named volume, which podman makes writable by the image's user:

```sh
podman volume create dockit-data
podman run --rm -v dockit-data:/data dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com
```

Note the one-time password it prints.

### Start and stop DockIt

```sh
podman run --rm -d --name dockit --stop-timeout 15 -p 8080:8080 -v dockit-data:/data dockit
```

Then open `http://localhost:8080/` and log in. `podman stop dockit` sends
SIGTERM; DockIt finishes in-flight requests for up to 10 seconds, then removes
`dockit.lock` and exits. Podman's default wait before SIGKILL is also 10
seconds, so `--stop-timeout 15` gives DockIt time to release the lock. If it is
killed anyway, the next start refuses because the dataset is locked; run
`podman run --rm -it -v dockit-data:/data dockit unlock` once you are sure no
other instance is running (`-it` lets it ask you first).

Run only one `serve` per volume. A second one refuses to start because the
dataset is locked, which is the intended guard. Other commands run in a
throwaway container against the same volume; `check` is safe while DockIt runs:

```sh
podman run --rm -v dockit-data:/data dockit check
```

The image sets these environment variables; the other flags in
[Start DockIt](#start-dockit) work as `-e` settings too.

| Environment       | Image value | Purpose                                            |
|-------------------|-------------|----------------------------------------------------|
| `DOCKIT_DATA`     | `/data`     | The dataset directory, the volume mount point.     |
| `DOCKIT_LISTEN`   | `:8080`     | Listen on every interface. The program's default, `localhost:8080`, cannot be reached from outside the container. |
| `DOCKIT_BASE_URL` | not set     | In the cloud, a reverse proxy terminates TLS; set `-e DOCKIT_BASE_URL=https://dockit.example.com` so the login cookie is marked Secure. |

### Use a host directory instead of a volume

A directory bind-mounted from the host must be writable by user 65532 inside
the container. With rootless podman your own user is root inside the container,
not 65532, so a plain bind mount fails with `permission denied`. Either map your
user to 65532, which keeps the files owned by you on the host:

```sh
podman run --rm -d --name dockit --stop-timeout 15 -p 8080:8080 \
  --userns=keep-id:uid=65532,gid=65532 -v /srv/dockit/data:/data:Z dockit
```

or hand the directory to that user once with
`podman unshare chown -R 65532:65532 /srv/dockit/data`, after which you edit it
through `podman unshare`. On SELinux hosts (Fedora, RHEL) add `:Z` to the
mount, as above, so the container may use the directory.

### Back up and restore

A backup is a copy of the volume without `dockit.lock`; it is safe to take
while DockIt runs. For a named volume:

```sh
podman unshare tar -C "$(podman volume inspect dockit-data --format '{{.Mountpoint}}')" \
  --exclude=./dockit.lock --exclude='.*.tmp' -cf dockit-backup.tar .
```

To restore, stop DockIt and load the copy into a new, empty volume:

```sh
podman volume create dockit-restored
podman volume import dockit-restored dockit-backup.tar
```

With a host directory, copy the directory as described in [Your data](#your-data).

### Upgrade

Build or pull the new image, stop DockIt, and run `upgrade`. It copies the
dataset to a backup directory before changing anything. By default that goes
beside the dataset directory, which in the container is `/`, neither writable nor
kept, so give it a second volume with `-backup`:

```sh
podman stop dockit
podman volume create dockit-backups
podman run --rm -v dockit-data:/data -v dockit-backups:/backup dockit upgrade -backup /backup
podman run --rm -v dockit-data:/data dockit check
```

The image's `/backup` is owned by its user, so a new named volume there is
writable. The backup lands in the `dockit-backups` volume, named for example
`data.format-1.20260927T021500Z`. Remove it once you are happy with the result,
then start DockIt from the new image as before. With a host directory, point
`-backup` at a second host directory, mounted and owned the same way as the
dataset.

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
