# Operating DockIt

Reference for running DockIt beyond the quick start in [README.md](README.md).
The examples use rootless podman; docker works the same way. To run DockIt
without a container, see
[Running as a systemd service](#running-as-a-systemd-service), from the .deb
package or by hand, or, on Alpine Linux,
[Running as an OpenRC service](#running-as-an-openrc-service), from the APK
package or by hand.

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

For the systemd service, set them in `/etc/dockit/dockit.conf`, and for the
OpenRC service in `/etc/conf.d/dockit`. In a container, pass them as `-e`
settings. The image sets two of them:

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

`serve` exits with status 78 when it cannot start until its settings or the
dataset are fixed: before `init`, when the dataset has errors, or while another
instance holds the lock. Other errors exit with status 1, and mistakes on the
command line with 2.

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
and logs the change (`podman logs dockit`, `journalctl -u dockit` for the
systemd service, or `/var/log/dockit/dockit.log` for the OpenRC service):

```
level=INFO msg="dataset format updated" dataset=/data from=2.0 to=2.1
```

It makes no backup, and older versions refuse an upgraded dataset, so copy the
dataset directory first, with DockIt stopped:

```sh
cp -r $DOCKIT_DIR $DOCKIT_DIR.before-upgrade
```

Run `check` after upgrading, and remove the copy once you are happy with the
result. If the upgrade fails, or you go back to the older image or package,
restore the copy.

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

## Running as a systemd service

DockIt can run as a systemd service, without a container. On Debian, Ubuntu
and their derivatives, install the .deb package; elsewhere, install the files
by hand. Either way, DockIt runs as the `dockit` user, keeps its dataset in
`/var/lib/dockit`, reads its [settings](#settings) from
`/etc/dockit/dockit.conf`, and logs to the journal.

### The .deb package

Download `dockit_<version>_amd64.deb`, or `_arm64.deb`, from the
[GitHub releases](https://github.com/pdutton/DockIt/releases), or build it as
in [TESTING.md](TESTING.md#build-the-deb-packages), and install it:

```sh
sudo apt install ./dockit_2.2.7_amd64.deb
```

That installs `dockit` and the service, creates the `dockit` user and
`/var/lib/dockit`, and enables the service so that it starts at boot, but
does not start it yet. Create the dataset as the `dockit` user, so that user
owns the files, and note the one-time password. Then start DockIt:

```sh
sudo -u dockit dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com /var/lib/dockit
sudo systemctl start dockit
```

DockIt listens on `localhost:8080`. Change that, and any other setting, in
`/etc/dockit/dockit.conf`, then run `sudo systemctl restart dockit`. Upgrades
keep your changes to that file.

To upgrade, stop DockIt, copy the dataset (see [Upgrading](#upgrading)), and
install the new package:

```sh
sudo systemctl stop dockit
sudo cp -a /var/lib/dockit /var/lib/dockit.before-upgrade
sudo apt install ./dockit_2.2.8_amd64.deb
```

Installing a new version starts DockIt again, or restarts it if it is
running, and DockIt upgrades the dataset as it starts. If DockIt fails to
start, apt says so, and `journalctl -u dockit` shows why. The package leaves
DockIt stopped if you disabled it (`systemctl disable dockit`), or if
`/usr/sbin/policy-rc.d` forbids starting services, as it does in some
container and WSL images.

`sudo apt remove dockit` stops DockIt and removes it, but keeps
`/etc/dockit/dockit.conf`. `sudo apt purge dockit` removes that too. Neither
removes the dataset or the `dockit` user.

If you installed DockIt by hand before, first remove the unit, sysusers.d and
tmpfiles.d files you installed under `/etc`, which would hide the package's.

### Installing by hand

`packaging/` holds the files the package installs:

| File                                | Install as                           | Purpose                                     |
|-------------------------------------|--------------------------------------|---------------------------------------------|
| `packaging/systemd/dockit.service`  | `/etc/systemd/system/dockit.service` | Runs `dockit serve` as the `dockit` user, sandboxed |
| `packaging/dockit.conf`             | `/etc/dockit/dockit.conf`            | The [settings](#settings), as `DOCKIT_*` variables |
| `packaging/systemd/dockit.sysusers` | `/etc/sysusers.d/dockit.conf`        | Creates the `dockit` user and group         |
| `packaging/systemd/dockit.tmpfiles` | `/etc/tmpfiles.d/dockit.conf`        | Creates the dataset directory, `/var/lib/dockit` |

Build `dockit` as in [TESTING.md](TESTING.md#build-and-test). Then, from the
repository, install it and the files, and create the user and the directory:

```sh
sudo install -m 0755 dockit /usr/bin/dockit
sudo install -D -m 0644 packaging/systemd/dockit.service /etc/systemd/system/dockit.service
sudo install -D -m 0644 packaging/dockit.conf /etc/dockit/dockit.conf
sudo install -D -m 0644 packaging/systemd/dockit.sysusers /etc/sysusers.d/dockit.conf
sudo install -D -m 0644 packaging/systemd/dockit.tmpfiles /etc/tmpfiles.d/dockit.conf
sudo systemd-sysusers /etc/sysusers.d/dockit.conf
sudo systemd-tmpfiles --create /etc/tmpfiles.d/dockit.conf
sudo systemctl daemon-reload
```

DockIt listens on `localhost:8080`; change that, and any other setting, in
`/etc/dockit/dockit.conf`. Create the dataset as the `dockit` user, so that
user owns the files, and note the one-time password:

```sh
sudo -u dockit dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com /var/lib/dockit
```

Start DockIt, now and at every boot:

```sh
sudo systemctl enable --now dockit
```

To upgrade, stop DockIt, copy the dataset (see [Upgrading](#upgrading)),
install the new binary, and start it again:

```sh
sudo systemctl stop dockit
sudo cp -a /var/lib/dockit /var/lib/dockit.before-upgrade
sudo install -m 0755 dockit /usr/bin/dockit
sudo systemctl start dockit
```

### Managing the service

- `systemctl status dockit` shows whether DockIt is running, and
  `journalctl -u dockit` shows its log.
- `systemctl start dockit` waits until DockIt is listening, and fails if it
  cannot start.
- `systemctl stop dockit` shuts DockIt down cleanly, releasing the lock.
- After a crash, systemd restarts DockIt, which removes the lock the crash
  left behind (see [Stale locks](#stale-locks)).
- When `serve` cannot start until something is fixed, such as before `init`,
  it exits with status 78 (`status=78/CONFIG` in the log) and systemd does not
  restart it. Fix what the log says, then `systemctl start dockit`.
- Run the other commands as `dockit` too, such as
  `sudo -u dockit dockit check /var/lib/dockit`.

The service can write only to its dataset directory, and cannot see home
directories. To keep the dataset somewhere else, create the directory owned
by `dockit`, set `DOCKIT_DATA`, and let the service write there with
`sudo systemctl edit dockit`:

```ini
[Service]
ReadWritePaths=/srv/dockit
```

To listen on a port below 1024, add `AmbientCapabilities=CAP_NET_BIND_SERVICE`
and `CapabilityBoundingSet=CAP_NET_BIND_SERVICE` the same way.

## Running as an OpenRC service

On Alpine Linux, DockIt can run as an OpenRC service, without a container.
Install the APK package, or install the files by hand. Either way, DockIt runs
as the `dockit` user, keeps its dataset in `/var/lib/dockit`, reads its
[settings](#settings) from `/etc/conf.d/dockit`, and logs to
`/var/log/dockit/dockit.log`.

The commands use doas, which setup-alpine sets up for the admin user it
creates; sudo works the same way. As root, leave out `doas`, and run the
`dockit` commands as the `dockit` user with su instead, such as
`su -s /bin/sh -c 'dockit check /var/lib/dockit' dockit`.

### The APK package

Download `dockit_<version>-r0_x86_64.apk`, or `_aarch64.apk`, from the
[GitHub releases](https://github.com/pdutton/DockIt/releases), or build it as
in [TESTING.md](TESTING.md#build-the-apk-packages), and install it. The
package is not signed, so apk needs `--allow-untrusted`:

```sh
doas apk add --allow-untrusted ./dockit_2.2.8-r0_x86_64.apk
```

That installs `dockit` and the service, and creates the `dockit` user and
`/var/lib/dockit`. As is usual on Alpine, it neither starts DockIt nor adds it
to a runlevel. Create the dataset as the `dockit` user, so that user owns the
files, and note the one-time password. Then start DockIt, now and at every
boot:

```sh
doas -u dockit dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com /var/lib/dockit
doas rc-update add dockit default
doas rc-service dockit start
```

DockIt listens on `localhost:8080`. Change that, and any other setting, in
`/etc/conf.d/dockit`, then run `doas rc-service dockit restart`. Upgrades keep
your changes to that file, and put the new version beside it as
`/etc/conf.d/dockit.apk-new`.

To upgrade, stop DockIt, copy the dataset (see [Upgrading](#upgrading)), and
install the new package:

```sh
doas rc-service dockit stop
doas cp -a /var/lib/dockit /var/lib/dockit.before-upgrade
doas apk add --allow-untrusted ./dockit_2.2.9-r0_x86_64.apk
```

Installing a new version starts DockIt again if it is in a runlevel, or
restarts it if it is running, and DockIt upgrades the dataset as it starts. If
DockIt fails to start, apk shows the error from the log, and the upgrade still
succeeds; fix the problem and run `doas rc-service dockit start`. An older
package installs the same way, and restarts DockIt with the older version.

`doas apk del dockit` stops DockIt and removes it, but keeps
`/etc/conf.d/dockit` if you changed it. It never removes the dataset, the log
or the `dockit` user, and DockIt stays in its runlevel, so it starts at boot
again if you reinstall the package.

If you installed DockIt by hand before, first stop it and remove
`/etc/init.d/dockit`, or apk keeps that file and puts the package's beside it
as `/etc/init.d/dockit.apk-new`. The package keeps your `/etc/conf.d/dockit`.

### Installing by hand

`packaging/openrc/` holds the files the package installs:

| File                                | Install as                | Purpose                                     |
|-------------------------------------|---------------------------|---------------------------------------------|
| `packaging/openrc/dockit.initd`     | `/etc/init.d/dockit`      | Runs `dockit serve` as the `dockit` user, under supervise-daemon |
| `packaging/openrc/dockit.confd`     | `/etc/conf.d/dockit`      | The [settings](#settings), as exported `DOCKIT_*` variables, and the log file |
| `packaging/openrc/dockit.logrotate` | `/etc/logrotate.d/dockit` | Rotates the log, if logrotate is installed  |

Build `dockit` as in [TESTING.md](TESTING.md#build-and-test), with
`CGO_ENABLED=0` if you build it on another distribution, so that it does not
need that distribution's C library. Then, from the repository, install it and
the files, and create the user and the directory:

```sh
doas install -m 0755 dockit /usr/bin/dockit
doas install -m 0755 packaging/openrc/dockit.initd /etc/init.d/dockit
doas install -m 0644 packaging/openrc/dockit.confd /etc/conf.d/dockit
doas install -D -m 0644 packaging/openrc/dockit.logrotate /etc/logrotate.d/dockit
doas addgroup -S dockit
doas adduser -S -D -H -h /var/lib/dockit -s /sbin/nologin -G dockit -g DockIt dockit
doas install -d -m 0750 -o dockit -g dockit /var/lib/dockit
```

DockIt listens on `localhost:8080`; change that, and any other setting, in
`/etc/conf.d/dockit`. Create the dataset as the `dockit` user, so that user
owns the files, and note the one-time password:

```sh
doas -u dockit dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com /var/lib/dockit
```

Start DockIt, now and at every boot:

```sh
doas rc-update add dockit default
doas rc-service dockit start
```

To upgrade, stop DockIt, copy the dataset (see [Upgrading](#upgrading)),
install the new binary, and start it again:

```sh
doas rc-service dockit stop
doas cp -a /var/lib/dockit /var/lib/dockit.before-upgrade
doas install -m 0755 dockit /usr/bin/dockit
doas rc-service dockit start
```

### Managing the OpenRC service

- `rc-service dockit status` shows whether DockIt is running, and
  `/var/log/dockit/dockit.log` is its log.
- `rc-service dockit start` waits until DockIt is listening. If it cannot
  start, such as before `init`, with dataset errors, while another instance
  holds the lock, or when the port is in use, `start` fails and shows the error
  from the log.
- `rc-service dockit stop` shuts DockIt down cleanly, releasing the lock. It
  is killed if it takes more than 15 seconds.
- After a crash, supervise-daemon starts DockIt again, which removes the lock
  the crash left behind (see [Stale locks](#stale-locks)). It gives up after
  more than `respawn_max` crashes in `respawn_period`; Alpine's
  `/etc/rc.conf` sets 5 in 30 minutes.
- Run the other commands as `dockit` too, such as
  `doas -u dockit dockit check /var/lib/dockit`.
- logrotate, once installed (`doas apk add logrotate`), rotates the log
  weekly from crond and keeps four, as its `/etc/logrotate.conf` says. Until
  then the log keeps growing.

To keep the dataset somewhere else, create the directory owned by `dockit`,
with mode 0750, and set `DOCKIT_DATA` in `/etc/conf.d/dockit`. To keep the log
somewhere else, set `error_log` there, and change the path in
`/etc/logrotate.d/dockit` too.

To listen on a port below 1024, add this to `/etc/conf.d/dockit`:

```sh
capabilities="^cap_net_bind_service"
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
