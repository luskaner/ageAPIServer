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

`Config` also carries `Console` (where the per-request log line goes; `nil` is
stdout, `io.Discard` keeps a host application's output clean), `PublicBaseURL`
(the base of download and media URLs; empty derives it from each request),
`MaxBodySize` (zero means 1 MiB; `Publish` needs more) and `MatchContent` (the
savegame store; the zero value leaves those endpoints off).

## Endpoints

Every path of `api_spec.yml` is served. Matching is case-insensitive and the
trailing slash is optional, because the clients disagree: AOE2 sends
`/api/v4/mods/...`, AOE3 and AOE4 send `/api/v4/Mods/...`, AOM sends both in the
same session, and the website sends `/api/v4/mods/UnSubscribe/...`.

| Group | Endpoints | Auth |
|---|---|---|
| v4 read-only | `Games`, `Tags`, `Featured`, `Find`, `Detail`, `Related`, `Reviews` | anonymous |
| v4 user | `Installed`, `My`, `Subscribe`, `Unsubscribe`, `Rate`, `Download`, `Download .../file` | required |
| v4 write | `Publish`, `CheckAndPublishFile`, `Delete`, `Report` | required |
| v1 legacy | `Detail`, `FindAsUser`, `Installed`, `My`, `Flag`, `Install`, `Uninstall`, `Create`, `Delete` | required |
| Polls | `/Poll/list`, `/Poll/{pollId}` | required |
| Moderation | `/textmoderation` | required |
| MatchContent | `/api/MatchContent/`, `/{matchId}` and `/{matchId}/file/{hash}` (outside the spec) | anonymous |
| Media | `/api/v4/mods/image/{name}` (outside the spec) | anonymous |
| Ops | `/healthz` (outside the spec) | anonymous |

Authentication is `Authorization: Steam1.0 <token>`, `Authorization: XBL3.0
x=<puid>;<token>` or the `.AgeOfEmpiresServices` cookie. The AOE3 capture
carries both game schemes in one session (Steam for the read endpoints, XBL
for `My` and `Delete`): the ticket and the XToken are not verified
cryptographically, so any well-formed credential creates a user keyed by its
id; do not expose this to the internet without a real check.

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
- **searchParams echo**: every list echoes the RESOLVED filter, not the raw
  body. The AOM client's `Featured` with the body `{"game":1001}` answers
  `{"q":"","creator":null,"filter":0,"sort":"popular","order":"DESC","start":1,
  "count":10,"game":1001,"modid":"0"}` — the defaults filled per endpoint
  (`My` answers the caller as the creator and its default count 10, `Installed`
  answers creator "" and count 1000), the order upper-cased and the mod id
  always as the JSON string (`"0"` when unset). The website sends its sort and
  count explicitly (`Featured` holds `recentpopular`/3 that way), and any
  explicit `My` count lands on the 500 maximum the captures echo (12, 1000 and
  20000 all answer 500). The legacy v1 echo keeps the raw request instead,
  modid null when absent.
- **Related rows** carry `modTags` null, unlike `Find`/`My`/`Featured`, which
  carry the tag names there; both shapes are what the captures show.
- **Mod statuses**: only two pairs live across every capture -
  `modStatusId 2`/"Published" (all the created mods) and `modStatusId 5`/"Featured"
  (the AOM `Featured` rows and the featured 286553 detail). `Featured` lists the
  status-5 mods of the game, the echoed name filter resolves "featured" to 5, and
  every other stored id renders "Published". A list row whose `modTypeId` does
  not resolve in the Tags catalog of its game answers `"modType": "Unknown"`
  (the type 1 of game 1001) while the `Detail` of the same mod answers null.
- **Ratings** render one decimal: the AOM `Rate` of a 125-rating mod answers
  average 4.1, so the stored aggregate and both surfaces keep the rounded form.
- **textmoderation** answers the detected language of the text, not the echo
  of the input (`"Test Mod"` answers `en` even with `es-ES` as the input).
  The base subtag of the input language is the deterministic stand-in, since
  a real detector is not worth a dependency.
- **CORS**: the website sends a preflight before every POST. The middleware
  answers it with 204 before the mux and never uses a wildcard origin while
  credentials are allowed.
- **Logs**: `Authorization` and `Cookie` are redacted in one place
  (`internal/logger/redact.go`); the access log carries only method, path,
  status and duration.
- **Publish** takes `multipart/form-data` and answers
  `{"errorCode":0,"errorMessage":"0","resultKey":"ModId","resultValue":"<id>"}`.
  `modId` present means update, absent means create; both are creator-only.
  A created mod takes its id from the reserved local band
  `[1,000,000,000, 2^31-1)` (`models.LocalModIDFloor`), which the official
  namespace - the upstream scrape peaks at 593,695 - can never reach, so the
  same numeric id cannot mean two different mods when a client switches
  between this server and the live one. The band only ever advances:
  deleting a mod does not recycle its id.
  `modStatusId` is always the published 2 — the created-mod capture of the
  AOE2 logs answers `2`/`Published` for an `Unlisted` visibility and every
  live detail shows the same pair, so visibility and status are independent.
  `modTags` is accepted as repeated fields or as one comma-separated value, and
  every tag is checked against `mod_types` for the game, so an unknown tag is a
  400 rather than a foreign-key error. `ImageMetaData` entries are paired with
  the `modImages` parts by file name; an entry without an upload keeps the
  thumbnail the client sent, which is how an update reuses an existing image.
  The AOE4 client shapes the same body differently, and all of it is accepted:
  `modName` and `description` arrive as JSON localization maps
  (`{"es":"moddisplayname"}`) and are stored as their plain string (the `"en"`
  entry wins, otherwise the first entry in the order the client wrote); the
  visibility arrives lowercase (`public`); `clientId` is accepted and left
  null; the changeList can be absent, which stores an empty string instead of
  the wrapped header. An update publish that carries no `modVersion` bumps the
  stored version by 0.1 (the AOE4 capture moves 1.1 to 1.2), and the Detail of
  a subscriber whose installed version predates it flips `updateAvailable` to
  true - re-installing clears it again. The Detail always renders `modTags`
  null (both created-mod captures do, with `modTagIds [26]` filled) - only the
  list rows carry the tag names there.
- **CheckAndPublishFile** (`PUT /api/v4/mods/CheckAndPublishFile`) is the Azure
  Block Blob flow the AOE4 client uploads the archive with right after Publish.
  One request per block, then a commit, both answering the bare `true`:
  `?comp=block&blockid=00000000&modId=N` carries the raw file bytes as the body
  (staged under `UploadDir/blocks/<modId>/`), and
  `?comp=blocklist&modId=N` carries
  `<?xml…?><BlockList><Latest>00000000</Latest></BlockList>`. The commit
  concatenates the listed blocks in order, stores them as the mod file under
  the CRC32 name (the same storage Publish uses), fills `fileUrl`, `fileCRC`,
  `modFileSize` and `modDiskSize` (the capture shows them equal: 1509669
  both), stamps `lastUpdate` with the commit time of the capture and answers
  `true`. Both requests require authentication, the commit also requires the
  caller to own the mod, and the route is exempt from `Server.MaxBodyBytes`
  with its own 512 MiB block cap, since the capture's single block is already
  1.5 MB.
- **Create (v1)** is the AOE2 client flow of the same idea: a multipart form
  whose fields are spelled differently (`ModName`, `GameTitleId`, `ModType`,
  `Description`, `modVisibility`, `ChangeList`, `ImageMetaData` with a string
  `imageFileSize`, `modImages`, `modFiles` — that casing is read
  case-insensitively). A body with `ModId` of the caller's mod UPDATES it and
  answers the same id - the AOE2 client re-publishes through this endpoint
  (the launcher log of November 19 shows the update carrying the stored image
  identity and no file parts), and handin new ids there crashed the game on
  every update, so it is what the endpoint's update mode pins: same id, the
  +0.1 version tick and the updateAvailable flip, the changeList rewrapped,
  and the stored image untouched when the body only re-declares its identity.
  Its create side effects, pinned by the capture of the created
  `test_mod` (593779): the status stays Published (2); the stored `modVersion`
  is zero and only the v4 Detail renders it as 1.1; `verifiedVersion` starts unset (the v1 rows answer null and the v4 Detail an empty string); the description is mirrored into `modDescription`; the changeList is
  stored prefixed with the update-date header of the day —
  `<h4 class="mod-update-date">11/18/2026</h4><p>changelog</p>`; the creator
  counts as its first download and install (popular and both counters start at
  1, and `userDownloaded`/`userInstalled` are true); the first uploaded image
  becomes the thumbnail; `modDiskSize` is the uncompressed size of the
  archive; and a create without explicit tags gets `Other` (26) even when the
  game's catalog does not sell it — the AOE4 created mod answers
  `modTagIds [26]` with an empty `modTagNames`, so the id renders without a
  name there. `modDiskSize` and the storage of `modTypeId` need no
  catalog lookup: the upstream accepts a `ModType` its Tags catalog does not
  sell (the capture created game 2 with `ModType` 1, and the AOE4 client
  uploads type 6 — `Extension` — whose name the server still resolves), so
  the column is a join hint, not a foreign key.
- **Delete (v1)** and **Delete (v4)**: the legacy endpoint answers
  `{"errorCode":0,"errorMessage":"0","resultKey":"Deleted","resultValue":"true","resultJson":null}`
  for `{"id":N}`; the v4 one answers the bare `true` for `{"modId":N}`. Both
  remove the mod and, through `ON DELETE CASCADE`, its tags, images,
  reviews, ratings, installs and reports. Only the creator may delete, and a
  mod without a creator cannot be deleted by anyone.
- **Missing resources**: the v4 `Detail` of a deleted or unknown mod answers
  the RFC 9110 `ProblemDetails` body of the web server
  (`{"type":"…/html/rfc9110#section-15.5.5","title":"Not Found","status":404,
  "traceId":"00-…"}`) in place of the mod error envelope, and the v4
  `Related` of one answers `204 No Content` — both are the answers of the
  captures of the deleted `test_mod`.
- **The legacy v1 row shape** differs from the v4 one for the same mods, and
  the v1 endpoints reproduce it: the lists carry the tags as string ids in
  `modTags` with `modTagIds` null, `ratings` null, the rating aggregates zero
  (`currentRating` 0.0), `changeList` null, `popular` null, and `totalCount`
  null on an empty page (and always in `My`). The legacy `Detail` is a sparse
  outer object — identity, texts, type, status and the image list, with every
  viewer field, date, size and counter null or zero, and `isLoggedOn` false
  even for the owner — wrapped around the real render nested in `metaData`,
  snapshot included.
- **Uploads**: `Publish` stores the archive and the screenshots under
  `[Storage] UploadDir` (default `uploads`). The stored name is the CRC32 of the
  content plus the original extension, so uploading the same bytes twice does
  not duplicate the file. An empty `UploadDir` rejects any publish that carries
  a file with a 500.
- **MatchContent**: the game posts a multiplayer savegame or replay as multipart
  (`fileContent` + `matchId`) and later asks for the metadata, which carries an
  absolute `filePath`; a second GET streams the bytes. The upload is streamed to
  disk and never buffered, only the extensions the games use are accepted, and
  `[MatchContent]` bounds the per-file size, the accumulated size (the oldest
  file is evicted first) and the TTL. The route is exempt from
  `Server.MaxBodyBytes` because a savegame is measured in megabytes; the store
  applies its own limit. The path parameter is a `matchId` and the file URL also
  carries the MD5, so metadata from before an overwrite cannot fetch the new
  bytes.
- **Download** answers the absolute URL of `/api/v4/Mods/Download/{modId}/file`, which streams the stored
  archive from `[Storage] UploadDir`. That route does not count a download: the counter belongs to the JSON
  endpoint. `imageThumbnail` and `imageName` are emitted as absolute URLs of `/api/v4/mods/image/{name}` (both
  refer to the same stored image in the captures), and the `fileUrl` of a Detail is the absolute download URL,
  because the upstream answer carries URLs everywhere and a bare name is unreachable for the client. A value that is
  already a URL passes through untouched, so a seeded mod keeps its captured CDN address.
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
internal/models/           SQLite schema, stores, seed, match file store
internal/routes/router/    Group over ServeMux + middlewares
internal/routes/shared/    DTOs, FlexInt, pagination, download URL
internal/routes/matchcontent/   /api/MatchContent endpoints
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
   4). The table is therefore keyed on `(gameTitleId, modTypeId)`, and
   `mod_tag_map` references that pair. `mods.modTypeId` does not: the live
   Create accepted a `ModType` the Tags catalog does not sell, so the column
   is a join hint, not a foreign key. `testdata/tags.json` holds the
   upstream body and the test compares against it byte for byte.
