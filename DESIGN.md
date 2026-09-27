# DockIt Design

These are specific decisions on the DockIt architecture and design.

> **Status: sketch.** Items marked **Decided** have been agreed; everything else is a
> proposal until reviewed.

## Technologies

DockIt shall be implemented in Go.

The dataset shall be a collection of YAML files under a directory tree.

DockIt will run in a container with a mounted volume that contains the dataset.  This container will
present the Web and REST interfaces.  Only one DockIt instance will run against a given dataset at a time,
but in theory multiple instances could act on a dataset at different times.  Consider the presence of a
file in the dataset directory as a lock file to prevent multiple access, and require manual intervention
if a stale lock file is encountered.  Do not rely on any specific file locking implementation as the
filesystem and host operating system should not be assumed.

Proposed supporting choices:

- A single static binary, `dockit`, with subcommands (see [Command Line](#command-line)).  The container
  image is that binary plus nothing else (distroless or scratch).  Running locally means running the same
  binary, with or without a container.
- **Decided:** Web UI templates and static assets are compiled into the binary (`embed`), so there is no
  separate asset deployment and no front-end build step.  There is no on-disk override.  Code reads
  assets only through Go's `fs.FS` interface, so moving them into the image as files later means
  swapping the `embed.FS` for a directory, not a redesign.
- TLS is terminated by a reverse proxy in cloud deployments; DockIt can optionally serve TLS itself for
  local use.
- Configuration is by flags or environment variables only (`DOCKIT_DATA`, `DOCKIT_LISTEN`,
  `DOCKIT_BASE_URL`, ...).  Configuration is not stored in the dataset, because it describes the install,
  not the data.

## Architecture

```
            browser                    scripts / AI / apps
               |                              |
        +------v-------+              +-------v-------+
        |  Web UI      |              |  REST API     |
        | (HTML forms) |              |  (JSON, /api) |
        +------+-------+              +-------+-------+
               |   authn (session)            |   authn (API token)
               +--------------+---------------+
                              |
                     +--------v---------+
                     |  Service layer   |  permissions, validation,
                     |                  |  version checks, ID assignment
                     +--------+---------+
                              |
                     +--------v---------+
                     |  In-memory index |  runtime cache of the whole dataset
                     +--------+---------+
                              |
                     +--------v---------+
                     |  Dataset store   |  YAML read/write, atomic writes, lock
                     +--------+---------+
                              |
                        dataset volume
```

- **The Web UI and the REST API are peers over the same service layer.**  The Web UI does not call the
  REST API over HTTP.  All rules (permissions, validation, conflict detection) live in the service layer,
  so the two interfaces cannot drift apart.
- **The in-memory index is the read path.**  At startup DockIt reads the entire dataset into memory.
  Every read is served from memory; every write goes to disk first and updates memory only after the
  file is safely written.  This is the "cached information about the dataset" that the requirements
  allow as runtime state.  A to-do dataset is small enough that this is cheap, and it makes listing and
  filtering trivial without a database.
- **One writer at a time.**  Writes are serialized through a single mutex in the service layer.  Write
  volume is tiny, and this removes any need for finer-grained locking.
- Nothing in the implementation outlives the process except what is written to the dataset.  Sessions
  and the index are rebuilt or discarded on restart.

**Decided — Web UI style:** server-rendered HTML with Go templates and plain forms, plus a little
progressive JavaScript at most.  No build toolchain, trivially embeddable, and XSS handling stays in one
place.  (Rejected: a single-page app on the REST API, which adds a JavaScript toolchain and duplicates
validation in the browser.)

## DataSet

Each project will be a subdirectory under the dataset root.  There will be a YAML file in this directory that
defines the project's properties.

Each task shall be kept in a single YAML file. This file will be under its project directory tree.

Users information will also be in the dataset in YAML format; however, related authentication information will
either need to be kept separately or encrypted.  See [Authentication](#authentication).

The YAML file names will be the item's (project, task, etc) ID with the yaml extension.
Individual files will be written in a manner that protects against corruption.

### Layout

```
<dataset root>/
├── dockit.yaml                 # dataset metadata (format version, dataset id)
├── dockit.lock                 # present only while an instance is running
├── projects/
│   └── WEB/
│       ├── WEB.yaml            # project WEB
│       └── tasks/
│           ├── WEB-1.yaml      # task WEB-1, including its comments
│           └── WEB-2.yaml
├── users/
│   └── pdutton.yaml            # user profile: no secrets
└── auth/
    └── pdutton.yaml            # password hash and API token hashes
```

- Projects live under `projects/` rather than directly under the root, so a project ID can never collide
  with `users`, `auth` or any directory added later.
- Comments live inside their task's file, keeping "one task, one file".
- There is no counter file.  The next task number for a project is `max(existing) + 1`, computed from the
  index.  Tasks are never deleted, so numbers are never reused.
- File name must equal the ID inside the file.  `dockit check` reports any mismatch.

### Dataset metadata and versioning

`dockit.yaml`:

```yaml
format: 1                       # dataset format version, an integer
dataset_id: 3f8c2a1e-...        # random UUID, identifies the dataset across copies
created: 2026-09-26T21:54:43Z
```

- `format` is incremented for any change to what may appear in the dataset, including a new optional
  field or a new enumeration value.  An older install therefore never loads data it does not fully
  understand, and so can never drop an unknown field when it rewrites a file.  For purely additive
  changes, `dockit upgrade` only has to bump the number.
- On startup, DockIt compares `format` to the range it supports:
  - equal: run.
  - older but upgradeable: refuse to serve and tell the operator to run `dockit upgrade`, which rewrites
    the dataset to the current format in place, after writing a full copy to a sibling backup directory.
  - newer than supported: refuse to run.  This is what "portable between installs of sufficient version"
    means in practice.
- Upgrades are never performed implicitly by `serve`, so a dataset is never silently made unreadable to
  an older install.

### Record formats

All timestamps are RFC 3339 in UTC with a `Z` suffix.  Enumerated values are stored as stable lowercase
identifiers; display strings live in the implementation.  Fields are always written in a fixed order so
that files diff cleanly (a dataset kept in git is a reasonable backup strategy).

Project, `projects/WEB/WEB.yaml`:

```yaml
id: WEB
version: 4
name: Website Redesign
state: active
description: |
  Markdown text.
urls:                           # optional; map of URL type -> ordered list
  code:
    - https://github.com/example/site       # first entry is the primary for its type
    - https://github.com/example/site-infra
  web:
    - https://example.com
created: 2026-09-26T21:54:43Z
modified: 2026-09-27T08:10:00Z
```

User, `users/pdutton.yaml`:

```yaml
id: pdutton
version: 2
name: Peter Dutton
email: peter@example.com
role: admin
active: true
created: 2026-09-26T21:54:43Z
modified: 2026-09-26T21:54:43Z
```

Task, `projects/WEB/tasks/WEB-12.yaml`:

```yaml
id: WEB-12
version: 7
title: Replace The Header Logo
description: |
  Markdown text.
creator: pdutton
owner: pdutton
state: complete
substate: done
priority: 3
created: 2026-09-26T21:54:43Z
modified: 2026-09-27T09:00:00Z
last_comment_id: 1              # optional; highest comment id ever used
comments:
  - id: 1
    version: 1
    commenter: pdutton
    created: 2026-09-26T22:00:00Z
    modified: 2026-09-26T22:00:00Z
    text: |
      Markdown text.
```

Notes:

- `version` is a per-record integer incremented on every write; it drives
  [concurrency control](#concurrent-edits).  Comments carry their own `version`, so comments and task edits never conflict.
- `created`/`modified` on projects and users are not required, but they cost nothing and help operators.
- Comment `id` is a per-task sequence, never reused, so the REST API can address a comment.  A deleted
  comment is removed from the file.  Because deleting the newest comment would otherwise let its id be
  reused, the task keeps `last_comment_id`, and the next id is one more than the larger of that and the
  highest id present.
- Project `urls` is keyed by URL type.  Order within each list is significant, so the first URL of a
  type is its primary.  Order between types is not significant; keys are written in the enumeration's
  built-in order and empty lists are omitted.  The REST API uses the same shape.
- The task's project is implied by its ID prefix and its directory; it is not stored separately.

### Identifiers

| Entity  | Format (proposed)                        | Example   |
|---------|------------------------------------------|-----------|
| Project | 2–10 chars, `A-Z` then `A-Z0-9`          | `WEB`     |
| Task    | `<project id>-<n>`, `n` from 1 per project | `WEB-12`  |
| User    | 2–32 chars, `a-z` then `a-z0-9._-`; also the login name; immutable | `pdutton` |
| Comment | integer, per task                         | `1`       |

IDs are used as file names, and the dataset may sit on a case-insensitive filesystem (Windows, macOS).
Fixing the case of each ID type means `web` and `WEB` can never both exist.

**Decided — project ID case:** uppercase only, which reads well in task IDs like `WEB-12`.  Mixed case
is ruled out by case-insensitive filesystems.

### Enumerations

Built into the implementation as a table of stable id → display string:

| Kind          | Stored ids                                             |
|---------------|--------------------------------------------------------|
| Project state | `planned`, `active`, `inactive`, `dormant`, `complete` |
| Task state    | `new`, `in_progress`, `deferred`, `paused`, `complete` |
| Substate      | `complete`: `done`, `rejected`                         |
| URL type      | `code`, `doc`, `web` (displayed as Code, Documentation, Website) |
| Role          | see [Roles and Permissions](#roles-and-permissions)    |

- A task's `substate` is required if and only if its state has substates.  On a transition out of such a
  state, the substate is cleared.
- Unknown ids should never be encountered, because a dataset using newer values has a newer `format` and
  will not be loaded.  To play it safe, an unknown id found anyway is not fatal: it is logged as a
  warning, preserved on disk, returned as-is by the REST API, and shown in the Web UI as the raw id with
  a warning marker.  Clients can never set an unknown id; validation rejects it.

### Atomic writes

Every file write is:

1. Write the full new content to `.<name>.tmp` in the same directory.
2. `fsync` the temp file.
3. Rename it over the target (atomic on POSIX; `MoveFileEx` with replace on Windows).
4. `fsync` the directory where the platform supports it.

On Windows, the rename fails with a sharing violation if another process (an outside reader, a backup
tool, antivirus) has the target open without `FILE_SHARE_DELETE`.  The store retries the rename briefly
with backoff, then fails the write cleanly and leaves the old file intact.

A reader, or a crash, sees either the old file or the new one, never a partial one.  Leftover `.tmp`
files are deleted at startup, which is safe because the lock is held.

### Lock file

- On startup, `serve` creates `dockit.lock` with create-exclusive semantics (`O_CREATE|O_EXCL`), which is
  an atomic operation on local filesystems and is not an OS locking API.  It writes the host name,
  process ID, start time and a random instance ID into it.
- If the file already exists, DockIt refuses to start and prints its contents.  The operator either
  stops the other instance or confirms it is gone and runs `dockit unlock`.
- On clean shutdown the lock file is removed.
- Before each write, the store confirms the lock file still holds its own instance ID.  This is a cheap
  guard against an operator unlocking a dataset that is actually still in use.
- Caveat: create-exclusive is not reliable on some network filesystems.  Document that the dataset volume
  should be local or a filesystem with reliable exclusive create.

### Backup, restore and outside readers

- **Backup:** copy the dataset directory.  Because every file is written atomically, a copy taken while
  DockIt runs contains only complete files, and no invariant spans files except "a referenced user exists",
  which holds because users are never deleted.  Exclude `dockit.lock` from the copy.
- **Restore:** stop DockIt, replace the directory, start DockIt.  The index is rebuilt from disk.
- **Outside readers** may read the dataset at any time.  **Outside writers** are not supported while
  DockIt is running, because the index would go stale.  Anyone editing files by hand should stop DockIt,
  edit, run `dockit check`, and restart.
- `dockit check` validates a dataset offline: YAML parses, required fields, ID formats, file names match
  IDs, references resolve, substate rules hold.

## Authentication

Your starting ideas were host-based users, pluggable auth, or no auth for early development.  Proposal:

- **Pluggable, behind an interface.**  An `Authenticator` takes a request and returns a user ID or
  failure.  Version 1 ships local passwords (Web) and API tokens (REST).  An OIDC or trusted-proxy-header
  authenticator can be added later without touching the dataset format.
- **Host-based users: not recommended.**  Container host accounts differ between installs, which breaks
  dataset portability.
- **No-auth development mode:** a `--dev-insecure-user=<id>` flag that treats every request as that
  user.  It refuses to start unless bound to localhost.

### Where secrets live

`users/` holds only profile data.  Secrets live in `auth/<user id>.yaml`:

```yaml
user: pdutton
password: $argon2id$v=19$m=65536,t=3,p=4$...
must_change_password: true      # optional; set on a one-time password, cleared when changed
tokens:
  - id: tok_7Kq2
    name: laptop script
    hash: sha256:...            # tokens are high-entropy random, so a fast hash is enough
    created: 2026-09-26T21:54:43Z
```

Nothing stored is reversible: passwords use argon2id, and API tokens are 256-bit random values stored only
as SHA-256 hashes and shown once at creation.

**Decided — where authentication data lives:** in the dataset under `auth/`, hashed as above.  One
directory to back up and move; hashes are one-way, so an outside reader of the dataset learns nothing
usable.  Operators can exclude `auth/` when sharing a dataset for reading.  (Rejected: a second volume,
which makes restore and moves two-part; and encryption with an environment key, which adds key management
and a lock-out risk for little gain over one-way hashes.)

### Sessions and tokens

- **Web:** username and password form; on success a random session ID in an `HttpOnly`, `SameSite=Lax`
  cookie (`Secure` when served over HTTPS).  Sessions are held in memory only, so a restart logs everyone
  out, which is acceptable.  State-changing forms carry a CSRF token.
- **REST:** `Authorization: Bearer <token>`.  Users create and revoke their own tokens in the Web UI.
  Tokens carry the user's role; there are no per-token scopes in version 1.
- Deactivated users cannot log in, and their tokens stop working immediately.
- Failed logins are rate-limited per user ID and per client address, in memory.
- Passwords are 8 to 256 characters.  There are no composition rules.
- An admin can reset a user's password, which gives the user a new one-time password that must be changed
  at next login.  This is the recovery path for a forgotten password.

### Bootstrap

`dockit init <dir>` fails unless `<dir>` is missing or empty.  It creates an empty dataset
(`dockit.yaml`, directories) and one admin user, and prints a one-time password that must be changed at first login.  `serve` refuses to start on a directory that has
no `dockit.yaml`, so a mistyped volume path is not silently turned into a new dataset.

## Roles and Permissions

Roles are built into the implementation.  Each user has exactly one role.  **Decided:** three roles, `viewer`, `member`
and `admin`:

| Action                                   | viewer | member | admin |
|------------------------------------------|:------:|:------:|:-----:|
| Read projects, tasks, comments, users    | ✓      | ✓      | ✓     |
| Create and edit tasks                    |        | ✓      | ✓     |
| Add comments                             |        | ✓      | ✓     |
| Edit or delete own comments              |        | ✓      | ✓     |
| Manage own password and API tokens       | ✓      | ✓      | ✓     |
| Create and edit projects                 |        |        | ✓     |
| Create, edit, deactivate users; set role |        |        | ✓     |

- Permissions are global, not per project.  Per-project roles can be added later by storing a role map on
  the project, without a format change for existing data.
- "Edit or delete own comments" follows the requirement that only the commenter may edit or delete.
  Admins get no override.
- A deactivated user remains the owner and commenter of existing records.  They cannot be chosen as the
  owner in a new assignment.
- An admin cannot deactivate or demote the last active admin.

## Concurrent Edits

The requirement is that the second of two concurrent edits is rejected.  This uses optimistic concurrency
on each record's `version`:

- Every read returns the version: the REST API as an `ETag` header (`"7"`), the Web UI as a hidden form
  field.
- Every update must send the version it was based on: the REST API in `If-Match`, the Web UI in the
  form.  Inside the write mutex, the service compares it with the current version; on mismatch the write
  is rejected.
  - REST: `412 Precondition Failed`, with the current record in the body.  An update with no `If-Match`
    gets `428 Precondition Required`.
  - Web: the form is shown again with the user's input kept, the current values alongside, and a
    conflict message.
- Creates need no version.

**Decided — comments do not conflict with task edits.**  The task's `version` covers only the task's
own fields, and each comment has its own `version`.  Adding, editing or deleting a comment does not change
the task's `version`, so it never rejects someone's concurrent edit of the title, and vice versa.  Any
change, including a comment, still updates the task's `modified` timestamp, per the requirements.
(Rejected: one version per task file, where adding a comment invalidates every open edit form for that
task.)

## Web UI

- Server-rendered pages: project list, project detail with its task
  list, task detail with comments and edit form, user admin, and "my account" for password and tokens.
- Task lists can be filtered by state, owner and priority and sorted by priority or modified time.  This
  is cheap given the in-memory index.
- Go `html/template` escapes all plain-text fields automatically.
- Markdown fields (project and task descriptions, comments) are rendered with a CommonMark renderer, then
  passed through an allow-list HTML sanitizer, with raw HTML disabled in the renderer as well.
- Project URLs are accepted only with `http` or `https` schemes, closing the `javascript:` URL hole.
- Enumerated fields are always shown via the built-in display strings, never as raw stored values
  (except unknown ids, which are shown raw with a warning, as noted above).
- A strict `Content-Security-Policy` header (no inline scripts) as defence in depth.
- Timestamps are sent in UTC and converted to local time in the browser.

## REST API

JSON over HTTPS, versioned in the path.  Resources mirror the dataset:

```
GET    /api/v1/projects                         list
POST   /api/v1/projects                         create (admin)
GET    /api/v1/projects/{pid}
PATCH  /api/v1/projects/{pid}                   update (admin, If-Match)

GET    /api/v1/projects/{pid}/tasks             list, ?state=&owner=&priority=
POST   /api/v1/projects/{pid}/tasks             create; server assigns the ID
GET    /api/v1/tasks/{tid}
PATCH  /api/v1/tasks/{tid}                      update (If-Match)

POST   /api/v1/tasks/{tid}/comments             add
PATCH  /api/v1/tasks/{tid}/comments/{cid}       edit own (If-Match)
DELETE /api/v1/tasks/{tid}/comments/{cid}       delete own (If-Match)

GET    /api/v1/users                            list
POST   /api/v1/users                            create (admin)
GET    /api/v1/users/{uid}
PATCH  /api/v1/users/{uid}                      update, deactivate (admin, If-Match)

GET    /api/v1/me                               current user
GET    /api/v1/me/tokens                        list own tokens (metadata only)
POST   /api/v1/me/tokens                        create; token returned once
DELETE /api/v1/me/tokens/{id}                   revoke

GET    /api/v1/enums                            states, substates, roles, display strings
```

- `PATCH` bodies are JSON Merge Patch (RFC 7396).
- JSON field names match the YAML field names, so the API and the files describe records the same way.
- Enumerated values are sent as stable ids; `/enums` supplies display strings for clients that want them.
- Errors use one shape: `{"error": {"code": "...", "message": "...", "field": "..."}}`.
- Markdown fields are returned as source text.  Clients that render it are responsible for sanitizing.

## Validation

Enforced in the service layer, identically for both interfaces:

| Field                     | Rule (proposed)                                    |
|---------------------------|----------------------------------------------------|
| Project name, task title  | required, 1–200 characters, no control characters  |
| User name                 | required, 1–100 characters                         |
| Email                     | required, syntactically valid; need not be unique  |
| Markdown fields           | optional, up to 64 KiB                             |
| URLs                      | absolute `http`/`https`, up to 2 KiB each; key must be a known URL type |
| Priority                  | integer 1–5, default 3                             |
| Owner                     | an existing active user                            |

## Command Line

| Command          | Purpose                                                      |
|------------------|--------------------------------------------------------------|
| `dockit serve`   | Take the lock, load the dataset, serve Web and REST          |
| `dockit init`    | Create a new dataset and its first admin; empty dir only     |
| `dockit check`   | Validate a dataset offline                                   |
| `dockit upgrade` | Back up, then migrate a dataset to the current format        |
| `dockit unlock`  | Remove a stale lock file after the operator confirms         |
| `dockit version` | Print the build version and supported dataset formats        |

Commands other than `serve` that modify the dataset (`init`, `upgrade`) take the same lock.

## Miscellaneous Rules

All datetimes must be stored in UTC in a machine friendly timestamp format (RFC 3339, `Z` suffix).
Timezone conversion and formatting is the job of the web interface / browser or the REST consumer.

## Decision Log

| # | Decision                               | Outcome                                  |
|---|----------------------------------------|------------------------------------------|
| 1 | Web UI style                           | Server-rendered HTML                     |
| 2 | Where authentication data lives        | `auth/` in the dataset, hashed           |
| 3 | Do comments conflict with task edits?  | No: separate versions                    |
| 4 | Role set                               | viewer / member / admin                  |
| 5 | Project ID case                        | Uppercase                                |
| 6 | Web assets                             | Embedded in the binary; no override      |
