---
name: dockit
description: Read and update tasks in a DockIt issue tracker through its REST API, using the bundled dockit-api script. Use when the user mentions a DockIt task ID (an uppercase project ID of 2–10 characters, a dash and a number, such as DOCKIT-34 or WEB-12), or asks about the tickets, tasks or issues they track in DockIt, such as what is assigned to them or what a task says, or asks to create, start, update, complete, comment on or link tasks.
---

# DockIt

DockIt is a to-do list tracker. Projects hold tasks, and each task has an
owner, a state, a priority, comments, and links to other tasks. You work with
a DockIt server through its REST API, using `dockit-api`, a script in this
skill's directory. Run it by its full path; the examples below leave the path
out.

## Setup

`dockit-api` needs bash, curl and jq, and two environment variables:

- `DOCKIT_URL`: the server's address, such as `http://localhost:8080`.
- `DOCKIT_TOKEN`: an API token. It acts as the user who created it, with that
  user's role.

Both are set outside this skill, for example under `env` in
`~/.claude/settings.json`. If either is missing, `dockit-api` says so. Tell
the user, rather than guessing an address or hunting for a token. Never print
the token or put it in a comment, a file or a URL.

Run `dockit-api me` to see which user you act as, and `dockit-api` with no
arguments for the full usage.

## Finding tasks

```
dockit-api mine                          # your open tasks, in every project
dockit-api tasks WEB                     # open tasks in project WEB
dockit-api tasks WEB --all --sort priority
dockit-api tasks WEB --state deferred --owner pdutton
dockit-api projects
```

`tasks` and `mine` print one line per task, with tab-separated columns: ID,
state (with `/substate`), priority, type, owner and title. "Open" means
anything but `complete` and `deferred`, as in the web interface; pass
`--state` (repeatable) or `--all` to change that. `--json` prints the full
records instead, including every description and comment, which can be large.
There is no text search, so to find a task by subject, list the project with
`--all` and read the titles.

## Reading a task

`dockit-api task WEB-12` prints the task as JSON, with its comments and an
added `links` list. Read a task before you change it.

