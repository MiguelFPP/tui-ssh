# Contributing to tui-ssh

## Environment

Everything runs inside Docker Compose. Never run `go`, `gofmt`, `go test` or
`go mod` on the host.

```bash
make dev   # TUI with hot reload (needs a TTY)
make build # dev and runtime images
```

For work that needs real hosts, use `make dev-ssh`, which mounts `~/.ssh`
read-only. See [`SECURITY.md`](SECURITY.md).

## Workflow

1. Branch from `develop`.
2. Keep the change coherent: one commit, one intent.
3. Run the tests of the packages you touched during the phase.
4. Open a PR against `develop`. CI (`make test` + `make vet`) is the merge gate.

Commit messages are in English and use a conventional prefix (`feat:`, `fix:`,
`refactor:`, `test:`, `docs:`, `chore:`, `ci:`, `style:`).

## Tests by phase

During the phases, run **only** the packages the phase touches:

```bash
make test-pkg PKG='./internal/<pkg>/...'
make test-run PKG='./internal/<pkg>/...' RUN='TestName'
make vet-pkg PKG='./internal/<pkg>/...'
make test-integration PKG='./internal/<pkg>/...'
make bench-pkg PKG='./internal/<pkg>/...'
```

`make test`, `make vet` and `go test ./...` are reserved for the final phase and CI.
The rationale lives in [`.agents/testing.md`](.agents/testing.md).

## Timers in tests

`tea.Tick` blocks until its deadline, so a test that runs a real tick sleeps for
the whole interval. Inject `Deps.Expire` (a no-op in tests) and call
`DisableTicker()` on the tunnel panel. If a test needs the effect of a tick, send
the message directly.

## Architecture boundaries

Before adding code, read [`AGENTS.md`](AGENTS.md) and
[`.agents/architecture.md`](.agents/architecture.md). The short version:

- `internal/domain/` and `internal/ports/` are pure.
- `internal/adapters/` implements I/O; `internal/tui/` never imports a concrete
  adapter.
- `Update` and `View` never perform I/O; every external operation is a `tea.Cmd`
  with a `context.Context` returning a typed message.
- `ssh` runs through `os/exec` with separate arguments and no shell.

## Adding a dependency

1. Add it to `go.mod` with its exact version.
2. Run `make tidy` to regenerate `go.sum`.
3. Run `make build` to rebuild the images.

Justify new dependencies in the PR: the project deliberately ships only Bubble
Tea, Lip Gloss and Bubbles.
