# Plan de implementación — ageAPIServer

Estado: **listo para ejecutar** por un agente implementador. Fuente: revisión
externa verificada contra el código (rutas/ficheros citados comprobados) más
las restricciones oficiales de subida WEB aportadas por el dueño del repo.

Mapa de fases frente al informe original:

| Fase | Puntos del informe | Riesgo | Tamaño |
|---|---|---|---|
| 0 — Higiene | 3, 13, 14 (gates de CI) | cero | S |
| 1 — Correcciones | 1, 2, 4, 5, 6 + contrato WEB de Publish | bajo | L |
| 2 — Contrato spec | 10, 11, 12 | cero (solo tests) | M |
| 3 — Disco | 8, 16 | bajo-medio | M |
| 4 — Superficie nueva | 21, 22 | medio | M |
| 5 — Docs y proyecto | 9, 15, 18, 20 | cero técnico | M |
| — | 7, 17 y 19 excluidos por el dueño | — | — |

---

## Cómo ejecutar este plan

1. `AGENTS.md` manda sobre todo lo demás. Si algo de este documento
   conflicta con él, gana `AGENTS.md` y se reporta la discrepancia.
2. Ejecutar las fases **en orden 0 → 5**. Dentro de una fase, cada `N.M`
   es atómico: un commit por punto, con mensaje referenciando el punto
   (`1.6 publish: enforce the web upload contract for cookie publishes`).
3. Tras **cada punto** pasar el gate completo:

   ```bash
   CGO_ENABLED=0 go build ./...
   go vet ./...
   gofmt -l $(git ls-files '*.go')        # debe imprimir nada
   go test ./... -count=1
   ```

   Y en la última pasada de cada fase, además: `go test -race ./...`.
4. No se empieza un punto nuevo con el gate rojo. No se saltan fases: la
   4 toca `routeInventory` y depende del test de la 2; la 5.1 presupone
   la 1.6 (tamaños del proxy) y la 4.2 (rate limit de MatchContent).
5. Los pasos marcados **[USER]** paraban a preguntar; todos los que
   había están ya resueltos (licencia AGPL-3.0 en 5.3, sin `/metrics`,
   go-yaml v4 en 2.1, rate limit off por defecto). Si durante la
   ejecución aparece una decisión nueva equivalente, parar y preguntar;
   no improvisar decisiones de producto.
6. Órdenes de trabajo que caben en un PR propio van juntas, pero nunca
   mezcladas con otra fase.
7. Commit final de cada fase: verificar `git status` limpio y el gate
   verde antes de anunciar la fase como terminada.

## Invariantes — no romper bajo ninguna circunstatura

- **Las formas de respuesta están fijadas por capturas**: sin trailing
  newline, `Content-Type: application/json; charset=utf-8`, `null` y
  defaults tal cual. Los replays (`api_*_capture_test.go`) caen si se
  "limpia" una forma: son el guardián, no el obstáculo.
- **SQLite `MaxOpenConns(1)`** y cerrar `rows` antes de otra query. Nunca
  una segunda conexión ni cgo.
- **Rutas en minúsculas y sin barra final**; el patrón no codifica la
  capitalización del spec. Cambio de ruta = `routeInventory` +
  `api_spec.yml` + tabla de `README.md` a la vez.
- **Nunca loguear credenciales**; `internal/logger/redact.go` es el único
  punto de redacción. El access log lleva método, ruta, status y duración.
- **La versión viene del linker** (`-X main.version`), nunca hardcode.
- Config: cada clave nueva son tres superficies — campo en
  `internal/config.go`, clave comentada en `resources/config/config.toml`,
  y el override `AGEAPISERVER_*` / `--section.key` sale solo.
- `api_spec.yml` es el spec oficial verbatim: las rutas locales que no
  existen upstream (`/openapi.yml`) se manejan con el allowlist del
  test 2.1, nunca editando el spec.
- **El GC nunca borra por conteo de referencias**: los nombres de fichero
  son CRC32 del contenido y se comparten entre mods (ver 3.1).

---

## Contexto técnico que usa este plan (verificado en código)