5. **`modStatusId` mapping**: the created mod of the AOE2 captures answers
   `2`/`Published` for an Unlisted visibility, and every live detail shows the
   same pair, so `Published` is 2. The other three names guess the remaining
   slots and remain a constant that a capture can correct without a
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
request it received. The AOE2 captures of a deleted mod add two answers this
server honours: the `Related` of a missing mod is `204 No Content` instead of
an error body, and the v4 `Detail` of one
is the RFC 9110 `ProblemDetails` of the web server.

Two deviations are deliberate. `isLoggedOn` reports the real session on the v4
list endpoints, where the August/November captures say `false` even for `My`
and `Installed` called with a valid cookie; reporting the truth is a superset
of that — and the AOE2 v1 captures agree, their lists say `true`. And the
`createDate` of a seeded mod keeps the value it was seeded with, which may be
RFC 3339; a mod this server publishes is stamped with the capture format
(`2026-11-18T22:07:21.043`), the millisecond instant without the zone marker.

## The AOE2 captures

The `api_aoe2` folder captures the AOE2 client and the website against the
live service (2026-11-11 and 2026-11-18) and pins what was missing from the
web captures: the legacy `Create`, the legacy `Delete`, the legacy row shape
of the lists and of `Detail` (sparse wrapper around the `metaData` snapshot),
and the answers for a missing or deleted mod. `TestCreateMatchesTheLegacyCapture`
walks the whole lifecycle — create, detail lookup from both versions, install,
my list, delete and the two post-delete answers — and asserts each one against
the captured bytes.

## Database compatibility

`schemaVersion` is 5. `Migrate` only creates missing objects, so a database
created with the previous single-column `mod_types` key is not upgraded: the
seed detects the partial catalog and refuses to start, asking for the file to be
deleted. Version 4 also drops the `mods.modTypeId` foreign key, which an older
database keeps; it only constrains the columns a fresh database creates without.
Version 5 adds the `mod_id_allocator` of the locally published id band. There is
no data to preserve in a fresh deployment.

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
