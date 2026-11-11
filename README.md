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
| v4 write | `Publish`, `Delete`, `Report` | required |
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
- **Publish** takes `multipart/form-data` and answers
  `{"errorCode":0,"errorMessage":"0","resultKey":"ModId","resultValue":"<id>"}`.
  `modId` present means update, absent means create; both are creator-only.
  `modStatusId` is derived from `modVisibility` (Public 1, Unlisted 3, Private
  4), which is an assumption: no capture shows the two fields together.
  `modTags` is accepted as repeated fields or as one comma-separated value, and
  every tag is checked against `mod_types` for the game, so an unknown tag is a
  400 rather than a foreign-key error. `ImageMetaData` entries are paired with
  the `modImages` parts by file name; an entry without an upload keeps the
  thumbnail the client sent, which is how an update reuses an existing image.
- **Uploads**: `Publish` stores the archive and the screenshots under
  `[Storage] UploadDir` (default `uploads`). The stored name is the CRC32 of the
  content plus the original extension, so uploading the same bytes twice does
  not duplicate the file. An empty `UploadDir` rejects any publish that carries
  a file with a 500.
- **Delete** removes the mod and, through `ON DELETE CASCADE`, its tags, images,
  reviews, ratings, installs and reports. Only the creator may delete, and a mod
  without a creator cannot be deleted by anyone.
- **Report** stores the concern in `mod_reports` and answers the bare `true`.
  Every field is a string in the capture, including `modId`, unlike `Delete`.

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
testdata/tags.json         verbatim GET /api/v4/mods/Tags, used as a golden
uploads/                   default UploadDir of Publish (gitignored)
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
4. **Tag catalog.** `mod_types` is seeded with the exact body of a live
   `GET /api/v4/mods/Tags`: 56 rows over 22 distinct `modTypeId` values. The id
   is global, not per game, so the same id carries different names in different
   titles (1 is "Single-player scenarios" in game 1 and "Crafted Map" in game
   4). The table is therefore keyed on `(gameTitleId, modTypeId)`, and `mods`
   and `mod_tag_map` reference that pair. `testdata/tags.json` holds the
   upstream body and the test compares against it byte for byte.
5. **`modStatusId` mapping** (1 Published, 2 Featured, 3 Unlisted, 4 Draft) is a
   working assumption, kept as a Go constant so it can be corrected without a
   migration.

## Upstream drift between the August and November captures

The captures in the `api_web` log folder come from two sessions, 2026-08-09 and
2026-11-11. Comparing them field by field shows that the service changed the
`ratings` block in between, and the November shape is the one this server
implements:

| Field | August | November |
|---|---|---|
| `ratings.leastFavorableReview` | object or null | gone |
| `ratings.mostFavorableReview` | object or null | gone |
| `ratings.rating.count1Star`..`count5Star` | integer or null | gone |
| `ratings.rating.average`, `totalCount` | present | present |
| `modList[].metaData` | the whole mod nested | null |

Everything else is identical: the 52 keys of a mod entry, the `searchParams`
echo and the `errorCode`/`errorMessage` pair. Three details of the November
captures are easy to miss and are pinned by tests:

- `ratings` is always an object, even for a mod with no ratings, where it is
  `{"rating":{"average":0,"totalCount":0},"reviewsCount":0}`.
- Every list response carries `errorCode: -1` and `errorMessage: ""`, including
  the ones that return data.
- `Reviews` answers `totalCount: null` when the mod has no reviews, and `Rate`
  answers `statusCode: null` and `responseMessage: null`.

`Related` is a fixed page: `start` 0, `count` 6, `modid` as a string and a
`totalCount` equal to the size of that page, unlike `Find`, which echoes the
request it received.

Two deviations are deliberate. `isLoggedOn` reports the real session on the list
endpoints, where every capture says `false` even for `My` and `Installed` called
with a valid cookie; reporting the truth is a superset of that. And `createDate`
is stored and emitted as RFC 3339 (`2026-11-11T20:41:14Z`) where the capture
carries `2026-11-11T20:41:14.543`, the same instant without the zone marker.

## Database compatibility

`schemaVersion` is 3. `Migrate` only creates missing objects, so a database
created with the previous single-column `mod_types` key is not upgraded: the
seed detects the partial catalog and refuses to start, asking for the file to be
deleted. There is no data to preserve in a fresh deployment.

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
`MaxOpenConns(1)` deadlock regression), the full authenticated HTTP flow, the
multipart parsing and upload storage of `Publish`, the creator-only rules of
`Publish` and `Delete`, and the all-routes inventory.

The three write handlers and their helpers are at 100% statement coverage. The
global figure is lower because the read-only handlers are exercised through the
HTTP layer while their store helpers keep unreachable error branches.
