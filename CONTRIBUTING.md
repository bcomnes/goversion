# Contributing

## Guidelines

- Patches, ideas, and changes are welcome.
- Bug fixes are almost always welcome.
- New features are sometimes welcome:
  - Please open an issue to discuss the idea **before** investing significant time.
  - The proposal may be rejected.
  - If you’d rather skip the discussion and jump straight into implementation, be prepared to maintain a fork if the idea is respectfully declined.
- Please follow the style of the existing code.
- All tests must pass.
- New features or code paths must include tests.
- Aim for 100% test coverage.
- Questions are welcome! However, unless there is an official support contract in place, support is not guaranteed.
- Contributors reserve the right to walk away from the project at any time, with or without notice.

## Releasing

The repository registers its own main package as a Go tool so the checked-out source provides the same local release workflow consumers use:

```console
go tool github.com/bcomnes/goversion/v2 -dry patch
go tool github.com/bcomnes/goversion/v2 patch
make all
go tool github.com/bcomnes/goversion/v2 publish
```

For hosted releases, manually dispatch `.github/workflows/release.yml` and select a version directive.
The workflow runs the full suite, invokes the trusted moving `go-bump v0` action reference, validates the exact release commit through `make all`, and delegates Git refs, GitHub Release creation, and Go proxy verification back to `goversion publish`.
Explicit custom versions omit the leading `v`, for example `2.5.0`.
