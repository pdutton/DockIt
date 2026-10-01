# Operating DockIt

Reference for running DockIt beyond the quick start in [README.md](README.md).
The examples use rootless podman; docker works the same way.

## The container image

The image holds the static `dockit` binary and nothing else, on
`gcr.io/distroless/static-debian12:nonroot`. It runs as the non-root user
65532, keeps its dataset in the volume `/data`, and listens on port 8080. The
entrypoint is `dockit` and the default command is `serve`, so any other command
goes after the image name. The build compiles with `CGO_ENABLED=0` in a Go
container, so the host needs no Go install. `VERSION` is what `dockit version`
prints.

The flags in the README's commands do this:

- `--userns=keep-id:uid=65532,gid=65532` maps your user to 65532 inside the
  container, so DockIt can write to a host directory you own and the files stay
  owned by you on the host. Without it, rootless podman makes you root inside
  the container, not 65532, and the bind mount fails with `permission denied`.
- `:Z` on a mount lets the container use the directory on SELinux hosts (Fedora,
  RHEL).
- `--stop-timeout 15`: `podman stop` sends SIGTERM, and DockIt finishes
  in-flight requests for up to 10 seconds, then removes `dockit.lock` and exits.
  Podman's default wait before SIGKILL is also 10 seconds, so without this flag
  DockIt can be killed before releasing the lock.

Instead of mapping your user, you can hand the directory to user 65532 once
with `podman unshare chown -R 65532:65532 $DOCKIT_DIR` and leave out `--userns`;
after that you edit it through `podman unshare`.

## Settings

Every command that works on a dataset takes the dataset directory as its last
argument, or reads it from `$DOCKIT_DATA` if none is given. `serve` also takes:

| Flag                 | Environment       | Default          | Purpose                           |
|----------------------|-------------------|------------------|-----------------------------------|
| `-listen`            | `DOCKIT_LISTEN`   | `localhost:8080` | Address to listen on. Use `:8080` to accept connections from other machines. |
| `-tls-cert`, `-tls-key` | `DOCKIT_TLS_CERT`, `DOCKIT_TLS_KEY` | | Serve HTTPS directly. In the cloud, terminate TLS at a reverse proxy instead. |
| `-base-url`          | `DOCKIT_BASE_URL` |                  | The URL people use to reach DockIt, such as `https://dockit.example.com`. An `https` URL marks the login cookie Secure; set it when a proxy terminates TLS. |
| `-dev-insecure-user` |                   |                  | Development only: no authentication; every request acts as this user. Refuses to start unless listening on localhost. |

In a container, pass them as `-e` settings. The image sets two of them:

| Environment       | Image value | Why                                                |
|-------------------|-------------|----------------------------------------------------|
| `DOCKIT_DATA`     | `/data`     | The dataset directory, the volume mount point.     |
| `DOCKIT_LISTEN`   | `:8080`     | Listen on every interface. The program's default, `localhost:8080`, cannot be reached from outside the container. |

## Commands

| Command          | Purpose                                                          |
|------------------|------------------------------------------------------------------|
| `dockit init`    | Create a dataset and its first admin user in a missing or empty directory. Prints a one-time password, which must be changed at first login. |
| `dockit serve`   | Lock the dataset, upgrade it if it is from an older DockIt (see [Upgrading](#upgrading)), and serve the web interface at `/` and the REST API under `/api/v1/`. Only one instance may run against a dataset at a time. |
| `dockit check`   | Validate a dataset: file names match IDs, required fields are present, and references between records resolve. Safe to run while DockIt is running. `-q` prints errors only. Exits 1 if there are errors. |
| `dockit unlock`  | Remove a stale lock left by an instance that is no longer running. Shows who holds the lock and asks first; `-yes` skips the question. |
| `dockit upgrade` | Upgrade a dataset, in place, to the format this DockIt uses, without serving it. `serve` does the same when it starts. It makes no backup, so copy the dataset first. |
| `dockit version` | Print the build version and the dataset format it supports.      |

Run `dockit <command> -h` for a command's flags.

## Stale locks

If DockIt is killed without releasing its lock, the next start refuses because
the dataset is locked.

On Linux, `serve` and `upgrade` remove the lock themselves when they can tell
it is stale: the same host took it before the last reboot, or took it since in
the same PID namespace and that process has gone. They log
`removed a stale lock` with the old lock's host, PID and start time. That
lets a service manager such as systemd bring DockIt back after a crash or a
power cut.

A container usually gets a new host name and PID namespace on each run, so a
stale lock from a killed container is not removed this way. Once you are sure
no other instance is running, remove the lock (`-it` lets `unlock` ask you
first):

```sh
podman run --rm -it --userns=keep-id:uid=65532,gid=65532 -v $DOCKIT_DIR:/data:Z dockit unlock
```

## Upgrading

A new version of DockIt upgrades the dataset when `serve` starts, in place,
and logs the change (`podman logs dockit`):

```
level=INFO msg="dataset format updated" dataset=/data from=2.0 to=2.1
```

It makes no backup, and older versions refuse an upgraded dataset, so copy the
dataset directory first, with DockIt stopped:

```sh
cp -r $DOCKIT_DIR $DOCKIT_DIR.before-upgrade
```

Run `check` after upgrading, and remove the copy once you are happy with the
result. If the upgrade fails, or you go back to the older image, restore the
copy.

The first two numbers of a DockIt version are the dataset format it writes:
DockIt 2.1.x writes format 2.1. A new minor format only adds to the dataset,
so the upgrade just records the new number, once the dataset loads without
errors; a dataset with errors keeps its format until they are fixed. A new
major format rewrites
records. A release that changes only the last number leaves the dataset alone.
`dockit upgrade` does the same upgrade without starting DockIt.

## Your data

A dataset is a directory of YAML files, one per project, task and user, plus
`links.yaml` for the links between tasks, so any tool can read it. See [DESIGN.md](DESIGN.md#dataset) for the layout and formats.

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

## Using a named volume

A named volume works instead of a host directory. Podman makes a new volume
writable by the image's user, so no `--userns` is needed:

```sh
podman volume create dockit-data
podman run --rm -v dockit-data:/data dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com
podman run --rm -d --name dockit --stop-timeout 15 -p 8080:8080 -v dockit-data:/data dockit
podman run --rm -v dockit-data:/data dockit check
```

Back up a volume (safe while DockIt runs), and restore it into a new, empty
volume with DockIt stopped:

```sh
podman unshare tar -C "$(podman volume inspect dockit-data --format '{{.Mountpoint}}')" \
  --exclude=./dockit.lock --exclude='.*.tmp' -cf dockit-backup.tar .
podman volume create dockit-restored
podman volume import dockit-restored dockit-backup.tar
```

To upgrade, stop DockIt, back up the volume as above, then start the new image
and check the result:

```sh
podman stop dockit
podman run --rm -d --name dockit --stop-timeout 15 -p 8080:8080 -v dockit-data:/data dockit
podman run --rm -v dockit-data:/data dockit check
```

## The REST API

Requests authenticate with an API token: `Authorization: Bearer <token>`.
Create one on the **My account** page of the web interface (or with
`POST /api/v1/me/tokens`). A token acts as you, with your role, and is shown
only once.

Request bodies are JSON and must be sent as `Content-Type: application/json`.
To update a record, send the `ETag` you read back as `If-Match`; if someone else
changed the record in the meantime, the update is rejected with `412` and the
current record. The endpoints are listed in [DESIGN.md](DESIGN.md#rest-api).
