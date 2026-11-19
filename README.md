# ageAPIServer

Go implementation of the **Age of Empires Mods API 1.0.0** (`api_spec.yml`;
backed by SQLite. It runs as a standalone binary and mounts as one more route in
any existing `net/http` application.

- Module: `github.com/luskaner/ageAPIServer`
- Go: 1.27
- Storage: SQLite via `modernc.org/sqlite` (pure Go, no cgo)
- Transport: plain HTTP, TLS terminated in-process, or HTTPS offloaded to a
  proxy (`[Server.TLS] Mode`)

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

Docker images and compose files live in the ageLANServer-style tree
`tools/server-docker/` (multi-stage `golang:1.27-alpine3.24` → `upx` →
`alpine:3.24`):

```bash
docker compose -f tools/server-docker/Compose/server.bridge.yml up --build
```

`tools/server-docker/README.md` documents the bridge, host and nginx variants,
the volumes and the environment variables.

The spec has no login endpoint, so an operator obtains a website session cookie
with:

```bash
./ageAPIServer --new-session '<steam-token>'
```

## HTTPS

`[Server.TLS] Mode` picks the deployment. The game clients speak plain HTTP, so
the default is `off`; the other two are for a host that is reached over TLS:

| `Mode` | This listener | Absolute URLs (`downloadUrl`, `imageThumbnail`, `filePath`) |
|---|---|---|
| `off` (default) | plain HTTP | follow the request, `X-Forwarded-Proto`/`-Host` aware |
| `on` | TLS, terminated here | `https` |
| `proxy` | plain HTTP, terminated by a proxy | `https` unless `X-Forwarded-Proto` says otherwise |

`on` keeps the conventional layout: the pair is looked up as `cert.pem` and
`key.pem` inside `resources/certificates` next to the binary (created on first
use), and `[Server.TLS] Folder`, `Cert` and `Key` override that. The project does
not generate certificates - bring the pair, either issued by a CA or any
self-signed one your clients trust:

```bash
./ageAPIServer --server.addr=:443 --server.tls.mode=on
```

Behind an external terminator (nginx, Caddy, a cloud load balancer) keep the
HTTP listener and set `proxy`. It is `off` plus one statement: a request that
arrives without TLS and without `X-Forwarded-Proto` still answers `https` links,
so a proxy that forwards only the host does not hand the client an http
download. `Server.PublicBaseURL` still pins the whole base when the deployment
has a canonical one.

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

`GET /openapi.yml` serves the embedded `api_spec.yml` (it is the file shipped in
the releases, byte for byte), so a client generator can read the contract
without unpacking the archive.

| Group | Endpoints | Auth |
|---|---|---|
| v4 read-only | `Games`, `Tags`, `Featured`, `Find`, `Detail`, `Related`, `Reviews` | required |
| v4 user | `Installed`, `My`, `Subscribe`, `Unsubscribe`, `Rate`, `Download`, `Download .../file` | required |
| v4 write | `Publish`, `CheckAndPublishFile`, `Delete`, `Report` | required |
| v1 legacy | `Detail`, `FindAsUser`, `Installed`, `My`, `Flag`, `Install`, `Uninstall`, `Create`, `Delete` | required |
| Polls | `/Poll/list`, `/Poll/{pollId}` | required |
| Moderation | `/textmoderation` | required |
| MatchContent | `/api/MatchContent/`, `/{matchId}` and `/{matchId}/file/{hash}` | anonymous |
| Media | `/api/v4/mods/image/{name}` | anonymous |
| Ops (local, not upstream) | `/healthz`, `/openapi.yml` | anonymous |

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

## Operation

### Resource bases

Two bases, deliberately - it is the layout of the ageLANServer family:

| Resolved against the working directory | Resolved against the executable's folder |
|---|---|
| `resources/config/config.toml` | `resources/certificates/cert.pem` |
| `ageAPIServer.log` (and its `.old` generation) | `resources/certificates/key.pem` |
| `ageAPIServer.db` (plus `-wal`/`-shm`) | |
| `uploads/` (archives, screenshots, block staging) | |
| the MatchContent folder when it is not `auto` | |

`[MatchContent] Folder = "auto"` is `<os.TempDir()>/ageAPIServer/custom-files`, so
a savegame store that has to survive a reboot must name a folder.
`[Storage] UploadDir` and `[Server.TLS] Folder` override their defaults. Run the
binary from (or set `WorkingDirectory` to) the directory that should own the
database and the uploads.

### Behind a reverse proxy

Keep the listener on loopback or a private network and let the proxy be the only
public entry point. Three body limits are mandatory, because nginx defaults to
1 MiB while the uploads are measured in megabytes and gigabytes:

```nginx
# AOE4 block upload: one 512 MiB block per request.
location /api/v4/mods/checkandpublishfile {
    client_max_body_size 512m;
    proxy_request_buffering off;
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
    proxy_pass http://127.0.0.1:8080;
}

# Publish (v4) and Create (v1): a .zip of up to 1 GiB plus multipart framing.
location ~* ^/api/(v4/mods/publish|v1/mods/create)/?$ {
    client_max_body_size 1100m;
    proxy_request_buffering off;
    proxy_read_timeout 600s;
    proxy_send_timeout 600s;
    proxy_pass http://127.0.0.1:8080;
}

# MatchContent savegames and replays: 32 MiB plus slack.
location /api/matchcontent {
    client_max_body_size 36m;
    proxy_request_buffering off;
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;
    proxy_pass http://127.0.0.1:8080;
}
```

`docs/nginx-ageapiserver.conf` is the same file for a bare-metal proxy, and
`tools/server-docker/Compose/server.proxy.bridge.yml` wires the containerized
equivalent in front of the server. The high read/send timeouts are for the same
bodies: a slow 1 GiB upload must not be cut at 60 s.

`X-Forwarded-Proto`, `-Host` and `-For` are trusted unconditionally. They only
build the absolute URLs of the response, so they are not an authentication
bypass by themselves - but any client can set them when it talks to the server
directly. A trusted-proxy allowlist is future work (see `SECURITY.md`).

### Web upload vs game upload

The two upload surfaces have different contracts, and only `Publish` /
`Create` distinguish them by the credential that resolved the request:

| | Website (`.AgeOfEmpiresServices` cookie) | Game clients (`Steam1.0` / `XBL3.0`) |
|---|---|---|
| `modName` | required | required |
| `description` | required | optional |
| screenshots | 1 to 5, closed list of formats | optional, any number |
| tags | at most 7 | no limit |
| archive | `.zip`, up to `[Storage] MaxModFileBytes` (1 GiB) | any extension; AOE4 sends its `.sga` in blocks through `CheckAndPublishFile` |
| body cap | exempt from `Server.MaxBodyBytes`; streamed under `MaxModFileBytes` | same |

The website rules live in `ValidateWebPublish` and are applied to a cookie
publish only. The game path stays exactly as the captures encode it - no image
minimum, no archive extension rule - because a new game client with another
format must not be broken by a form the website happens to enforce. Oversized
files answer 400 before anything is stored: the limit is applied to the bytes
streamed to disk, not to the client-supplied multipart size header.

### Log rotation

`[Log] MaxSizeBytes` is a startup check, not a watcher: when the file is already
bigger than the limit it is renamed to `<File>.old` (one generation) and a new
file is opened. `0` disables it.

For anything richer, leave the key at `0` and rotate externally:

```
# /etc/logrotate.d/ageAPIServer
/var/lib/ageAPIServer/ageAPIServer.log {
    daily
    rotate 14
    compress
    missingok
    notifempty
    copytruncate
}
```

On Windows, a scheduled task can rename the file and restart the service, or
use any log-shipping agent; the server does not need to be told.

### Removing leftover uploads

`--gc-uploads` compares `[Storage] UploadDir` with the database and reports the
files nothing references; `--gc-apply` deletes exactly what was reported. Run it
with the server stopped:

```bash
./ageAPIServer --gc-uploads            # dry run, report only
./ageAPIServer --gc-uploads --gc-apply # delete what was reported
```

A stale `upload-*` temporary is collected only when it is more than an hour old,
so a publish in flight is never touched. `blocks/<modId>` of a mod that does not
exist is collected whole, and the MatchContent folder is left alone because that
store expires its own files. The GC never deletes by reference count: stored
names are the CRC32 of the content, and two mods can share one file, so a name
survives while any row still points at it.

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

## Tests

```bash
go test -race ./...
```

Coverage is measured across packages (`-coverpkg=./...`), because the handlers
are exercised through the root HTTP suite rather than a package-local test:
without the flag those packages would report 0% while being fully exercised.

```bash
go test -coverpkg=./... ./... -coverprofile cover.out
go tool cover -func cover.out | tail -1
```

Coverage includes path normalization, CORS preflight, credential redaction,
pagination and sentinel rules, the store queries (including the
`MaxOpenConns(1)` deadlock regression), the full authenticated HTTP flow, the
multipart parsing and upload storage of `Publish`, the creator-only rules of
`Publish` and `Delete`, and the all-routes inventory.

The three write handlers and their helpers are at 100% statement coverage. The
global figure is lower because the read-only handlers are exercised through the
HTTP layer while their store helpers keep unreachable error branches.

## License

ageAPIServer is released under the **GNU Affero General Public License v3.0
only** (AGPL-3.0-only). The full text is in `LICENSE`; every source file carries
its SPDX identifier.
