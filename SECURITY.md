# Security Policy

## Reporting a vulnerability

Please report suspected vulnerabilities privately through GitHub's
[security advisories](https://github.com/MiguelFPP/tui-ssh/security/advisories/new)
instead of a public issue.

Include the affected version (`tui-ssh --version`), a description of the issue
and, when possible, a minimal reproduction. You will get an initial reply as soon
as the report is triaged.

## Supported versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | :white_check_mark: |
| < 0.1   | :x:                |

Only the latest release line receives security fixes.

## What tui-ssh reads

- **`~/.ssh/config` (read-only).** It is parsed to list hosts and their literal
  directives. The TUI never writes to it: `E` opens it in `$EDITOR` and the user
  decides what changes.
- **`tags.json` and `forwards.json`** in the XDG configuration directory. They hold
  only tag names, aliases and forward specifications — never keys, passphrases or
  connection content.
- **`$XDG_RUNTIME_DIR/tui-ssh`**: the control sockets, a bounded log per tunnel and
  the tunnel manifest. A socket grants control over the master it belongs to, so
  the directory is created with `0700`.

## What tui-ssh never does

- It never writes, rewrites or reformats `~/.ssh/config`.
- It never reads private keys. Key paths are copied from the configuration to the
  detail panel; the keys themselves are only used by `ssh` itself.
- It never logs credentials. The tunnel log is the stderr of `ssh`, which may
  contain host names and errors, and nothing secret.
- It never builds a shell command line. Every `ssh` invocation uses separate
  arguments and a validated alias: no spaces, no control characters and no leading
  dash.

## Mounting `~/.ssh`: key-equivalent access

`compose.yaml` keeps `~/.ssh` out of the default services. Mounting it gives the
container access to the private keys, which is equivalent to impersonating the user
against every host those keys unlock.

Because of that:

- The mount is **opt-in**: only the `dev-ssh` (`ssh`) and `app-ssh` (`prod-ssh`)
  Compose profiles mount `~/.ssh`, and always **read-only**.
- Those profiles also forward `SSH_AUTH_SOCK` and use `network_mode: host`. The host
  network is required for the tunnels: a port opened inside the container's network
  namespace would not be reachable from the host.
- The default `dev` and `app` services mount nothing, so UI work never needs the
  keys.
- No key material is ever copied into the image, the logs or the state directory.

If you do not need real hosts, do not mount `~/.ssh`.

## Tunnel processes

Each tunnel is an OpenSSH master in the background with its own control socket. The
state is queried with `ssh -O check` and ended with `ssh -O exit`; the TUI never
sends signals to a PID it guessed. A master that dies leaves its entry as
`stopped` instead of a phantom `active` row.

## Handling of untrusted input

- Every alias is validated before it reaches `ssh`: no spaces, no control
  characters, no leading dash. That blocks a value such as `-oProxyCommand=...`
  from being treated as an option.
- Forward specifications are parsed into structured fields (direction, addresses,
  ports) and rendered back into the single argument `-L`/`-R`/`-D` expects.
- `ssh` and the external editor are executed with argument lists, never through a
  shell.
- The configuration, the report, the tunnel logs and the buffers are bounded by
  size and line limits.

## Dependency updates

Dependencies are pinned in `go.mod`/`go.sum` and updated through the review
process. Report a vulnerable transitive dependency the same way as any other
finding.
