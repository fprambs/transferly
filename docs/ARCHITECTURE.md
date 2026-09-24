# Arquitectura de Sentinel

Cliente web autoalojado para FTP, FTPS y SFTP: workspaces multiusuario, vault de credenciales cifrado y transferencias simultáneas.

> Idioma: este documento y el roadmap están en español; el desarrollo (código, comentarios, guías, ADR) va en inglés.

## 1. Principios

1. **Rápido**: streaming de extremo a extremo, pipelining SFTP, pools de conexiones, UI virtualizada.
2. **Resiliente**: las conexiones se caen; las transferencias se reanudan.
3. **Fácil de usar**: doble panel, drag and drop, menús contextuales, atajos de teclado.
4. **Seguro por defecto**: secretos cifrados, host keys verificadas, protección SSRF, mínimo privilegio.
5. **Extensible**: protocolos tras una sola interfaz; de un nodo a varios sin reescribir.
6. **Todo en Docker**: el host solo necesita Docker (Compose v2) y git.
7. **Web primero, listo para escritorio**: ver [ADR-0001](adr/0001-web-first-desktop-ready.md).
8. **Global por diseño**: todo texto de usuario es traducible; todo instante es UTC.
9. **Seguridad verificada**: cada fase termina con una revisión de seguridad ([docs/security](security/README.md)).

## 2. Modelo de ejecución

Producción:

```sh
git clone <repo> && cd <repo>
docker compose up -d
```

- Sin configuración obligatoria: los secretos de la instancia se generan en el primer arranque y se guardan en el volumen `data`.
- Las migraciones se aplican al arrancar. Un asistente de primer arranque crea al administrador.
- `SENTINEL_DOMAIN` más `--profile https` activa Caddy con TLS automático.

Desarrollo:

```sh
docker compose -f compose.dev.yaml up
docker compose -f compose.dev.yaml run --rm tools <comando>
```

- El contenedor `tools` reúne Go, Node, pnpm, sqlc, goose, oapi-codegen, golangci-lint, govulncheck y air.
- Las cachés viven en volúmenes nombrados; los contenedores corren con `HOST_UID:HOST_GID`.
- Un Dev Container conecta VS Code a `tools`. Los targets de `make` son atajos opcionales.

## 3. Stack

| Capa | Elección |
|---|---|
| Backend | Go, `chi`, `pkg/sftp`, `x/crypto/ssh`, `jlaffaye/ftp`, `coder/websocket`, `log/slog` |
| Datos | PostgreSQL 17, `pgx/v5`, `sqlc`, `goose` |
| Contrato | OpenAPI 3.1 (spec primero): `oapi-codegen` para Go, `openapi-typescript` para TypeScript |
| Frontend | React 19, TypeScript, Vite, Tailwind CSS v4, shadcn/ui, lucide-react |
| Estado cliente | TanStack Query (servidor), Zustand (UI), TanStack Router |
| Listas | TanStack Table, TanStack Virtual, react-arborist, dnd-kit |
| Formularios | react-hook-form, zod |
| i18n | i18next, react-i18next, `Intl`, `golang.org/x/text` |
| Editor | CodeMirror 6 |
| Tests | `testing`, testcontainers, Vitest, Testing Library, MSW, Playwright, axe |
| Infra | Docker multi-stage, distroless, Compose, Caddy |

Versiones fijadas: [guides/README.md](guides/README.md).

## 4. Núcleo portable

Arquitectura hexagonal: el núcleo es Go puro y no conoce HTTP ni el almacenamiento.

```
web:      transport/httpapi  ->  core  ->  store/postgres, remotefs/{sftp,ftp}
escritorio: transport/wails  ->  core  ->  store/sqlite,   remotefs/{sftp,ftp,local}
```

Reglas, verificadas en CI:

