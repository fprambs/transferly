# Arquitectura de Transferly

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
- `TRANSFERLY_DOMAIN` más `--profile https` activa Caddy con TLS automático.

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
  cmd/transferly/                 binario web
  cmd/transferly-desktop/         futuro binario Wails
  internal/
    core/{ports,auth,workspace,authz,vault,site,remotefs,session,transfer,netguard,audit}
    adapters/{remotefs/{sftpfs,ftpfs,s3fs,gdrivefs,azure,azblobfs,azfilefs,davfs,memfs,connect,remotefstest,connectortest},googleauth,store/{postgres,sqlite,storetest},clock,events}
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
| `memberships` | workspace_id, user_id, role |
| `workspace_key_grants` | workspace_id, user_id, version, sealed (la clave del workspace sellada con la clave pública X25519 del miembro) |
| `invitations` | workspace_id, email, role, token_hash, invited_by, expires_at (una fila por invitación pendiente; se borra al aceptar, revocar o vencer) |
| `sites` | id, workspace_id, collection_id, name, connector, config (jsonb con los ajustes del conector según su esquema, ADR-0022), speed_limit_bps, remote_dir, color |
| `site_secrets` | site_id, kind, nonce, ciphertext, dek_version |
| `known_hosts` | workspace_id, host, port, key_type, fingerprint_sha256 |
| `transfers` | user_id, workspace_id, site_id, direction, path, status, total_bytes, bytes_done (desde dónde se reanuda), error_code, attempts. En la fase 1 cada subida o descarga HTTP es una fila; `transfer_jobs` / `transfer_items` (expansión de carpetas, prioridad, checksum) llegan con las fases 2 y 3 |
| `audit_events` | workspace_id, user_id, action, target, ip, meta (jsonb), at (timestamptz) |

Toda consulta de negocio filtra por `workspace_id` y pasa por RBAC.

## 7. Vault de credenciales