- **Dos superficies de subida con reglas distintas** (restricciones
  oficiales de la web):
  - **WEB** (cookie `.AgeOfEmpiresServices`): título obligatorio,
    descripción obligatoria, de 1 a 5 imágenes con lista cerrada de
    formatos, hasta **7 tags** y el mod como `.zip` de hasta 1 GB.
  - **JUEGO** (ticket `Steam1.0`/`XBL3.0`): imágenes y descripción
    opcionales; AOM publica un `.zip` por multipart (`modFiles` en
    `Publish`); AOE4 no usa la web y su `.sga` llega **en bloques** por
    `PUT /api/v4/mods/CheckAndPublishFile`
    (`internal/routes/api/mods/v4/checkpublish.go`:
    `maxBlockSize = 512 MiB`, lista de hasta 50 000 ids).
  - Consecuencia de diseño: las reglas web se aplican **solo a publishes
    autenticados con cookie**; una regla global (".zip siempre")
    rompería el día que un capture nuevo traiga otro cliente de juego.
- **Hoy `Publish` no puede recibir un mod real**: `Validate()`
  (`internal/routes/shared/publish.go:315`) solo exige `gameTitleId`,
  `modName` y `modVisibility`; ni `publish` ni `/api/v1/mods/create`
  están en `exemptBodySizePrefixes`, así que el tope global
  `Server.MaxBodyBytes` (1 MiB) corta la subida en
  `internal/routes/router/maxBodySizeMiddleware.go`.
- Los ficheros se nombran `<crc32><ext>` (`shared.SaveUpload`), así que
  dos mods pueden compartir fichero.
- `models.Viewer` (`internal/routes/router/authMiddleware.go:Resolve`)
  hoy no distingue el esquema de credencial: campo nuevo en 1.6.
- `limitedWriter` + `ErrFileTooLarge` ya existen en
  `internal/models/customFiles.go`: patrón a reutilizar en 1.6.
- `internal/cmd/deps.go` expone seams (`openStoresFn`/`closeStoresFn`)
  para testear el arranque sin listener ni fichero de DB real.

---

## Fase 0 — Higiene mecánica

### 0.1 · `go mod tidy` (punto 3)

- [ ] Ejecutar `go mod tidy` y revisar el diff de `go.mod`: debe aparecer
      un bloque `require` directo con `github.com/spf13/pflag`, los seis
      koanf importados (`knadh/koanf/v2`, `parsers/toml/v2`,
      `providers/confmap`, `providers/env`, `providers/file`,
      `providers/posflag`) y `modernc.org/sqlite`; `// indirect` queda
      solo en los indirectos reales. `go.sum` sin cambios de versión.
- [ ] Verificar: `go mod tidy -diff` termina 0 (sin salida).

### 0.2 · Unificar acciones de GitHub (punto 13)

- [ ] `.github/workflows/ci.yml`: `actions/checkout@v4` → `@v7`,
      `actions/setup-go@v5` → `@v7` (mismas que `release.yml`).

### 0.3 · Gates nuevos en `ci.yml` (punto 14)

- [ ] gofmt alineado con AGENTS.md: sustituir
      `test -z "$(gofmt -l .)" || (gofmt -l . && exit 1)` por
      `test -z "$(gofmt -l $(git ls-files '*.go'))" || exit 1` — el
      `gofmt -l .` actual también recorre `.tools/` (ignorado).
- [ ] Paso `tidy`: `go mod tidy -diff`.
- [ ] Pasos del módulo anidado (`working-directory: tools/scripts`):
      `go vet ./...` y `go build ./...`.
- [ ] Paso `goreleaser check`: `cd tools/scripts && go run
      ./cmd/generateGoreleaserConfig.go /tmp/go-legacy` y luego
      `goreleaser check` (el path del toolchain no se resuelve en
      `check`; si la versión instalada de goreleaser rechazara el campo
      `tool:`, mover el paso a `release.yml` y documentarlo en el PR).
- [ ] Cobertura: añadir `-covermode=atomic -coverprofile cover.out` al
      paso test y subir `cover.out` con `actions/upload-artifact`.
      (Publicar porcentaje en un indicador externo: fuera de alcance.)
- [ ] Crear `.github/dependabot.yml`: `package-ecosystem: gomod` y
      `github-actions`, ambos `interval: weekly`.

**Aceptación de fase:** un push a rama con CI verde y los gates activos.

---

## Fase 1 — Correcciones puntuales de código

### 1.1 · CORS anuncia los métodos que se sirven (punto 1)

**Archivos:** `internal/routes/router/corsMiddleware.go`,
`internal/routes/router/corsMiddleware_test.go`, `api.go`.

