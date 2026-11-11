# ageAPIServer

Go implementation of the **Age of Empires Mods API v3.2.0** (`api_spec.yml`)
backed by SQLite. It runs as a standalone binary and mounts as one more route in
any existing `net/http` application.

- Module: `github.com/luskaner/ageAPIServer`
- Go: 1.27
- Storage: SQLite via `modernc.org/sqlite` (pure Go, no cgo)

## Build and run

```bash
go build -o ageAPIServer ./cmd/ageAPIServer
./ageAPIServer
```

The binary reads `resources/config/config.toml`, then the environment (prefix
`AGEAPISERVER_`), then the flags:

```bash
AGEAPISERVER_SERVER_ADDR=:9000 AGEAPISERVER_DB_PATH=/var/lib/mods.db ./ageAPIServer
./ageAPIServer --server.addr=:9000 --log.level=debug
```

The spec has no login endpoint, so an operator obtains a website session cookie
with:

```bash
./ageAPIServer --new-session '<steam-token>'
```

## Use as a library

```go
srv, err := ageAPIServer.New(ageAPIServer.Config{DBPath: "mods.db"})
if err != nil {
    log.Fatal(err)
}
defer srv.Shutdown(context.Background())

mux := http.NewServeMux()
mux.Handle("/aoeMods/", http.StripPrefix("/aoeMods", srv.Handler()))
```

`srv.Routes()` returns the inventory (`Method`, internal `Path`, `AuthRequired`,
`SpecPath`) so an integrator can generate a client or a proxy.

## Endpoints

Every path of `api_spec.yml` is served. Matching is case-insensitive and the
trailing slash is optional, because the clients disagree: AOE2 sends
`/api/v4/mods/...`, AOE3 and AOE4 send `/api/v4/Mods/...`, AOM sends both in the
same session, and the website sends `/api/v4/mods/UnSubscribe/...`.

| Group | Endpoints | Auth |
|---|---|---|
| v4 read-only | `Games`, `Tags`, `Featured`, `Find`, `Detail`, `Related`, `Reviews` | anonymous |
| v4 user | `Installed`, `My`, `Subscribe`, `Unsubscribe`, `Rate`, `Download` | required |
| v1 legacy | `Detail`, `FindAsUser`, `Installed`, `My`, `Flag`, `Install`, `Uninstall` | required |
| Polls | `/Poll/list`, `/Poll/{pollId}` | required |
| Moderation | `/textmoderation` | required |
| Ops | `/healthz` (outside the spec) | anonymous |

Authentication is `Authorization: Steam1.0 <token>` or the
`.AgeOfEmpiresServices` cookie. The Steam ticket is not verified
cryptographically yet: any well-formed token creates a user, so do not expose
this to the internet without a real check.

## Behaviour worth knowing

- **Pagination**: `start` is a 1-based page number, so
  `OFFSET = max(0, start-1) * count`. `start` 0 and 1 both mean the first page.
  `count: 0` means "no limit", bounded by `Pagination.MaxCount`. Switch to
  `[Pagination] Base = "offset"` if a capture with response bodies proves
  otherwise.
- **Sentinels**: `q: ""` and `q: "*"` mean "no filter"; `filter: 0` and
  `modid: 0`/`""` likewise; negative tag ids exclude.
- **Counters**: `Subscribe` does not count, `Download` counts once per user, so
  the AOE4 pair (`Subscribe` + `Download`) does not double the counter.
- **CORS**: the website sends a preflight before every POST. The middleware
  answers it with 204 before the mux and never uses a wildcard origin while
  credentials are allowed.
- **Logs**: `Authorization` and `Cookie` are redacted in one place
  (`internal/logger/redact.go`); the access log carries only method, path,
  status and duration.

## Layout

```
api.go                     library API: Config, New, Handler, Routes
cmd/ageAPIServer/main.go   thin binary
internal/cmd/              flags, config, listener, shutdown, --new-session
internal/config.go         koanf/TOML + environment + pflag
internal/http.go           Bind, JSON, Json[T], H, A
internal/logger/           console + file logger, credential redaction
internal/models/           SQLite schema, stores, seed
internal/routes/router/    Group over ServeMux + middlewares
internal/routes/shared/    DTOs, FlexInt, pagination, download URL
internal/routes/api/mods/v4/   one file per /api/v4/mods endpoint
internal/routes/api/mods/v1/   one file per /api/v1/mods endpoint
internal/routes/poll/          /poll endpoints
internal/routes/textmoderation/  /textmoderation endpoint
```

The `internal/routes` tree mirrors the URL: `/api/v4/mods/Games` is served by
`internal/routes/api/mods/v4/games.go`. `router` and `shared` are the only
exceptions, because they are infrastructure rather than endpoints.

## Deviations from the implementation plan

1. **Binary location.** The plan puts `main.go` and `api.go` in the repository
   root, which Go rejects: one directory is one package. The root is the library
   package `ageAPIServer` and the binary lives in `cmd/ageAPIServer`, so
   `go build ./cmd/ageAPIServer` produces it.
2. **`subscriptions.downloadCounted`.** The plan's counter rule (increment only
   when `downloadedAt` was NULL) never counts, because `Subscribe` already sets
   `downloadedAt`. A dedicated flag keeps the plan's intent: the pair counts
   once.
3. **`reviews.reviewerId` stays a string** as the spec declares, so `userReview`
   is resolved through the viewer's Steam id. A cookie-only session has no
   Steam id and therefore no `userReview`.
4. **Tag catalog.** The catalog is per game, not shared: a live
   `GET /api/v4/mods/Tags` returns a different list for every title. Games 1, 2
   and 4 are transcribed from that call; games 3 and 1001 were never captured
   and keep a placeholder with stable ids.
5. **`modStatusId` mapping** (1 Published, 2 Featured, 3 Unlisted, 4 Draft) is a
   working assumption, kept as a Go constant so it can be corrected without a
   migration.

## Open question: anonymous read-only endpoints

`api_spec.yml` declares `security: []` on Games, Tags, Featured, Find, Detail,
Related and Reviews, and this server honours that. A live probe of
`api.ageofempires.com` answered `401` with `errorCode 900` on all of them except
Games and Tags.

The evidence is not conclusive. The two endpoints that still answer are exactly
the two static, cacheable GETs, so a CDN serving them from cache while the
origin rejects anonymous traffic explains the split without any policy change.
The probe also reported an empty body for `Detail`, which contradicts the JSON
error it got everywhere else, and it never ran a control request with a valid
credential.

The default therefore stays spec-compliant. Set `Auth.RequireForReadOnly` (or
`AGEAPISERVER_AUTH_REQUIREFORREADONLY=true`) to make those seven endpoints
reject anonymous requests if the finding is confirmed.

## Tests

```bash
go test -race ./...
```

Coverage includes path normalization, CORS preflight, credential redaction,
pagination and sentinel rules, the store queries (including the
`MaxOpenConns(1)` deadlock regression), the full authenticated HTTP flow and the
all-routes inventory.