```
contraseña maestra --Argon2id(salt, m=64MiB, t=3, p=4; RFC 9106, poca memoria)--> KEK
KEK --AES-256-GCM (wrap)--> DEK del usuario (workspace personal)
DEK del usuario --wrap--> clave privada X25519
clave del workspace --ECDH efímero + HKDF-SHA-256 + AES-256-GCM, con la clave pública de cada miembro--> workspace_key_grants
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

- Email y contraseña (Argon2id, PHC), TOTP y passkeys; OIDC más adelante. Detalle en [ADR-0005](adr/0005-authentication.md).
- Cookie de sesión opaca (`HttpOnly`, `SameSite=Lax`, `Secure` con https; solo se guarda el hash del token), token CSRF firmado con la clave de instancia y ligado a la sesión, guarda de `Sec-Fetch-Site` y `Origin`, rate limit en login y setup.
- La IP del cliente sale de la conexión, o de `X-Forwarded-For` solo detrás de proxies de confianza configurados (`TRANSFERLY_TRUSTED_PROXIES`).
- Los sitios solo pueden apuntar a direcciones públicas; las redes privadas, loopback, link-local y metadatos de nube se rechazan salvo las listadas en `TRANSFERLY_ALLOWED_NETWORKS`. La comprobación ocurre al conectar (`net.Dialer.Control`), por lo que resiste DNS rebinding (`core/netguard`).
- Los administradores de la instancia (`role = admin`, distinto del rol en un workspace) pueden ampliar esa lista desde `/admin` sin reiniciar: las redes se guardan en `network_allowances` y `netguard.Policy` las aplica a la siguiente conexión. Nunca se permiten `0.0.0.0/8`, `169.254.0.0/16` (metadatos de nube), multicast, reservados ni redes más anchas que /8 (IPv4) o /7 (IPv6), aunque las pida un administrador.
- La administración (`core/admin`) comprueba el rol del actor en cada operación, no se puede cambiar la propia cuenta y el último administrador activo no se puede deshabilitar ni degradar. Deshabilitar una cuenta cierra sus sesiones; quitar el segundo factor borra la app, las passkeys y los códigos de recuperación en una transacción. Todo queda en `audit_events` sin workspace y con acción `admin.*`, que solo leen los administradores.
- Qué operaciones exigen sesión sale del contrato OpenAPI.

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
- Errores normalizados: `ErrNotFound`, `ErrExists`, `ErrPermission`, `ErrNotDirectory`, `ErrNotEmpty`, `ErrInvalid`, `ErrAuth`, `ErrHostKeyMismatch`, `ErrConnectionLost`, `ErrQuota`; `Retryable(err)` indica si conviene reintentar tras reconectar.
- El adaptador `memfs` (en memoria) es la implementación de referencia de la suite y un doble de test para la cola de transferencias.
- Un protocolo nuevo es un adaptador que pasa la suite y un conector del catálogo (ADR-0019, ADR-0022): un descriptor con su esquema de campos y sus capacidades, y un opener en `remotefs/connect` que arma el adaptador desde la configuración del sitio. `connectortest` verifica, para cada conector incorporado, que el descriptor sea válido, que las capacidades declaradas sean las del adaptador y que la suite pase a través de un sitio guardado.

SFTP:

- `UseConcurrentReads`, `UseConcurrentWrites`, 64 requests por archivo, paquetes de 32 KiB; ver [guides/pkg-sftp.md](guides/pkg-sftp.md).
- Un canal SSH no supera ~`2 MiB / RTT` (ventana de `x/crypto/ssh`; ~20 MB/s a 100 ms), así que se ejecutan varias transferencias a la vez por sitio como clientes SFTP separados sobre la misma conexión SSH (por defecto 4). Ver [spikes](spikes/sftp-throughput.md).
- Cifrados: `aes128-gcm@openssh.com`, `chacha20-poly1305@openssh.com`.
- Auth: password, clave privada con passphrase, keyboard-interactive.
- Host keys: confianza en el primer uso, bloqueo si cambian.
- ProxyJump y proxies SOCKS5/HTTP.
- Herramientas SSH (ADR-0027): `sftpfs` implementa el puerto opcional `remotefs.Commands` con canales `exec` de la misma conexión (copiar, checksum, comprimir, extraer y medir en el servidor), con un sondeo por conexión y respaldo cuando el servidor no permite comandos. Guía: [guides/ssh-tools.md](guides/ssh-tools.md).

FTP y FTPS:

- Modo pasivo primero (EPSV, luego PASV); activo opcional.
- FTPS explícito (AUTH TLS) e implícito (990), con reutilización de sesión TLS en el canal de datos: `tls.Config` con `ClientSessionCache` compartida y `ServerName` siempre no vacío (con IP como host la reutilización falla). Ver [spikes](spikes/ftps-session-reuse.md).
- `MLSD` con fallback a `LIST`, `MDTM`, `SIZE`, `REST`, `UTF8`, charset configurable.
- El certificado del servidor no se valida contra autoridades: su huella SHA-256 se fija como una host key (tipo `tls-certificate`) y el usuario decide si confiar. La IP anunciada en PASV se ignora siempre. Sin `Rewrite` (FTP), una escritura reanudada solo puede empezar al final del archivo. El conector elige FTPS implícito solo en el puerto 990 (configurable en el conector) y explícito en los demás; FTP no informa permisos y la interfaz muestra un guion. Estado actual y reglas en [guides/ftp.md](guides/ftp.md).

S3 y Google Cloud Storage (ADR-0023, ADR-0024):

- `s3fs` sobre minio-go. Las carpetas se emulan con prefijos y objetos marcador; los renombres son copias en el servidor seguidas de un borrado.
- Las subidas son atómicas (multipart) y no se reanudan: sin `Resume`, la cola escribe directo y cancela la subida si el origen falla, sin pisar el original.
- Cloud Storage usa el mismo adaptador con claves HMAC sobre su API compatible con S3. Guía: [guides/minio-go.md](guides/minio-go.md).

Google Drive (ADR-0024):

- `gdrivefs` es un cliente REST propio de Drive v3: Mi unidad o una unidad compartida, rutas resueltas a IDs con caché y, si hay nombres duplicados, se muestra el más antiguo.
- Los documentos de Google se exportan al leerlos; las subidas son reanudables en trozos de 8 MiB y el borrado va a la papelera.
- `googleauth` hace el flujo de código con PKCE en una ventana emergente; el resultado vuelve por un `BroadcastChannel` validado por `state` y el token de renovación se sella en el vault. Guía: [guides/google.md](guides/google.md).

Azure Storage (ADR-0025):

- `remotefs/azure` es un cliente REST propio (el SDK suma 13 MB): firma Shared Key, tokens SAS o acceso anónimo, y endpoints propios para nubes soberanas y emuladores.
- `azblobfs` (Blob Storage) emula carpetas como S3, sube en bloques de 8 MiB confirmados al final y renombra con copias en el servidor.
- `azfilefs` (Azure Files) tiene carpetas reales, escribe en rangos de 4 MiB con reanudación, renombra de forma nativa y fija fechas. Guía: [guides/azure.md](guides/azure.md).

WebDAV (ADR-0026):

- `davfs` es un cliente propio de RFC 4918: PROPFIND de profundidad 0 y 1, GET por rango, PUT en streaming con `If-None-Match: *` al crear, MKCOL, DELETE y MOVE con `Overwrite: F`. Carpetas reales; sin reanudar subidas ni fijar fechas.
- HTTPS se verifica contra las autoridades; si no las pasa, la huella del certificado se fija como la de un certificado FTPS (`tls-certificate`), y un certificado que las autoridades aceptan nunca se fija. Guía: [guides/webdav.md](guides/webdav.md).

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
- Límites y cuotas (`core/limits`): los valores de arranque salen del entorno (`TRANSFERLY_TRANSFER_LIMIT_*`, `TRANSFERLY_BANDWIDTH_GLOBAL` y `_PER_USER` en bytes por segundo, `TRANSFERLY_MAX_SITES_PER_WORKSPACE`, `TRANSFERLY_MAX_TEAMS_PER_USER`, `TRANSFERLY_MAX_MEMBERS_PER_TEAM` y `TRANSFERLY_DAILY_BYTES_PER_USER`; 0 = sin límite) y un administrador puede reemplazarlos desde `/admin` sin reiniciar. Lo guardado (`instance_limits`) gana al entorno, se aplica a la siguiente petición o bloque de datos y no interrumpe lo que ya corre. El ancho de banda espacia los bloques dentro de `Tracker.Progress` (un `shaper` por persona y uno global, sin ráfaga tras una pausa, cancelable con el contexto); las cuotas se consultan justo antes de crear el sitio, la invitación, el equipo o la transferencia. El volumen diario suma `bytes_done` de las últimas 24 horas. Bajar una cuota no borra lo que ya existe.
- Scheduler (`core/scheduler`, en memoria): límites global, por workspace, por usuario y por sitio o conexión rápida, configurables con `TRANSFERLY_TRANSFER_LIMIT_GLOBAL`, `_PER_WORKSPACE`, `_PER_USER` y `_PER_SITE` (32, 16, 8 y 4 por defecto; 0 = sin límite). Las transferencias esperan en cola (`queued`) hasta que caben en todos los límites; el orden es prioridad (alta, normal, baja) y luego llegada, sin bloqueo de cabeza de línea. Una transferencia en cola se pausa, reanuda, prioriza o cancela; una en curso solo se cancela. Pendiente: reparto equitativo entre usuarios y planificación dirigida por el servidor (`SELECT ... FOR UPDATE SKIP LOCKED`) cuando existan carpetas y transferencias servidor a servidor.
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

Diseño objetivo (la fase 1 entrega una sola vista con panel de detalles; el doble panel llega en la fase 2 y reutiliza filas, barra y dock):

- Layout: barra superior (espacio de trabajo, vault, cuenta), barra lateral con el árbol de sitios y colecciones y la conexión rápida, explorador, panel de detalles y dock de transferencias plegable.
- Interacción: menús contextuales, teclado completo en la tabla (flechas, Inicio, Fin, Enter, Retroceso, F2, Supr), arrastrar y soltar desde el escritorio para subir, ⌘⇧K para la conexión rápida. Pendientes para la fase 2: arrastrar entre paneles, selección múltiple, F5, Ctrl+L y la paleta de comandos.
- Listas virtualizadas con TanStack Virtual (filas de 32 px); el progreso se recibe por WebSocket, con velocidad suavizada en el cliente.
- Estado: datos del servidor en TanStack Query (la caché es la fuente de verdad de sesión, sitios, colecciones, listados y vault); estado de UI en Zustand (`ui-store` persistido con tema, seguir al sistema, cola abierta y espacio elegido; `dialogs-store` y `transfers-store` en memoria). Los eventos del WebSocket alimentan el `transfers-store` y refrescan el estado del vault.
- Enrutado con TanStack Router por código: una ruta de estructura `shell` con guarda de sesión (redirige a `/setup` o `/login`), y `/`, `/sites/$siteId?path=`, `/settings` y `/servers` dentro; `/login` y `/setup` fuera. El contexto del router lleva la caché, el cliente de la API y el `Transport`.
- Los componentes nunca llaman a `fetch` ni a `WebSocket`; usan la interfaz `Transport` (`HttpTransport` hoy, `WailsTransport` después), que ofrece `request`, `url`, `events` y `setCsrfToken`. El cliente tipado (`lib/api`) se construye sobre `Transport` a partir del contrato OpenAPI.
- Formularios con react-hook-form y zod; los mensajes de validación son claves de traducción. Diseño con tokens: un tema es un conjunto de tokens que se aplica como variables CSS.
- `usePlatform()` expone las capacidades del entorno (`hasLocalFs`, `canPickFolders`).
- Los nombres remotos son entrada no confiable: siempre se renderizan como texto, nunca como HTML (con un E2E que lo comprueba).
- Subidas: los archivos elegidos se suben en secuencia con un identificador de transferencia; el archivo se conserva en memoria (hasta 50) mientras la subida no termine, para poder reanudarla; tras recargar la página se vuelve a elegir el archivo y se comprueba su nombre y tamaño. Las descargas las hace el navegador con un enlace al endpoint (adjunto inerte).
- Accesibilidad: foco visible, ARIA (árbol, tabla como `grid`, diálogos, `progressbar`), teclado completo y escaneo axe de los 12 temas en el E2E.

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
- Las preferencias guardan nombres IANA (`America/Santiago`), nunca offsets fijos: `users.timezone`, el ajuste `list_timezone` de un sitio FTP. El valor inicial sale de `Intl.DateTimeFormat().resolvedOptions().timeZone`.
- El frontend convierte y formatea solo mediante `lib/datetime` (sobre `Intl` con `timeZone` explícito; Temporal cuando Safari lo soporte).
- Fechas de los servidores remotos:
  - SFTP: segundos epoch, UTC.
  - FTP `MLSD` y `MDTM`: UTC por especificación.
  - FTP `LIST`: sin zona. Se interpreta con el ajuste `list_timezone` del sitio, detectada subiendo un archivo de prueba y comparando su hora en `LIST` con el reloj del cliente (redondeo a 15 min), y editable por el usuario. `MDTM` no es fiable: vsftpd con `use_localtime=YES` lo devuelve en hora local. Ver [spikes](spikes/ftp-list-timezone.md).
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
3. `transferly worker` separado para transferencias pesadas servidor a servidor.
4. Escritorio: `transferly-desktop` (Wails) reutiliza el núcleo con SQLite y un solo usuario.

## 18. Operación

- Configuración por variables `TRANSFERLY_*`, validadas al arrancar.
- `docker compose exec app transferly migrate status`, `transferly admin create-user`.
- Actualizar con `git pull && docker compose up -d --build`.
- Métricas: transferencias activas, bytes por segundo, reconexiones, errores por protocolo, latencia de listados, tamaño del pool.
- Backup con `pg_dump`; sin la contraseña maestra el backup no revela credenciales.
