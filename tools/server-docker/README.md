## Docker

All commands must be run from the **repository root** (`ageAPIServer/`), the
same layout as the `tools/server-docker` tree of ageLANServer.

### Dockerfile

| Image                                              | Description                                                                                                          |
|----------------------------------------------------|----------------------------------------------------------------------------------------------------------------------|
| [Server](Dockerfile/server/Dockerfile)             | Main API server. Multi-stage: compiles with `golang:1.27-alpine3.24`, compresses with `upx`, runs on `alpine:3.24`.  |

The build accepts `VERSION` (default `development`) and stamps it into the
binary with `-X main.version`; the compressed binary is the same code the
release binaries carry, without the Windows 7 toolchain.

### Compose

All compose files live under `tools/server-docker/Compose/`. Run them with:

```sh
docker compose -f tools/server-docker/Compose/<file>.yml up
```

| File                          | Network | Description                                                        |
|-------------------------------|---------|--------------------------------------------------------------------|
| `server.bridge.yml`           | bridge  | Publishes `8080/tcp`; works on Linux, macOS and Windows.           |
| `server.host.yml`             | host    | Shares the host network stack; Linux only.                         |
| `server.proxy.bridge.yml`     | bridge  | nginx in front on `80/tcp` with the per-location upload limits.    |

The proxy variant exists because nginx defaults to a 1 MiB body: a direct
bridge deployment does not need it, but one behind nginx does. The same limits
are documented for a bare-metal proxy in `docs/nginx-ageapiserver.conf`.

### Environment variables

#### Server

| Variable       | Required | Default | Description                                                                 |
|----------------|----------|---------|-----------------------------------------------------------------------------|
| `SERVER_ARGS`  | No       | _(empty)_ | Extra flags for the binary, e.g. `--log.level=debug --matchcontent.ratelimitperminute=60`. |
| `VERSION`      | No       | `development` | Build arg stamped into the binary.                                      |

Every configuration key can also be overridden with `AGEAPISERVER_*`
(`AGEAPISERVER_SERVER_TLS_MODE=proxy`, `AGEAPISERVER_STORAGE_MAXMODFILEBYTES=0`,
…). The base include sets the container-friendly paths:

| Variable                          | Value                          |
|-----------------------------------|--------------------------------|
| `AGEAPISERVER_SERVER_ADDR`        | `:8080`                        |
| `AGEAPISERVER_DB_PATH`            | `/app/data/ageAPIServer.db`    |
| `AGEAPISERVER_STORAGE_UPLOADDIR`  | `/app/uploads`                 |
| `AGEAPISERVER_LOG_FILE`           | `/app/logs/ageAPIServer.log`   |

Add more with `environment:` in an include file or with
`docker compose run -e AGEAPISERVER_...`.

### Ports (bridge mode)

| Port    | Protocol | Service | Description                    |
|---------|----------|---------|--------------------------------|
| `8080`  | TCP      | server  | API (`/test`, `/cacert.pem`, `/openapi.yml`, mods, MatchContent) |
| `80`    | TCP      | nginx   | Only in `server.proxy.bridge.yml` |

### Volumes

| Volume name     | Mount point                          | Description                                                        |
|-----------------|--------------------------------------|--------------------------------------------------------------------|
| `config_server` | `/app/resources/config` (read-only)  | `config.toml`; initialized from the packaged one.                  |
| `certs`         | `/app/resources/certificates`        | The `cert.pem`/`key.pem` pair when `[Server.TLS] Mode = "on"`.      |
| `uploads`       | `/app/uploads`                       | Mod archives, screenshots and block staging (`--gc-uploads` scans it). |
| `logs`          | `/app/logs`                          | `ageAPIServer.log` and its rotation generation.                    |
| `data`          | `/app/data`                          | The SQLite database (`ageAPIServer.db` plus `-wal`/`-shm`).        |

### Examples

**Bridge, build and start:**
```sh
docker compose -f tools/server-docker/Compose/server.bridge.yml up --build
```

**Host network (Linux):**
```sh
docker compose -f tools/server-docker/Compose/server.host.yml up --build
```

**Behind nginx (upload body limits applied):**
```sh
docker compose -f tools/server-docker/Compose/server.proxy.bridge.yml up --build
```

**Extra flags:**
```sh
SERVER_ARGS="--log.level=debug" \
  docker compose -f tools/server-docker/Compose/server.bridge.yml up
```

**Collect orphaned uploads (server stopped):**
```sh
docker compose -f tools/server-docker/Compose/server.bridge.yml run --rm \
  --entrypoint /app/ageAPIServer server --gc-uploads --gc-apply
```