- [ ] `CORSConfig` gana el campo:

  ```go
  // AllowedMethods is what Access-Control-Allow-Methods answers. Empty
  // keeps the historical "GET, POST, OPTIONS" so a host application
  // building its own chain sees no change.
  AllowedMethods []string
  ```

- [ ] En `CORS()`: `methods := cfg.AllowedMethods`; si vacío, literal
      `"GET, POST, OPTIONS"`; si no, `strings.Join` con `", "`.
- [ ] En `api.go` `New()`, junto al `router.Chain`, derivarlos del
      inventario: set único de `r.Method` en minúsculas de
      `routeInventory`, ordenado por lista fija
      `get, post, put, delete`, y siempre con `"OPTIONS"` al final.

  ```go
  methods := inventoryMethods(routeInventory) // p.ej. "GET, POST, PUT, OPTIONS"
  ```

- [ ] Tests: caso nuevo en `corsMiddleware_test.go` con
  `AllowedMethods` explícito; en el root, un preflight cuya cabecera
  `Access-Control-Allow-Methods` contenga `PUT` (calculada con el mismo
  helper `inventoryMethods`, no un literal duplicado).

### 1.2 · Nivel de log por línea (punto 2)

**Archivos:** `internal/logger/log.go`, `internal/logger/log_test.go`.

- [ ] `write` pasa a `func write(level slog.Level, prefix, format string, a ...any)`
      y el fichero recibe `fileLogger.Log(level, message, "prefix", prefix)`.
- [ ] Mapa de funciones existentes → nivel: `Ok, Info, Detail, Section,
  KV, Banner` → `slog.LevelInfo`; `Warn` → `slog.LevelWarn`; `Fail, Fault`
  → `slog.LevelError`. La consola no cambia: imprime todo, igual que hoy.
- [ ] Nueva `func Debug(format string, a ...any)` con prefijo
      `[ .. ]` y `slog.LevelDebug`, para las líneas que hoy no existen
      porque no había nivel donde ponerlas.
- [ ] Actualizar el comentario de `Log.Level` en
      `resources/config/config.toml`: "nivel mínimo escrito en el
      fichero; la consola muestra todo".
- [ ] Tests: con `Level=error` el fichero contiene la línea `Fail` y
      ninguna `Info`; con `Level=debug` aparecen las `Debug`; el
      console buffer es idéntico entre niveles.

### 1.3 · `--version` (punto 4)

**Archivos:** `internal/cmd/root.go`, `cmd/ageAPIServer/main.go`,
`internal/cmd/root_test.go` (nuevo).

- [ ] `Execute` cambia a `Execute(args []string) (error, int)`;
      `main.go` llama `cmd.Execute(os.Args[1:])`. El `pflag.NewFlagSet`
      parsea `args` en vez de `os.Args[1:]`.
- [ ] Registrar `fs.Bool("version", false, "print the version and exit")`.
- [ ] Inmediatamente tras `fs.Parse` (y **antes** de `i.Load` y de
      `OpenMainFileLog`): si `--version`, `fmt.Println("ageAPIServer " + Version)`
      y `return nil, 0`. Sin fichero de config, sin log file.
- [ ] Test: `Execute([]string{"--version"})` → `(nil, 0)` y stdout
      contiene `Version` actual; con flag inválido → `ErrConfig`.

### 1.4 · Eliminar `Session.SameSite` (punto 5)

**Archivos:** `internal/config.go`, `internal/cmd/root.go`,
`internal/routes/shared/deps.go`, `resources/config/config.toml`,
tests que lo mencionen.

- [ ] Borrar en un único commit: campo `Session.SameSite` y su default
      `Lax`; el `switch` de validación; `Config.SameSiteMode()`; el
      flag `--session.sameSite`; el campo `Deps.SameSite` (nunca se
      llenaba: el server no escribe cookies); la entrada de
      `config.toml`.
- [ ] `grep -rn -i samesite` tras el cambio: cero resultados en `.go`
      y `config.toml` (el README y este plan pueden mencionarlo como
      histórico).
- [ ] Nota para el PR: una clave `sameSite` residual en el fichero de
      un usuario se ignora (koanf no rompe con claves desconocidas);
      el README registra la eliminación.

### 1.5 · `http.Server.ErrorLog` al logger (punto 6)

**Archivos:** `internal/logger/log.go`, `internal/cmd/root.go`,
`internal/logger/log_test.go`.