| Field | Meaning |
|---|---|
| `id` | `<project>-<n>`, assigned by the server |
| `title` | Required, up to 200 characters |
| `type` | `task` (the default), `bugfix`, `enhancement`, `feature`, `documentation` or `research` |
| `description` | Markdown; optional |
| `creator` | Set by the server |
| `owner` | An active user's ID; defaults to the creator |
| `state`, `substate` | See [States](#states) |
| `priority` | 1 (highest) to 5; the default is 3 |
| `found_in`, `resolved_in` | Optional free-form versions: where a problem appeared, and the release that resolved it |
| `urls` | URLs by type, each a list whose first entry is the primary. Tasks have one type, `pr` (pull requests) |
| `created`, `modified` | RFC 3339 times in UTC |
| `version` | Goes up by one with each edit of the task's own fields. Comments and links do not change it |
| `comments` | Each has `id`, `version`, `commenter`, `created`, `modified` and `text` (Markdown) |

`dockit-api enums` lists the states, substates, types, URL types, link
relations and transitions the server knows. Where it disagrees with this
file, it is right.

## Changing tasks

```
dockit-api create WEB title="Fix the header" type=bugfix priority=2 description=@desc.md
dockit-api transition WEB-12 start
dockit-api update WEB-12 state=complete substate=rejected
dockit-api update WEB-12 resolved_in=v1.2.0 found_in=
dockit-api add-url WEB-12 pr https://github.com/example/site/pull/7
dockit-api link WEB-12 blocked_by WEB-7
```

- `FIELD=VALUE` sets a field. `FIELD=@FILE` reads the value from a file, and
  `@-` from stdin. `FIELD=` clears an optional field.
- Write Markdown through a quoted heredoc, so backticks and `$` reach DockIt
  unchanged:

  ```
  dockit-api comment WEB-12 <<'EOF'
  Fixed in `header.go`; see the PR.
  EOF
  ```

  `update WEB-12 description=@- <<'EOF'` works the same way.
- Each write prints the record it wrote. Tasks are printed without their
  comments.
- `urls` holds a list per type, and setting a type replaces its whole list.
  Use `add-url` and `remove-url`, which keep the rest of the list.
- `edit-comment` and `delete-comment` work only on your own comments.
- For anything else, such as projects and users, `dockit-api api METHOD PATH
  [BODY]` sends any request under `/api/v1`, adding `If-Match` to a `PATCH`
  or `DELETE`. The endpoints are listed in DockIt's DESIGN.md, under "REST
  API".

### Editing at the same time as someone else

Every task, comment, project and user has a `version`, and the server refuses
a write based on an old one with `412`. By default `dockit-api` reads the
version just before it writes, which only covers that moment. When a change
depends on what you read earlier, such as rewriting a description or acting
on a state you reported to the user, pass the `version` you read:

```
dockit-api update WEB-12 --version 7 description=@desc.md
```

If someone changed the task since, `dockit-api` fails and prints the current
record. Read it, then redo the change against what is there now, or ask the
user. Don't simply retry without `--version`. Comments and links never
conflict with task edits.

## States

| State | Meaning |
|---|---|
| `new` | Not started |
| `in_progress` | Being worked on |
| `paused` | Started, then set aside |
| `deferred` | Put off without starting |
| `complete` | Finished. Needs a substate: `done`, `rejected` or `duplicate` |

Leaving `complete` clears the substate. `update` can set any state. The
transitions are shortcuts for the common changes, and each one fails with
`409 wrong_state` unless the task is in its starting state, which guards
against acting on a stale view:

| Transition | From | To |
|---|---|---|
| `start` | `new` | `in_progress` |
| `defer` | `new` | `deferred` |
| `complete` | `in_progress` | `complete`, substate `done` |
| `pause` | `in_progress` | `paused` |
| `restart` | `paused` | `in_progress` |

## Links

A link is named from the side of the first task: `link WEB-12 blocked_by
WEB-7` records that WEB-7 blocks WEB-12, and either task can remove it. The
relations are `blocks` and `blocked_by`, `depends_on` and `dependency_of`,
`duplicates` and `duplicated_by`, `related`, and `conflicts`. Links may cross
projects. They are information only, and never stop a state change. Adding a
link that already exists does nothing.

## Permissions

A `viewer` can read. A `member` can also create and edit tasks, add
comments, add and remove links, and edit or delete their own comments. An
`admin` can also create and edit projects and users. A `403` means your
user's role does not allow the change. Tell the user rather than looking for
another way.

## Ground rules

- Change only what you were asked to. Leave a task's owner, priority, title
  and type alone unless asked. Record progress, findings and decisions as
  comments, rather than rewriting the description.
- Before creating a task, check that it isn't there already.
- Never edit DockIt's data files directly, even if you can see them. A
  running DockIt does not see the change, and it may overwrite it. Use the
  API.
- Times are in UTC. Convert them for the user when it matters.

## When something fails

`dockit-api` exits 0 on success, 1 when the server refuses a request or
cannot be reached, and 2 for a usage or setup problem. Errors go to stderr as
`dockit-api: <status> <code>: <message>`, naming the field when there is one.

| Status | Meaning |
|---|---|
| cannot reach | DockIt is not running, or `DOCKIT_URL` is wrong. Tell the user |
| `401` | The token is missing, wrong or revoked. Tell the user |
| `403` | Your role does not allow it |
| `404` | No such task, comment, project or user. Check the ID's case: project IDs are uppercase |
| `409 wrong_state` | The task is not in the transition's starting state. Read it again |
| `412 conflict` | Changed since the version you sent. See [above](#editing-at-the-same-time-as-someone-else) |
| `422 invalid` | A field failed validation. The message says which and why |
| `429` | Too many failed token attempts from this address. Wait a minute |
