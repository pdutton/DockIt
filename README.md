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

This locks the dataset and serves the web interface and the REST API. Only one
instance may run against a dataset at a time. *(Not implemented yet.)*

### Other commands

| Command          | Purpose                                                          |
|------------------|------------------------------------------------------------------|
| `dockit unlock`  | Remove a stale lock left by an instance that is no longer running. Shows who holds the lock and asks first; `-yes` skips the question. |
| `dockit check`   | Validate a dataset: file names match IDs, required fields are present, and references between records resolve. Safe to run while DockIt is running. `-q` prints errors only. Exits 1 if there are errors. |
| `dockit upgrade` | Back up a dataset, then migrate it to the current format. *(Not implemented yet.)* |
| `dockit version` | Print the build version and the dataset format it supports.      |

Run `dockit <command> -h` for a command's flags.