- [ ] En logger:

  ```go
  // StdLogger adapts the process logger to *log.Logger for
  // http.Server.ErrorLog: net/http handshake errors belong in the log
  // file, not in raw stderr.
  func StdLogger() *log.Logger {
      return log.New(&stdWriter{}, "", 0)
  }
  ```

  con `stdWriter.Write(p []byte)` que trimea y llama a
  `write(slog.LevelWarn, "[HTTP]", "%s", trimmed)`. Sin redacción
  extra: esas líneas no llevan cabeceras, pero pasan por el mismo
  pipeline por coherencia.
- [ ] `root.go`: `httpServer := &http.Server{ Handler: ..., ReadHeaderTimeout: ..., IdleTimeout: ..., ErrorLog: logger.StdLogger() }`.
- [ ] Test: unidad de `stdWriter` (una línea → `write` con `Warn`).
      Integration opcional: puerto ocupado → la línea `http:` aparece en
      el buffer del logger de test.

### 1.6 · El contrato de la subida WEB en `Publish` (restricciones oficiales)

**Archivos:** `internal/models/models.go` (Viewer), `internal/routes/router/authMiddleware.go`,
`internal/config.go`, `resources/config/config.toml`, `internal/cmd/root.go`,
`internal/routes/router/maxBodySizeMiddleware.go`, `internal/routes/shared/publish.go`,
`internal/routes/shared/deps.go`, `api.go`, `internal/routes/api/mods/v4/publish.go`,
tests root + shared.

- [ ] **`Viewer.Scheme`** (aditivo): en `models.Viewer` añadir
      `Scheme string` con constantes exportadas `models.SchemeSteam`,
      `models.SchemeXBL`, `models.SchemeCookie` (y `""` = anónimo).
      `Authenticator.Resolve` rellena el campo en cada rama. Sin cambios
      de wire.
- [ ] **Exención de cuerpo**: añadir `/api/v4/mods/publish` y
      `/api/v1/mods/create` a `exemptBodySizePrefixes`
      (`maxBodySizeMiddleware.go`), con comentario: el mod viaja en el
      multipart y el tope lo aplica el propio flujo al copiar a disco.
- [ ] **Tope al hacer stream**, no por el tamaño declarado (el
      multipart miente): clave `Storage.MaxModFileBytes int64` (tres
      superficies; default `1 << 30` GiB; `0` = sin tope; validar
      `>= 0`). `shared.Deps` gana `MaxModFileBytes int64` y `api.go` la
      rellena desde `Config`. `SaveUpload` cambia a
      `SaveUpload(dir string, header *multipart.FileHeader, max int64)`:
      con `max > 0` envuelve el origen en un `limitedWriter` (patrón e
      `ErrFileTooLarge` ya existen en `internal/models/customFiles.go`;
      se sube una copia pequeña a `shared` o se exporta — elegir lo
      menos invasivo). Callers pasan `max`: `v4/publish.go`,
      `v1/create.go`, `shared.PublishImages` (firma +1 param).
- [ ] **Reglas WEB solo con cookie** en el handler v4 de `Publish`,
      después de `ParsePublish`, mirando
      `viewer.Scheme == models.SchemeCookie`:
  - `description` obligatoria (tras `decodeLocalized`);
  - entre **1 y 5** imágenes: rango sobre `len(form.ImageMeta)`
    emparejadas con fichero;
  - **hasta 7 tags** (`len(form.TagIDs)`, los `modTags` ya parseados
    por `tagFields`, que hoy no impone límite — las capturas mandan
    1–2): máximo, sin mínimo obligatorio;
  - extensión del mod `.zip` y de cada imagen dentro de la lista
    oficial (constante en `shared`):

    ```go
    // webImageExtensions is the list the official website file picker
    // accepts, verbatim from its banner.
    var webImageExtensions = map[string]bool{
        "apng": true, "avif": true, "bmp": true, "gif": true, "heic": true,
        "heif": true, "ico": true, "jpe": true, "jpeg": true, "jpg": true,
        "jxl": true, "pjpeg": true, "pjp": true, "png": true, "svg": true,
        "svgz": true, "tif": true, "tiff": true, "webp": true, "xbm": true,
    }
    ```

    - errores con `shared.WriteError(w, http.StatusBadRequest, shared.CodeBadRequest, mensaje)`
      (misma envolvente que el resto; la web no tiene capturas):
      "description is required", "between 1 and 5 images are required",
      "at most 7 tags are allowed",
      "unsupported image format", "mod file must be a .zip".