1. `core/...` no importa `net/http`, `pgx`, `database/sql`, Wails, adaptadores, transportes ni `app` (depguard).
2. Los casos de uso son servicios del núcleo con DTOs propios; handlers y bindings solo traducen.
3. El actor viaja siempre explícito en `context.Context`.
4. El SQL del store se mantiene portable; lo específico de Postgres (`SKIP LOCKED`, `LISTEN/NOTIFY`) queda tras `JobQueue` y `EventBus`.
5. El núcleo publica en `EventSink`; el transporte entrega los eventos (WebSocket o Wails).
6. Los archivos locales son un adaptador `RemoteFS` (`localfs`, solo escritorio).
7. El tiempo sale de `ports.Clock`; configuración y secretos llegan por interfaces.

## 5. Estructura del repositorio

```
api/openapi.yaml
backend/
  cmd/sentinel/                 binario web
  cmd/sentinel-desktop/         futuro binario Wails
  internal/
    core/{ports,auth,workspace,authz,vault,site,remotefs,session,transfer,netguard,audit}
    adapters/{remotefs/{sftpfs,ftpfs,localfs,remotefstest},store/{postgres,sqlite,storetest},clock,eventbus}
    transport/{httpapi,wails}
    app/  config/  testutil/  docscheck/
  migrations/postgres/  queries/postgres/
frontend/src/
  app/  components/ui/  features/{auth,vault,workspaces,sites,explorer,transfers}
  lib/{transport,api,ws,platform,datetime}/  i18n/locales/<idioma>/<namespace>.json
docker/  testenv/  .devcontainer/  .githooks/  docs/
compose.yaml  compose.dev.yaml  compose.test.yaml
```

## 6. Modelo de datos

```
users --< memberships >-- workspaces --< sites --< site_secrets
  |                            |--< folders
  |                            |--< known_hosts
  |                            |--< transfer_jobs --< transfer_items
  |                            '--< audit_events
  '--< user_sessions
```

| Tabla | Columnas clave |
|---|---|
| `users` | id (UUIDv7), email, password_hash (argon2id), locale (BCP 47), timezone (IANA), totp_secret_enc |
| `user_keys` | user_id, kdf_params, kdf_salt, wrapped_user_dek, par de claves X25519 |
| `workspaces` | id, name, kind (`personal`, `team`), owner_id, settings (jsonb) |
| `memberships` | workspace_id, user_id, role, wrapped_workspace_dek |
| `sites` | id, workspace_id, folder_id, protocol, host, port, username, auth_method, list_timezone (IANA), options (jsonb) |
| `site_secrets` | site_id, kind, nonce, ciphertext, dek_version |
| `known_hosts` | workspace_id, host, port, key_type, fingerprint_sha256 |
| `transfer_jobs` / `transfer_items` | direction, src, dst, status, priority, bytes_done, attempts, checksum |
| `audit_events` | workspace_id, user_id, action, target, ip, meta (jsonb), at (timestamptz) |

Toda consulta de negocio filtra por `workspace_id` y pasa por RBAC.

## 7. Vault de credenciales

```
contraseña maestra --Argon2id(salt, m=64MiB, t=3, p=1)--> KEK
KEK --AES-256-GCM (wrap)--> DEK del usuario (workspace personal)
DEK del usuario --wrap--> clave privada X25519
DEK del workspace --sellada con la clave pública de cada miembro--> memberships.wrapped_workspace_dek
DEK --AES-256-GCM(nonce, AAD=site_id|kind|dek_version)--> site_secrets
```

- La contraseña de login y la maestra son secretos distintos.
- Cambiar la maestra solo re-envuelve la DEK.
- Quitar a un miembro rota la DEK del workspace y re-cifra sus secretos en segundo plano.
- Los parámetros KDF se guardan por usuario y se suben de forma transparente al desbloquear.
- Se genera un recovery kit en el setup; sin él, perder la maestra pierde los secretos.
- Las DEK viven en un key ring en memoria con TTL y bloqueo por inactividad; nunca se persisten ni se registran en logs.
- La API nunca devuelve secretos, solo si existen.
- Límite: el servidor necesita los secretos en claro para conectarse, así que no es zero-knowledge.

## 8. Autenticación y autorización

