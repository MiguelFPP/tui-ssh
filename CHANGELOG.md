# Changelog

All notable changes to this project are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0]

### Added

- Host list over `~/.ssh/config` with `Include` support, cycle detection and a
  bounded report of what was read.
- Incremental filter by alias, tag or `HostName`, plus a tag filter cycled with
  `[`/`]`.
- Tags stored in `tags.json` under XDG, editable from the TUI; the ssh
  configuration stays read-only.
- Effective host resolution through `ssh -G -F`, with a per-alias session cache and
  an approximate fallback flagged in the UI.
- Interactive SSH sessions that suspend the TUI and hand the terminal to the native
  binary.
- Local, remote and dynamic port forwarding as detached OpenSSH masters with a
  control socket each: `-O check` for the state, `-O exit` for a clean stop.
- Tunnel manifest for re-adopting the masters a previous session left running, and
  a quit confirmation that asks whether to stop them.
- Ephemeral port detection from the master log (`Allocated port N`).
- Config tab with the files read, the host count, the parser warnings and the
  numbered raw text; `E` opens it in `$EDITOR`.
- Four themes, configurable key overrides and the `--list-themes`,
  `--list-keys`, `--init-config`, `--print-config` and `--version` flags.
- Static binaries (`CGO_ENABLED=0`) for linux/darwin/windows on amd64 and arm64.