- [ ] **`Validate` no cambia de firma para tickets**: las reglas web se
      aplican en el handler v4, no dentro de `PublishForm.Validate`
      (que no ve el viewer). El camino de ticket queda exactamente como
      hoy — sin tope de imágenes, extensión libre —, que es lo que
      codifican los replays; AOE4 sube su `.sga` por
      `CheckAndPublishFile`, otra ruta que no se toca.
- [ ] Tests (root, en `api_mods_write_test.go` o un
      `api_web_publish_test.go` nuevo; helper que autentique con cookie
      creando la sesión por un segundo handle de stores, al estilo del
      `--new-session` de `internal/cmd`):
  - cookie sin descripción → 400;
  - cookie con 6 imágenes → 400;
  - cookie con **8 tags** → 400;
  - cookie con imagen `.exe` → 400;
  - cookie con mod `.sga` → 400;
  - cookie con `.zip` dentro del tope → publica OK;
  - fichero mayor que `MaxModFileBytes` (configurarlo pequeño en el
    test) → 400 de tamaño, no 500, y sin fichero huérfano en
    `UploadDir`;
  - mismo cuerpo con ticket `steamAuth()` → sigue publicando
    (regresión del replay: los tickets no ven las reglas web);
  - 0 imágenes con cookie → 400; 0 imágenes con ticket → OK (así
    publica hoy el juego);
  - 8 tags con ticket → OK (el juego no tiene tope de tags).

**Aceptación de fase:** gate verde + `go test -race ./...`; ningún
test de replay modificado salvo para pasar el nuevo parámetro de
`SaveUpload`/`PublishImages` (mecánico).

---

## Fase 2 — Convierte el spec en contrato probado

### 2.1 · Test `routeInventory` ↔ `api_spec.yml` (punto 10)

**Archivos:** `spec_inventory_test.go` (nuevo, root), `go.mod`
(dependencia de tests).

- [ ] `go get go-yaml v4` (dependencia solo-para-tests, decisión del
      dueño ya tomada; parsear 3 100 líneas de YAML con regex es la
      alternativa frágil).
- [ ] Parsear `paths`:

  ```go
  type specFile struct {
      Paths map[string]map[string]struct{ OperationID string `yaml:"operationId"` } `yaml:"paths"`
  }
  ```

  Quedarse solo con claves de método http (`get`, `post`, `put`,
  `delete`...). Normalizar cada path: `strings.ToLower` +
  `strings.TrimSuffix(p, "/")` (mismo criterio que `router.Normalize`).
- [ ] Comparar **en las dos direcciones** contra
      `{(r.Method, normalize(r.SpecPath))}` de `routeInventory`:
  - cada entrada del inventario existe en el spec;
  - cada operación del spec tiene entrada en el inventario.
- [ ] Allowlist de extensiones locales junto al test:

  ```go
  // inventoryOnly are served by this server but are not in the official
  // spec: they are local Ops additions. Phase 4 adds /openapi.yml here.
  var inventoryOnly = map[string]bool{}
  ```

  Las entradas se consultan como `inventoryOnly[method+" "+path]`.
- [ ] Si la comparación inicial falla: es un bug real del repo
      (inventario/spec desincronizados). Corregir whichever lado falle,
      contar la decisión en el PR. No amañar el test con excepciones
      nuevas.

**Aceptación:** `go test -run TestSpec -count=1 .` verde en ambas
direcciones; la afirmación del README queda probada.

### 2.2 · Tests de `internal/cmd` (punto 11)

**Archivos:** `internal/cmd/root_test.go` (nuevo), refactor menor de
`internal/cmd/root.go`.

- [ ] Seccionar `runRoot` en funciones puras por fase (se usan desde
      tests sin arrancar listener): las ya existentes
      (`certificatePair`, `newSession`, `tlsDescription`) bastan como
      corte; solo extraer si algún test lo pide.
- [ ] Tests:
  - `--version` (ver 1.3);
  - `--new-session <token>` con `openStoresFn` real sobre DB in-memory
    (`initializer.Initialize("", cfg.DB.BusyTimeoutMs, cfg.DB.MaxOpenConns)`)
    y un usuario sembrado por un segundo handle: imprime
    `"%s=%s\n" cookieName, token` más `expires:`;
  - `certificatePair`: modo `off` (no toca disco), `on` con par PEM
    auto-firmado generado con `crypto/x509` en `t.TempDir()`;
  - códigos de salida: flag inválido → `ErrConfig`; log file imposible
    (`--log.file` en un directorio inexistente) → `ErrCreateLogFile`;
    cert inválido en modo `on` → `ErrCertificate`.

