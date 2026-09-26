# DockIt Design

These are specific decisions on the DockIt architecture and design.

## Technologies

DockIt shall be implemented in go.

The dataset shall be a collection of YAML files under a directory tree.

DockIt will run in a container with a mounted volume that contains the dataset.  This container will
present the Web and REST interfaces.  Only one DockIt instance will run against a given dataset at a time,
but in theory multiple instances could act on a dataset at different times.  Consider the presence of a
file in the dataset directory as a lock file to prevent multiple access, and require manual intervention
if a stale lock file is encountered.  Do not rely on any specific file locking implementation as the
filesystem and host operating system should not be assumed.

## DataSet

Each project will be a subdirectory under the dataset root.  There will be a YAML file in this directory that
defines the project's properties.  

Each task shall be kept in a single YAML file. This file will be under its project directory tree.

Users information will also be in the dataset in yaml format; however, related authentication information will
either need to be kept seperately or encrypted.  Consider mapping users to host based users, or allow
pluggalble user auth?  Perhaps simple users with no auth for early development?  TBD.

The YAML file names will be the item's (project, task, etc) ID with the yaml extension
Individual files will be written in a manner that protects against corruption.

## Miscellaneous Rules

All datetimes must be stored in UTC in a machine friendly timestamp format.
Timezone conversion and formatting is the job ov the interface.
