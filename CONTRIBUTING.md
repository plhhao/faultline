# Contributing to Faultline

Thanks for considering a contribution.

## Before opening a change

- Read the root [README](README.md), the relevant document in `docs/`, and
  `specific.md` for the supported scope.
- Discuss a substantial feature or a protocol expansion in an issue first.
- Keep one change focused. Do not mix refactoring with behavior changes.
- Never commit real credentials, private keys, certificates, captured traffic,
  or runtime state.

## Development checks

Use Go 1.26.4 and run the relevant checks before opening a pull request:

```sh
go build ./...
go test ./...
go vet ./...
```

Integration tests bind localhost TCP and Unix sockets. Run `go test -race ./...`
for changes affecting concurrent control, proxy, or recorder code.

## Pull requests

Explain the user-visible change, test evidence, known limitations, and any
configuration or documentation changes. Update examples when configuration
behavior changes.

By submitting a contribution, you license it under the
[Apache License 2.0](LICENSE).