### 2.3 · Cobertura barata (punto 12)

- [ ] `internal/config_test.go`: tabla sobre `Validate` — un caso de
      error por campo (`Server.Addr` vacío, `MaxBodyBytes` 0, duraciones
      inválidas, enums inválidos de TLS.Mode/Log.Level/Pagination.Base,
      `MaxOpenConns` 0, `Session.CookieName` vacío) más el caso válido.
- [ ] `internal/logger/log_test.go`: `ParseLevel` (4 valores + default),
      `OpenMainFileLog` con `Enabled=false` no crea fichero, `Close`
      idempotente.

**Aceptación de fase:** `go test -race ./...` verde; cobertura de
`internal/cmd` (verla con `go test ./internal/cmd -cover`) > 80%.

---

## Fase 3 — Operación de disco

### 3.1 · `--gc-uploads` (punto 8)

**Archivos:** `internal/cmd/root.go`, `internal/cmd/deps.go`,
`internal/cmd/gc.go` (nuevo), `internal/cmd/gc_test.go` (nuevo),
`resources/config/config.toml` (solo docs del flag en README).

- [ ] Flags: `--gc-uploads` (bool, recolecta y **reporta**, dry-run) y
      `--gc-apply` (bool, borra lo recolectado; ignorado sin
      `--gc-uploads`). Corren tras cargar config y **antes** de abrir
      listener, con el server parado documentado como requisito
      (carrera con subidas en vuelo: por eso los temporales llevan
      condición de edad).
- [ ] Algoritmo (`gc.go`, función pura para testear directo):
  1. Referencias (unión, nunca conteo):

     ```sql
     SELECT COALESCE(fileUrl, '') FROM mods;          -- NULL → ''
     SELECT COALESCE(imageName, '') FROM mod_images;  -- idem
     SELECT modId FROM mods;                          -- set existente
     ```

  2. `filepath.WalkDir(UploadDir)`:
     - raíz: ficheros cuyo nombre **no** está en la unión de
       referencias → garbage;
     - `blocks/<modId>`: directorio con `modId` fuera del set → todo el
       directorio es garbage (commit que nunca llegó o mod borrado);
     - `upload-*` (temporales de `newUploadTemp`): garbage solo si
       `mtime` > 1h (una subida viva no se toca);
     - `custom-files/` (MatchContent): **fuera** — esa tienda ya
       expira sola (`purgeStale`/`evictLocked`).
  3. Reporte: una línea `logger.Info` por entrada y resumen
     `"%d files, %s" count, humanize.Bytes(size)`
     (`go-humanize` ya es dependencia).
  4. Con `--gc-apply`: `os.Remove`/`os.RemoveAll` de lo recolectado;
     error por entrada no aborta el resto (log + seguir), salida
     `ErrDatabase` solo si la DB no abre.
- [ ] **Nunca** código de borrado dentro de `ModStore.Delete`: los
      nombres son CRC y se comparten entre mods.
- [ ] Tests (`gc_test.go`, con DB real en `t.TempDir()` y UploadDir
      temporal, sembrados con un segundo handle como hace el root):
  - mod publicado y borrado → su fichero y sus imágenes son garbage;
  - mismo contenido publicado por dos mods, uno borrado → el fichero
    **no** es garbage (sigue referenciado);
  - `blocks/<modId>` con modId inexistente → garbage completo;
  - `upload-*` fresco → no garbage; con mtime vieja → garbage;
  - dry-run no toca disco; `--gc-apply` borra exactamente lo reportado.

### 3.2 · Rotación del log por tamaño (punto 16)

**Archivos:** `internal/logger/log.go`, `internal/config.go`,
`resources/config/config.toml`, `internal/cmd/root.go`,
`internal/logger/log_test.go`.

- [ ] Clave `Log.MaxSizeBytes int64` (tres superficies; default `0` =
      sin límite = comportamiento actual; validar `>= 0`).
- [ ] En `OpenMainFileLog`: si el fichero existe y
      `stat.Size() > MaxSizeBytes` (> 0), renombrar a `path + ".old"`
      (sobrescribe) y abrir nuevo. Una generación de respaldo, comprobada
      solo en arranque: sin goroutines ni watchers.
