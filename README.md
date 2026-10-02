# DockIt

DockIt is a simple to-do list tracker. It tracks tasks across projects (who owns
each one, its state and priority, and the discussion around it) through a web
page and a REST API. Your data is a directory of YAML files, easy to back up,
move, and read from other tools.

DockIt is in early development and licensed under the AGPL ([LICENSE](LICENSE)).

## Run DockIt in a container

Build the image:

```sh
VERSION=$(git describe --tags --always --dirty)
podman build --build-arg VERSION=$VERSION -t dockit:$VERSION .
podman tag dockit:$VERSION dockit:latest
```

Make a host directory for the data, then create the dataset and its first admin
user. Note the one-time password it prints:

```sh
DOCKIT_DIR=/srv/dockit/data
mkdir -p $DOCKIT_DIR
podman run --rm --userns=keep-id:uid=65532,gid=65532 -v $DOCKIT_DIR:/data:Z \
  dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com
```

Start DockIt, then open http://localhost:8080/ and log in:

```sh
podman run --rm -d --name dockit --stop-timeout 15 -p 8080:8080 \
  --userns=keep-id:uid=65532,gid=65532 -v $DOCKIT_DIR:/data:Z dockit
```

Stop DockIt:

```sh
podman stop dockit
```

Check the dataset for errors (safe while DockIt runs):

```sh
podman run --rm --userns=keep-id:uid=65532,gid=65532 -v $DOCKIT_DIR:/data:Z dockit check
```

Back up the dataset (safe while DockIt runs):

```sh
tar -C $DOCKIT_DIR --exclude=./dockit.lock --exclude='.*.tmp' -cf dockit-backup.tar .
```

After building a new image, stop DockIt, copy the dataset, and start DockIt
again with the command above. The new version upgrades the dataset as it
starts, in place, and keeps no copy of its own. Older versions refuse an
upgraded dataset, so the copy is how you go back:

```sh
podman stop dockit
cp -r $DOCKIT_DIR $DOCKIT_DIR.before-upgrade
```

## Install the .deb package

On Debian, Ubuntu and their derivatives, DockIt can run as a systemd service
instead. Install the package from a
[release](https://github.com/pdutton/DockIt/releases), create the dataset as
the `dockit` user, noting the one-time password, and start DockIt:

```sh
sudo apt install ./dockit_2.2.7_amd64.deb
sudo -u dockit dockit init -admin pdutton -name "Peter Dutton" -email peter@example.com /var/lib/dockit
sudo systemctl start dockit
```

Its settings are in `/etc/dockit/dockit.conf`. To upgrade, and for the rest,
see [Running as a systemd service](OPERATIONS.md#running-as-a-systemd-service).

## More

- [OPERATIONS.md](OPERATIONS.md): settings, named volumes, running as a
  systemd or OpenRC service, restore, stale locks, and caring for your data.
- [TESTING.md](TESTING.md): running DockIt without a container, developing
  it, and building the .deb packages.
- [DESIGN.md](DESIGN.md): architecture, dataset format, and the REST API.
