# Contributing

`AGENTS.md` is the complete brief for coding agents; this file is the part that
applies to a human contributor.

## The principle of the repository

The responses are pinned by captured client traffic. The goal is to reproduce
the bytes, not to improve the shape: trailing newlines are absent on purpose,
`Content-Type` is `application/json; charset=utf-8`, and fields the upstream
returns as `null`, as fixed defaults or as an echo of the resolved filter stay
that way. The `api_*_capture_test.go` replays are the guardian of that contract,
not an obstacle to a cleanup.

## Commands

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go build -o ageAPIServer ./cmd/ageAPIServer
go vet ./...
gofmt -l $(git ls-files '*.go')   # must print nothing
go test ./... -count=1
go test -race ./...               # what CI runs
```

`gofmt -l .` also walks the ignored local toolchain in `.tools/`, so use the
`git ls-files` form: it is what CI checks. Tests use `httptest` and
`t.TempDir()`; they bind no ports and need no network.

## Changing an endpoint

A route change updates three places together, and nothing cross-checks them
automatically:

1. `routeInventory` in `api.go` (the all-routes tests iterate it);
2. `api_spec.yml` (the spec is the official file, verbatim; local-only routes go
   in the `inventoryOnly` allowlist of `spec_inventory_test.go` instead);
3. the endpoint table in `README.md`.

Register lowercase, trailing-slash-free patterns (`GET /api/v4/mods/detail/{modid}`):
`router.Normalize` lowercases and trims, and the clients mix casings.

## Hard rules

- **SQLite is pure Go and single-connection.** `MaxOpenConns(1)` is deliberate:
  close `rows` before issuing another query, or read the ids into a slice first.
  No cgo driver, no second connection.
- **Never log credentials.** `internal/logger/redact.go` is the only redaction
  point; the access log carries method, path, status and duration.
- **Authentication and ownership are server-side.** Ownership is checked against
  the resolved viewer, never against an id from the request body.
- **The version comes from the linker** (`-X main.version=...`), never from a
  constant in the source.
- **Configuration is three surfaces of one key**: a field in
  `internal/config.go`, the key with its comment in `resources/config/config.toml`,
  and the environment/flag override that comes for free.
- **Do not commit** `*.db` (and `-wal`/`-shm`), `*.log`, `uploads/`, `dist/`,
  `.goreleaser.yaml`, `.tools/`, captured traffic or credentials.

## Tests

HTTP-level tests live in the root package; unit tests live next to the code they
cover. A new unit test belongs in `internal/...`, not in a root capture file.
`testdata/tags.json` is a verbatim upstream body: edit it only when the catalog
changes upstream.

## License

Contributions are accepted under AGPL-3.0-only, the license of the project.
