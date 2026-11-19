# AGENTS.md

Guidance for coding agents working in this repository.

## What this is

`ageAPIServer` is a Go implementation of the Age of Empires Mods API 1.0.0
(`api_spec.yml`) backed by SQLite. It ships as a standalone binary
(`cmd/ageAPIServer`) and as an embeddable `net/http` handler (the root
package). The wire contract is the official service: responses are matched
byte-for-byte against captured client traffic, so behaviour that looks odd is
usually a deliberate reproduction of a capture.

- Module: `github.com/luskaner/ageAPIServer`
- Go: 1.27, pure Go (`modernc.org/sqlite`), no cgo
- Storage: SQLite file, or in-memory when `DB.Path` is empty

## Commands

Run from the repository root. `CGO_ENABLED=0` is the expected build mode; the
project has no cgo dependency.

```bash
CGO_ENABLED=0 go build ./...                      # all packages
CGO_ENABLED=0 go build -o ageAPIServer ./cmd/ageAPIServer
go vet ./...
gofmt -l $(git ls-files '*.go')                   # must print nothing
go test ./... -count=1                            # full suite
go test -race ./...                               # what CI runs
go test -run TestAOM -count=1 -v .                # one capture flow
go test ./internal/models -run TestSeedCatalogs -count=1  # one package
```

