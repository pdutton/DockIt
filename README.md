# DockIt

DockIt is a simple to-do list tracker. It tracks tasks across projects (who owns
each one, its state and priority, and the discussion around it) through a web
page and a REST API. Your data is a directory of YAML files, easy to back up,
move, and read from other tools.

DockIt is in early development and licensed under the AGPL ([LICENSE](LICENSE)).

## Run DockIt in a container

Build the image:

```sh
podman build --build-arg VERSION=$(git describe --tags --always --dirty) -t dockit .
```

Make a host directory for the data:

```sh
DOCKIT_DIR=/srv/dockit/data
mkdir -p $DOCKIT_DIR
```

Create the dataset and its first admin user. Note the one-time password it prints:

```sh
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

After building a new image, stop DockIt, upgrade the dataset, and start DockIt again:

```sh
mkdir -p $DOCKIT_DIR.backups
podman run --rm --userns=keep-id:uid=65532,gid=65532 -v $DOCKIT_DIR:/data:Z \
  -v $DOCKIT_DIR.backups:/backup:Z dockit upgrade -backup /backup
```

## More

- [OPERATIONS.md](OPERATIONS.md): settings, named volumes, restore, stale
  locks, and caring for your data.
- [TESTING.md](TESTING.md): running DockIt without a container, and
  developing it.
- [DESIGN.md](DESIGN.md): architecture, dataset format, and the REST API.