- [ ] README (sección Operation de la fase 5): receta `logrotate`
      unix y tarea programada de Windows para rotación más rica.
- [ ] Tests: fichero pre-llenado > límite → al abrir existe el `.old`;
      límite 0 → nada cambia; sin fichero previo → creación normal.

**Aceptación de fase:** gate verde + `-race`; `--gc-uploads` sobre un
`uploads/` con fixture reporta exactamente los huérfanos.

---

## Fase 4 — Superficie nueva opt-in

Toda ruta nueva entra con: entrada en `routeInventory`, fila en la tabla
de endpoints del `README.md`, alta en el allowlist `inventoryOnly` del
test 2.1. El spec `api_spec.yml` no se edita.

### 4.1 · `GET /openapi.yml` (punto 22)

**Archivos:** `openapi.go` (nuevo, root), `api.go`,
`httptest_all_routes_test.go`, README.

- [ ] `//go:embed api_spec.yml` en el paquete raíz (el fichero ya viaja
      en los releases; el binario lo sirve sin depender del CWD, ~100 KB
      embebidos).
- [ ] Handler: escribe los bytes (sin cambios) con
      `Content-Type: application/yaml; charset=utf-8`.
- [ ] `routeInventory`:
      `{Method: "GET", Path: "/openapi.yml", SpecPath: "/openapi.yml"}`
      sin `AuthRequired`; alta en `inventoryOnly` del test 2.1.
- [ ] README: fila "Ops" junto a `/test` (anónima; sirve para que
      un integrador genere clientes sin desempaquetar el release).

### 4.2 · Rate limit en `/api/MatchContent/*` (punto 21)

**Archivos:** `internal/config.go`, `resources/config/config.toml`,
`internal/cmd/root.go`, `api.go` (Config), `internal/routes/matchcontent/`
(middleware), `matchcontent_test.go`.

- [ ] Clave `MatchContent.RateLimitPerMinute int` (tres superficies;
      default `0` = desactivado — decisión tomada según el original:
      ningún capture muestra un 429, así que un límite por defecto
      inventaría wire que el upstream nunca mostró; el operador lo
      activa con la clave). Se aplica solo a los endpoint HTTP de
      MatchContent (anónimos por diseño); las subidas de mods no
      entran.
- [ ] Implementación stdlib puro (sin `golang.org/x/time/rate` solo
      para esto): token bucket por IP (`host(addr)`, sin el puerto de
      `RemoteAddr`), refill continuo, mapa pequeño con `sync.Mutex` y
      limpieza de IPs dormidas (purga descriptible por tiempo desde el
      último uso). El bucket incluso por hora, no minuto: token burst.
- [ ] Respuesta: `shared.WriteError(w, http.StatusTooManyRequests,
      shared.CodeTooManyRequests, "too many requests")` + cabecera
      `Retry-After` en segundos. Revisar si `shared` ya tiene código
      para 429; si no, añadirlo a la envolvente existente en `shared`.
- [ ] Tests: límite N en el config → N requests 200/(upload) y la
      N+1 → 429 con `Retry-After`; desde OTRA IP → pasa (aislamiento
      por IP); límite 0 → sin 429 nunca (regresión).

**Aceptación de fase:** `httptest_all_routes_test.go` verde con la ruta
nueva (`/openapi.yml`); `go test -race ./...` verde.

---

## Fase 5 — Documentación, empaquetado y proyecto

### 5.1 · README de operación (puntos 9, 20 + contexto 1 GB)

**Archivos:** `README.md`.

- [ ] Sección "Operation" con:
  - **Bases de recursos** (punto 9): tabla de qué se resuelve contra el
    CWD (`resources/config/config.toml`, `*.log`, `ageAPIServer.db`,
    `uploads`, carpeta MatchContent) y qué contra el directorio del
    ejecutable (`resources/certificates/{cert,key}.pem`). No se
    unifican: es el layout deliberado de la familia ageLANServer.
  - **Detrás de un reverse proxy** (punto 20): recomendación loopback y
    los tres límites de cuerpo de nginx por location (los define 1.6):
    `location /api/v4/mods/checkandpublishfile` →
    `client_max_body_size 512m;`; `location /api/v4/mods/Publish` (y
    `/api/v1/mods/Create`) → `client_max_body_size 1100m;` (zip 1 GB +
    multipart); `location /api/matchcontent` →
    `client_max_body_size 36m;` (32 MiB + slack);
    `proxy_read_timeout`/`proxy_send_timeout` altos por los mismos
    cuerpos. Nota explícita: `X-Forwarded-*` se confía sin condición y
    solo construye URLs públicas — no es un bypass de auth por sí solo.
    La lista de proxies de confianza queda como future work.
  - **Subida WEB vs juego**: tabla de las dos superficies (las reglas y
    tamaños de 1.6: título+descripción, 1–5 imágenes con formatos,
    ≤7 tags, `.zip` ≤ 1 GB — quién aplica cada una y qué esperar en
    `Publish`).
  - `Log.MaxSizeBytes` y recetas de rotación externa (de 3.2).