- Email y contraseña (Argon2id), TOTP y passkeys; OIDC más adelante.
- Cookie de sesión opaca (`HttpOnly`, `Secure`, `SameSite=Lax`), token CSRF en mutaciones, rate limit en login y unlock.
- Nunca se deriva la IP del cliente de cabeceras de proxy no confiables; primero hace falta una lista de proxies de confianza.

| Acción | viewer | editor | admin | owner |
|---|:-:|:-:|:-:|:-:|
| Navegar y descargar | x | x | x | x |
| Subir, renombrar, borrar, chmod | | x | x | x |
| Crear y editar sitios y secretos | | x | x | x |
| Gestionar miembros, límites y auditoría | | | x | x |
| Borrar workspace, transferir propiedad | | | | x |

Un único punto de decisión: `authz.Can(ctx, actor, action, resource)`.

## 9. Protocolos (`RemoteFS`)

```go
type RemoteFS interface {
	List(ctx context.Context, dir string) ([]Entry, error)
	Stat(ctx context.Context, path string) (Entry, error)
	OpenRead(ctx context.Context, path string, offset int64) (io.ReadCloser, error)
	OpenWrite(ctx context.Context, path string, offset int64, mode WriteMode) (io.WriteCloser, error)
	Mkdir(ctx context.Context, path string) error
	Remove(ctx context.Context, path string) error
	RemoveAll(ctx context.Context, path string) error
	Rename(ctx context.Context, from, to string) error
	Chmod(ctx context.Context, path string, mode fs.FileMode) error
	Chtimes(ctx context.Context, path string, mtime time.Time) error
	Capabilities() Capabilities
	Ping(ctx context.Context) error
	Close() error
}
```

- Una suite de conformidad (`remotefstest`) corre contra cada adaptador con servidores reales.
- Errores normalizados: `ErrNotFound`, `ErrPermission`, `ErrExists`, `ErrAuth`, `ErrHostKeyMismatch`, `ErrConnectionLost`, `ErrQuota`.
- Un protocolo nuevo (S3, WebDAV, SMB, nubes) es un adaptador que pasa la suite.

SFTP:

- `UseConcurrentReads`, `UseConcurrentWrites`, 64 requests por archivo, paquetes de 32 KiB; ver [guides/pkg-sftp.md](guides/pkg-sftp.md).
- Cifrados: `aes128-gcm@openssh.com`, `chacha20-poly1305@openssh.com`.
- Auth: password, clave privada con passphrase, keyboard-interactive.
- Host keys: confianza en el primer uso, bloqueo si cambian.
- ProxyJump y proxies SOCKS5/HTTP.

FTP y FTPS:

- Modo pasivo primero (EPSV, luego PASV); activo opcional.
- FTPS explícito (AUTH TLS) e implícito (990), con reutilización de sesión TLS en el canal de datos.
- `MLSD` con fallback a `LIST`, `MDTM`, `SIZE`, `REST`, `UTF8`, charset configurable.

## 10. Sesiones y conexiones

- Por usuario y sitio: una conexión de navegación y N de transferencia.
- Las conexiones sobreviven a la pestaña del navegador; un timeout de inactividad las cierra.
- Keepalive: SFTP `keepalive@openssh.com`, FTP `NOOP`, cada 30 s de inactividad.
- Detección de conexiones muertas con deadlines y un ping antes de reutilizar conexiones inactivas.
- Reconexión con backoff exponencial y jitter (0,5 s a 30 s, intentos acotados).
- Los errores de autenticación, host key y permisos no se reintentan; las operaciones idempotentes sí, tras reconectar.
- Los listados se cachean con TTL corto, se invalidan al mutar y se refrescan en segundo plano.

```
Idle -> Connecting -> Ready -> Reconnecting -> Ready
              \-> Failed (no reintentable)
```

## 11. Transferencias

