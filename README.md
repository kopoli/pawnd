# Pawnd

A tool of running commands on file change and other triggers.
There are a lot like it and this one is mine.

## Installation

```
$ go get github.com/kopoli/pawnd
```

## Description

The idea is to have a terminal window that displays the current status of
building, running tests, running lint etc.  The window displays the output of
each process in a distinct manner.  At the bottom there would be a summary of
the commands, whether they failed or succeeded.

## Example with animations

![Example run](https://github.com/kopoli/pawnd/raw/master/example-usage/animation.gif)

The `Pawnfile` that is used in the above:

```
[godoc]
init
daemon=godoc -http=:6060

[gochange]
file=**/*.go
changed=gobuild

[gobuild]
init
exec=go build
succeeded=gotest megacheck

[gotest]
exec=go test ./lib/...

[megacheck]
exec=megacheck

[failing]
init
exec="/bin/sh -c 'sleep 2 && echo This fails 1>&2 && false'"
```

### Basic usage

1. Create a `Pawnfile` in the directory.
2. Run `pawnd`.

## Pawnfile reference

- The format is an ini-file.
- The section names are arbitrary names that are referred to by other sections
  and displayed on screen.
- Each of the sections will have exactly one of the following keys:
  - `file`: Is given a recursive globbing pattern that will trigger a command
    when a file matching the pattern is changed.
    - The commands to run is given with the `changed` key. It is
      space-separated list of section names.
  - `exec`: A command that is run. If the command succeeds triggers the
    commands in the `succeeded` key. If fails, triggers the `failed` commands.
	Can have `init` key which will run the command when `pawnd` starts.
  - `daemon`: Similar to the command, but displays a spinner instead of a
    progress bar on the screen.
	Can have `init` key which will run the command when `pawnd` starts.
  - `signal`: When `pawnd` receives a signal, the commands in the list of
    `triggered` key are run.
    - The `sigusr1` and `sigusr2` signals are supported on UNIX. None on
      Windows.
  - `cron`: Run commands in the list of `triggered` with intervals defined in
    cron-like manner. The spec for the cron can be found at:
    https://godoc.org/github.com/robfig/cron

## License

MIT license
