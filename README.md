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
  - `file`
  - `signal`
  - `cron`
  - `exec`
  - `daemon`
  - `script`.

- The keys trigger sections depending on their rules. The `exec`, `script` and
  `daemon` keys run commands as well.

### Triggers

#### file

The value is recursive globbing pattern that will trigger when a file matching
the pattern changes.

Globbing patterns are handled by https://github.com/mattn/go-zglob

Triggered sections are listed in the `changed` list.

Can have optional key `hysteresis` which will delay triggering until the given
timeout has run out. The timeout is parsed by https://godoc.org/time#ParseDuration

Minimal example:

```
[change]
file=README
hysteresis=5s
changed=checksum stat

[checksum]
exec=md5sum README

[stat]
exec=stat README
```

#### cron

The value is a cron like pattern that will trigger when said time is reached.

Cron functionality is handled by https://godoc.org/github.com/robfig/cron

Triggered sections are listed in the `triggered` list.

Minimal example:
```
[daily]
cron=@daily
triggered=show-date

[show-date]
exec=date
```

#### signal

The value is signal name. Either `sigusr1` or `sigusr2`. This is only
supported on UNIX.

Triggered sections are listed in the `triggered` list.

Minimal example:
```
[sigusr1]
signal=sigusr1
triggered=clean

[clean]
exec=make clean
```

##### exec, script and daemon

These triggers will run commands in addition to triggering additional sections.

The `exec` and `script` are synonymous. They both run a POSIX compatible shell
scripts.

The difference between `exec` and `daemon` is that `exec` is expected to stop
and `daemon` is supposed to run indefinitely.

These sections can have the `init` key. It means the sections will run on
start. The `init` key has no value.

If the command succeeds it will trigger the sections listed in the `succeeded`
list. If the command fails, then `failed` are triggered.

Can have optional key `cooldown` which will delay running the same command
again before the timeout has run out.

`exec` have optional key `timeout`, which terminate the command after the timeout has run out.

The timeouts are parsed by https://godoc.org/time#ParseDuration

Can have an optional key `visible` which is a boolean key of values `true` and
`false`. If this is `false` the command is not visible at the bottom of the
`pawnd` output. By default `exec` and `daemon` commands are visible.

Example:

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
succeeded=gotest

[gotest]
exec=go test ./lib/...
```

## License

MIT license