### 5.2 · Recetas de despliegue (punto 18)

**Archivos:** `Dockerfile`, `compose.yml`, `docs/systemd-ageapiserver.service`.

- [ ] `Dockerfile`: build multi-stage con `CGO_ENABLED=0`, binary a
      distroless/static, usuario no-root, `VOLUME`/documentación de
      `ageAPIServer.db`, `uploads/`, `resources/config/`.
- [ ] `compose.yml`: servicio (binario o build), healthcheck sobre
      `/test`, volúmenes persistentes, y el ejemplo de nginx con los
      `client_max_body_size` por location de 5.1 (**un compose sin eso
      se rompe con el primer mod grande, venga del juego o de la web**).
- [ ] `docs/systemd-ageapiserver.service`: `EnvironmentFile`
      (`AGEAPISERVER_*` cubre cualquier override), `WorkingDirectory`
      explícito (por las bases del punto 9), `Restart=on-failure`.
- [ ] Las releases oficiales (Taskfile/GoReleaser, builds win7 incluidos)
      no cambian ni una línea: Docker es un extra Linux.

### 5.3 · LICENSE, SECURITY.md, CONTRIBUTING.md (punto 15)

- [ ] `LICENSE`: **AGPL-3.0-only, decisión del dueño ya tomada**.
      Fichero con el texto completo de la licencia, sección "License"
      en el README y cabecera de una línea
      `// SPDX-License-Identifier: AGPL-3.0-only` al inicio de cada
      `.go` (mecánico, `gofmt` no se ve afectado). El texto de la
      licencia se copia íntegro del canonical de fsfe/gnu, sin resúmenes.
- [ ] `SECURITY.md`: contacto para informes (el del perfil del dueño) y
      el estado real, sin adornos: los tickets Steam/XBL **no** se
      verifican (nunca describirlo como seguro), `X-Forwarded-*` sin
      lista de confianza.
- [ ] `CONTRIBUTING.md`: extraer de `AGENTS.md` lo que aplica a
      humanos: comandos, gates, regla inventory+spec+README y el
      principio del repo — las formas de respuesta están fijadas por
      capturas: se reproduce el byte, no se "mejora" la forma.

---

## Definición de "hecho" del plan completo

- [ ] Fases 0–5 con el gate verde en cada una
      (`go test -race ./...` incluido).
- [ ] `api_spec.yml` sin cambios (salvo que 2.1 destape un bug previo).
- [ ] Ningún test de replay modificado salvo lo mecánico
      (firmas `SaveUpload`/`PublishImages` en 1.6).
- [ ] README con: tabla de endpoints al día, sección Operation,
      tablas web-vs-juego, sección License.
- [ ] `routeInventory` ↔ spec probado por `spec_inventory_test.go`,
      con `/openapi.yml` en `inventoryOnly`.
- [ ] Todas las decisiones de producto del plan resueltas (AGPL-3.0,
      sin `/metrics`, go-yaml v4, rate limit off por defecto): el
      implementador no asume ninguna nueva; si surge, pregunta.

## Fuera de este plan (con su porqué)

- **7 timeouts de body**: un `WriteTimeout` corta descargas de mods de
  1 GB en conexiones lentas y `net/http` no permite timeouts por ruta;
  el proxy de 5.1 es la mitigation operativa.
- **17 `/metrics` Prometheus**: decisión del dueño — no se añade la
  dependencia al binario ni la ruta. Si algún día hace falta, la
  aplicación host que embebe la librería puede montar su propio
  exporter envolviendo `Routes()`.
- **19 verificación de tickets**: contradice el diseño del proyecto
  (AGENTS.md) y los replays de captura.
- **Trusted proxies** (de 20), **CodeQL** (de 14), **indicador externo
  de cobertura** (de 14) y un **GC programado** (de 3.1): future work
  enunciado en el README.
