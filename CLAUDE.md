# tenetlint

A Go CLI that lints code against English rules, judged by TypeSafe's jev model.

## Commands

- Setup: `mise install`. Every `go`, `golangci-lint` and `goreleaser` command runs through mise, either as `mise exec -- <cmd>` or with mise activated in the shell.
- Test: `go test -race ./...`
- Lint: `golangci-lint run`
- Release build check: `goreleaser build --snapshot --clean`

## Tests against the live API

Tests that call the real jev API are skipped unless `TYPESAFE_API_KEY` is set. They cost money and need the network, so they are never part of a default run. Never print the key or commit it.

## Comments

A comment says only what the code cannot: why a constraint exists, why the obvious approach was rejected, what an external system does that the code works around. Never what the code does; a name or a smaller function says that.

## Working in this repo

This repo lifts nothing from the global rules: code changes still happen on a branch in a worktree, never in the main checkout, because other sessions share it.