- Un job se expande de forma perezosa en items; el recorrido de directorios es en streaming.
- Estados de item: `queued`, `running`, `done`, `failed`, `skipped`, `paused`, `canceled`.
- Persistente en Postgres; tras un reinicio los items `running` vuelven a `queued` y siguen desde `bytes_done`.
- Scheduler: `SELECT ... FOR UPDATE SKIP LOCKED`; límites global, por workspace, por usuario y por sitio destino; prioridades; equidad round-robin; ancho de banda con token bucket.
- Resume: offsets en SFTP, `REST` en FTP; verificación de tamaño o checksum cuando el servidor lo soporta.
- Políticas de sobrescritura: preguntar, sobrescribir, solo si es más nuevo, si difiere el tamaño, renombrar, omitir; archivo `.part` y renombrado opcionales.
- Rutas de datos: remoto a navegador (streaming con `Range`, carpetas como ZIP en streaming), navegador a remoto (subida por chunks reanudable), remoto a remoto (sin pasar por el navegador).

## 12. API y tiempo real

- REST bajo `/api/v1`, definido en `api/openapi.yaml`.
- Los errores siguen RFC 9457 (`application/problem+json`) con un `code` estable y `params` opcionales; el servidor nunca localiza texto.
- Los instantes son cadenas RFC 3339 en UTC.

| Área | Endpoints |
|---|---|
| Auth | `POST /auth/login`, `POST /auth/logout`, `POST /auth/totp/verify`, `GET /me` |
| Vault | `POST /vault/{setup,unlock,lock,change-master}` |
| Workspaces | `/workspaces`, `/workspaces/{id}`, `/workspaces/{id}/members` |
| Sitios | `/workspaces/{ws}/sites`, `/sites/{id}`, `POST /sites/{id}/test`, `POST /sites/import` |
| Conexiones | `POST /sites/{id}/connect`, `DELETE /connections/{id}` |
| Archivos | `/connections/{id}/fs/{list,mkdir,rename,delete,chmod,download,content}` |
| Subidas | `POST /uploads`, `PUT /uploads/{id}`, `HEAD /uploads/{id}` |
| Transferencias | `/transfers`, `PATCH /transfers/{id}` |
| Auditoría | `GET /workspaces/{ws}/audit` |
| Sistema | `/healthz`, `/readyz`, `/metrics` |

Un WebSocket por pestaña (`/api/v1/ws`), con validación de `Origin`:

- Servidor a cliente: `connection.state`, `transfer.progress` (agregado, cada ~250 ms), `transfer.item.state`, `transfer.prompt`, `auth.prompt`, `hostkey.prompt`, `vault.locked`, `fs.changed`.
- Cliente a servidor: `subscribe`, `prompt.answer`, `resume{lastEventId}` (el servidor responde con un snapshot).

## 13. Frontend

- Layout: barra superior (workspace, conexión rápida), sidebar con gestor de sitios, doble panel redimensionable (árbol, lista, breadcrumbs), cola de transferencias y log de actividad plegables.
- Interacción: drag and drop entre paneles y desde el escritorio, menús contextuales, selección múltiple, atajos (F2, Supr, F5, Ctrl+L, Enter), paleta de comandos (Ctrl+K).
- Listas virtualizadas; actualizaciones optimistas; render de progreso con throttling.
- Estado: datos del servidor en TanStack Query, estado de UI en Zustand, los eventos WebSocket actualizan la caché de Query.
- Los componentes nunca llaman a `fetch` ni a `WebSocket`; usan la interfaz `Transport` (`HttpTransport` hoy, `WailsTransport` después).
- `usePlatform()` expone las capacidades del entorno (`hasLocalFs`, `canPickFolders`).
- Los nombres remotos son entrada no confiable: siempre se renderizan como texto, nunca como HTML.
- Accesibilidad: foco visible, ARIA, teclado completo.

## 14. Internacionalización