`gofmt -l .` also walks the local toolchain in `.tools/` (see
[Builds and releases](#builds-and-releases)); use the `git ls-files` form
above so the result matches what CI checks.

Tests use `httptest` and `t.TempDir()`. They bind no ports, need no network
and have no build tags or `-short` gating: the whole suite is the gate.

## Layout

```
api.go                        package API: Config, New, Handler, Routes, routeInventory
cmd/ageAPIServer/main.go      process entry point (thin; only wires the version)
internal/cmd/                 flags, listener, lifecycle, --new-session
internal/cert/                certificate pair of the HTTPS listener (no generator)
internal/config.go            TOML + AGEAPISERVER_* env + pflag
internal/http.go              Bind (GET query / POST JSON / form), JSON, Json[T]
internal/boolInt.go           0/1 <-> bool, the single conversion point for SQLite flags
internal/errors.go            process exit codes of the entry point
internal/logger/              console + file logger, credential redaction
internal/models/              schema, stores, seed, match file store
internal/models/initializer/  opens the DB, migrates and seeds it
internal/routes/router/       Group over ServeMux, middleware chain, viewer context
internal/routes/shared/       DTOs, FlexInt, pagination, error/JSON writers, URLs
internal/routes/api/mods/v4/  one file per /api/v4/mods endpoint
internal/routes/api/mods/v1/  one file per /api/v1/mods endpoint
internal/routes/poll/         /Poll endpoints
internal/routes/textmoderation/
internal/routes/matchcontent/ /api/MatchContent endpoints
testdata/tags.json            verbatim GET /api/v4/mods/Tags, used as a golden
```

The `internal/routes` tree mirrors the URL: `/api/v4/mods/Games` is served by
`internal/routes/api/mods/v4/games.go`. `router` and `shared` are the only
exceptions, because they are infrastructure rather than endpoints.

## Rules that are easy to get wrong

- **Every endpoint lives in `routeInventory`** (`api.go`). `Routes()` serves the
  inventory, and the all-routes tests iterate it, so a route registered in the
  mux but missing from the inventory is silently untested. Adding or changing a
  route means updating `routeInventory`, `api_spec.yml` and the endpoint table
  in `README.md` together; nothing cross-checks those three automatically.
- **Paths are lowercase and have no trailing slash.** Register
  `GET /api/v4/mods/detail/{modid}` style patterns only; `router.Normalize`
  lowercases, collapses slashes and trims the trailing one. Clients mix casings
  (`/api/v4/Mods/...` and `/api/v4/mods/...` in one session), which is why the
  pattern must never encode the spec's own capitalisation.
- **Response shapes are pinned by captures.** Trailing newlines are omitted on
  purpose (the game compares raw bytes), `Content-Type` is
  `application/json; charset=utf-8`, and fields that the captures return as
  `null`, as fixed defaults, or as an echo of the resolved filter must stay that
  way. Do not "clean up" a shape the tests encode.
- **Write your own JSON through `internal.JSON` / `shared.WriteJSON`.** Do not
  stream request-derived values straight to the client, and use
  `shared.WriteError` / `shared.WriteProblemDetails` for failures so the error
  envelope stays consistent.
- **Authentication and ownership are server-side.** The credential schemes
  (`Steam1.0`, `XBL3.0`, session cookie) resolve through middleware; ownership
  (creator-only publish update and delete) is checked against the resolved
  viewer, never against an id taken from the request body. The tickets are not
  verified cryptographically by design; do not describe this as secure.
- **Never log credentials.** `internal/logger/redact.go` is the single place
  that redacts `Authorization` and `Cookie`; access logs carry method, path,
  status and duration only. Do not add header dumps.
- **SQLite stays pure Go and single-connection.** `MaxOpenConns(1)` is
  deliberate. A store call that opens a second query while a `rows` handle is
  still open blocks forever, so the suite times out instead of failing fast:
  close rows before issuing another query, or read the ids into a slice first.
  `TestFindHydratesImagesAndTagsWithoutDeadlock` pins that shape. Do not add a
  cgo SQLite driver or a second connection.
- **Configuration is three surfaces of one key**: a PascalCase field in
  `internal/config.go`, the key and its comment in
  `resources/config/config.toml`, and the environment (`AGEAPISERVER_`) /
  flag (`--section.key`) override that comes for free. Settings that live in
  the library `Config` (`api.go`) instead are documented in `README.md`.
- **TLS is a listener concern.** `[Server.TLS] Mode` is `off` (plain HTTP),
  `on` (this process serves TLS with `<folder>/cert.pem` and `key.pem`) or
  `proxy` (an external terminator; derived URLs default to https). Only `on`
  reads the pair, and nothing in the project generates certificates: do not add
  a generator. The pair lives next to the executable and is gitignored.
- **The version comes from the linker** (`-X main.version=...`, default
  `development`); never hardcode it.
- **Comments explain the capture, not the syntax.** The existing ones cite the
  client, endpoint or observed answer that justifies the code; match that style
  when adding non-obvious behaviour.

## Tests

The root package holds the HTTP-level tests — the capture replays and the
flows that pin the wire — all driven through `httptest`:

| File | Covers |
|---|---|
| `api_test.go` | core flows, pagination, error envelopes, the shared `newTestServer` / `seed` / `do` helpers |
| `api_aom_capture_test.go` | the AOM client: publish through `modFiles`, featured echo, `My` clamp |
| `api_aoe3_capture_test.go` | AOE3: mixed Steam + XBL in one session |
| `api_aoe4_capture_test.go` | AOE4: localized JSON names, `CheckAndPublishFile` block upload, version bump |
| `api_v1_capture_test.go` | the legacy v1 read/write shapes and the create/update flow |
| `api_mods_write_test.go` | Publish, Delete, Report, creator-only rules |
| `api_mods_media_test.go` | image/media routes and absolute URL builders |
| `api_upstream_shape_test.go` | null/echo details the upstream returns |
| `matchcontent_test.go` | savegame store: sizes, eviction, TTL, MD5-addressed file URLs |
| `httptest_all_routes_test.go` | every inventory route: CORS preflight 204, anonymous 401, registered (no 405) |

Everything below the root package keeps its unit tests next to the code
(`internal/config_test.go`, `internal/logger/log_test.go`,
`internal/models/*_test.go`, `internal/routes/router/*_test.go`,
`internal/routes/shared/*_test.go`); a new unit test belongs there, not in the
root files.

Helper conventions: `newTestServer(t)` builds a server over a temp DB and
returns its path, `seed(t, path)` fills it through a second handle, `do(...)`
issues the request, and `steamAuth()` / `steamToken()` provide the captured
identity. `testdata/tags.json` is a verbatim upstream body, so it is edited
only when the catalog itself changes upstream.

## Builds and releases

`Taskfile.yml` drives GoReleaser; the config is generated, never edited:

```bash
task build            # snapshot build into dist/ (goreleaser build --snapshot)
task release          # signed snapshot release
task release-no-sign  # release without the GPG signature
```

The config generator is a **separate Go module** in `tools/scripts` (so it does
not join `go build ./...` in the root module). It writes `.goreleaser.yaml` at
the repository root:

```bash
GOROOT_LEGACY=/path/to/go-legacy-win7 task build
# equivalent to:
cd tools/scripts && go run ./cmd/generateGoreleaserConfig.go "$GOROOT_LEGACY"
```

To change targets, archives or naming, edit
`tools/scripts/internal/goreleaser/project.go` — never the generated file.

- **Windows 7 needs the legacy toolchain.** Only the `ageAPIServer-win7-*`
  builds carry a `tool:` entry, pointing at `$GOROOT_LEGACY/bin/go(.exe)`; the
  other targets use the normal Go. Set `GOROOT_LEGACY` in the environment or in
  a `.env` file (the Taskfile loads it). A local copy of the SDK lives in
  `.tools/go-legacy-win7` (ignored by git); release builds produce PE
  subsystem 6.1 for win7 and 10.0 for win10.
- **Release matrix**: win7 (386, amd64), win10 (386, amd64, arm64), linux
  (386, amd64, arm64, arm 5/6), macOS universal. Archives are `zip` on Windows
  and `tar.gz` elsewhere, and include `README.md` and `api_spec.yml`.
- **CI**: `.github/workflows/ci.yml` runs gofmt, vet, build and `go test -race`
  on pushes to `main`, tags and pull requests.
  `.github/workflows/release.yml` runs on `v*.*.*` tags: it installs the legacy
  Go, generates the config, imports the GPG key and publishes a draft release.
  The signing key must be the one behind `release_public.key`, the public key
  shipped for verifying signatures.

## Do not commit

`*.db` and their `-wal`/`-shm`, `*.log`, `uploads/`, `dist/`,
`.goreleaser.yaml`, `.tools/`, IDE folders and any captured traffic or
credential. Keep private keys and environment-specific configuration out of
the repository.