- Los idiomas son datos: agregar uno es agregar archivos `locales/<idioma>/*.json`. El inglés es la fuente; el español sale primero.
- i18next con namespaces por feature, claves semánticas, mensajes de oración completa, plurales CLDR y carga perezosa de idiomas.
- Sin concatenar textos; interpolación solo para valores de runtime.
- Números, fechas, tiempos relativos, listas, tamaños en bytes y ordenación usan `Intl` (`NumberFormat`, `DateTimeFormat`, `RelativeTimeFormat`, `ListFormat`, `Collator` con orden numérico).
- Listo para RTL: `dir` sigue al idioma; utilidades lógicas de Tailwind (`ms-*`, `me-*`, `ps-*`, `pe-*`, `text-start`).
- Las claves están tipadas; la CI falla por claves faltantes, sobrantes o distintas entre idiomas.
- Contrato con el servidor: los errores llevan `code` y `params`; el cliente es dueño de todo el texto visible.
- El texto que origina el servidor (correos de invitación) se renderiza según el idioma del destinatario con `golang.org/x/text`; los logs siempre en inglés.
- Resolución del idioma: preferencia del usuario, luego `Accept-Language`, luego `en`.

## 15. Fechas y zonas horarias

- Los instantes se guardan como `timestamptz`, se manejan como `time.Time` en UTC y se exponen como RFC 3339 con `Z`. Nunca se guardan cadenas formateadas.
- Las preferencias guardan nombres IANA (`America/Santiago`), nunca offsets fijos: `users.timezone`, `sites.list_timezone`. El valor inicial sale de `Intl.DateTimeFormat().resolvedOptions().timeZone`.
- El frontend convierte y formatea solo mediante `lib/datetime` (sobre `Intl` con `timeZone` explícito; Temporal cuando Safari lo soporte).
- Fechas de los servidores remotos:
  - SFTP: segundos epoch, UTC.
  - FTP `MLSD` y `MDTM`: UTC por especificación.
  - FTP `LIST`: sin zona. Se interpreta con `sites.list_timezone`, detectada automáticamente comparando `LIST` con `MDTM` y editable por el usuario.
  - `LIST` puede omitir segundos o el año; las entradas incluyen `modified_precision`.
- Servidor: `time/tzdata` embebido en el binario; contenedor con `TZ=UTC`; el tiempo pasa por `ports.Clock`.
- Las transferencias programadas guardan la zona IANA y manejan explícitamente los saltos de horario de verano.
- Tests: relojes fijos, zonas explícitas (incluidos cambios de horario y offsets de media hora), suites de UI bajo varios valores de `TZ`.

## 16. Seguridad

- `netguard`: bloquea loopback, link-local y rangos de metadata de nube; opcionalmente rangos privados; valida la IP resuelta al conectar.
- CSP, HSTS, `X-Content-Type-Options`, `Referrer-Policy`; descargas con `Content-Disposition` y `nosniff`.
- Contenedor sin root, filesystem de solo lectura, `cap_drop: ALL`.
- Auditoría inmutable; secretos redactados en logs.
- `govulncheck`, `pnpm audit`, Trivy, Renovate.
- **Revisión de seguridad al cerrar cada fase**: modelo de amenazas, ataques y exploits conocidos, CVEs de dependencias, con informe en `docs/security/` y regresiones como tests. Ver [docs/security/README.md](security/README.md).

## 17. Escalado

1. Un nodo: API, workers y hub WebSocket, Postgres.
2. Varios nodos: sesiones pegajosas por usuario (conexiones y key ring viven en memoria); eventos por `LISTEN/NOTIFY` o NATS; la cola ya es segura con varios workers.
3. `sentinel worker` separado para transferencias pesadas servidor a servidor.
4. Escritorio: `sentinel-desktop` (Wails) reutiliza el núcleo con SQLite y un solo usuario.

## 18. Operación

- Configuración por variables `SENTINEL_*`, validadas al arrancar.
- `docker compose exec app sentinel migrate status`, `sentinel admin create-user`.
- Actualizar con `git pull && docker compose up -d --build`.
- Métricas: transferencias activas, bytes por segundo, reconexiones, errores por protocolo, latencia de listados, tamaño del pool.
- Backup con `pg_dump`; sin la contraseña maestra el backup no revela credenciales.
