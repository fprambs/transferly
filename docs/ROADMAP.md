# Roadmap de Sentinel

Cada fase termina con algo usable y desplegable. Una casilla se marca cuando se cumple su criterio de aceptación, no cuando se escribe el código. Diseño en [ARCHITECTURE.md](ARCHITECTURE.md).

> Idioma: este documento y la arquitectura están en español; el desarrollo (código, comentarios, guías, ADR) va en inglés.

## Política de calidad

Definición de terminado, para toda tarea:

1. Código y tests van juntos; cada bug corregido incluye un test de regresión.
2. La CI está en verde: lint, tipos, unitarios, integración y E2E de humo.
3. Los cambios de API actualizan `openapi.yaml` y los tests de contrato.
4. Los cambios de base de datos incluyen migración y test de migración (base vacía y base con datos).
5. Las librerías se usan como recomienda su documentación oficial; lo aprendido va a `docs/guides/<librería>.md`; desviarse requiere un ADR.
6. Todo el desarrollo está en inglés y documentado según [guides/documentation.md](guides/documentation.md): godoc y TSDoc en **todo** (también tests y funciones privadas, con los casos de uso de cada test), breve y sin comentarios dentro de funciones.
7. Todo texto de usuario pasa por i18n; los instantes son UTC y se muestran mediante `lib/datetime`.
8. Al cerrar cada fase se hace una **revisión de seguridad** (ver más abajo).

Barreras de merge a `main`:

| Barrera | Umbral |
|---|---|
| Lint, formato y tipos (`golangci-lint`, `eslint`, `prettier`, `tsc --noEmit`) | 0 errores |
| Tests unitarios (Go con `-race`, frontend) | todos pasan |
| Cobertura del código de producción (Go: `scripts/coverage-go.sh`; frontend: umbrales de Vitest) | 100 % de sentencias, ramas, funciones y líneas |
| Integración contra los servidores de `testenv` | todos pasan |
| Suite de conformidad `RemoteFS` por adaptador | todos pasan |
| Matriz de autorización (endpoint x rol x workspace ajeno) | todos pasan |
| Guard de documentación (`docscheck`) | todos pasan |
| Chequeo i18n (claves faltantes, sobrantes o distintas) | 0 hallazgos |
| E2E de humo (login, vault, conectar, subir, bajar) | todos pasan |
| `govulncheck`, `pnpm audit`, Trivy | sin altas ni críticas |
| Benchmarks (throughput, listados) | sin regresión mayor al 10 % |

La verificación de UI es código, nunca revisión manual ni navegación hecha por una IA. Corre sin interfaz en contenedores, en local y en CI:

| Tipo | Cubre | Herramienta |
|---|---|---|
| Componentes | render, interacción, estados de carga, error y vacío | Vitest, Testing Library, MSW |
| E2E | flujos completos contra el backend real y `testenv` | Playwright |
| Visual | por pantalla, tema e idioma, con líneas base versionadas | Playwright `toHaveScreenshot` |
| Accesibilidad | sin violaciones WCAG graves | `@axe-core/playwright` |

Convenciones: locators por rol y etiqueta, page objects en `frontend/e2e/pages/`, datos aislados por test, sin sleeps fijos, trazas al fallar, suites bajo varios valores de `TZ` y varios idiomas.

Cobertura del 100 %: se mide entre paquetes y excluye solo lo que no es lógica de producción: `main` de `cmd/`, helpers de test (`testutil`), guards del repositorio, `main.tsx` y `components/ui` (código generado por shadcn). Las ramas defensivas imposibles de alcanzar se eliminan con mejor diseño en vez de excluirse. Un cambio que baje del 100 % no se integra.

Hooks locales (`.githooks/, dentro de `tools`): `pre-commit` formatea y lintea, `pre-push` corre los tests unitarios. `main` está protegida: solo PR con CI en verde.

## Revisión de seguridad al cerrar cada fase

Ninguna fase se cierra sin este análisis. Proceso, plantilla y catálogo de ataques conocidos en [security/README.md](security/README.md); el informe de cada fase queda en `docs/security/fase-N.md`.

1. Modelo de amenazas (STRIDE) de lo construido en la fase.
2. Checklist OWASP (Top 10 y ASVS) aplicable.
3. Ataques y exploits conocidos que aplican, cada uno con un test que lo cubre.
4. Dependencias: `govulncheck`, `pnpm audit`, Trivy y búsqueda de CVEs recientes de las librerías usadas.
5. Pruebas ofensivas: fuzzing de parsers y rutas, entradas maliciosas, matriz de autorización.
6. Hallazgos con severidad y estado; cada corrección lleva un test de regresión.
7. Sin hallazgos altos o críticos abiertos para cerrar la fase.

## Fase 0: Fundaciones

### 0.1 Desarrollo 100 % en Docker

- [x] `docker/tools.Dockerfile` con Go, Node, pnpm, sqlc, goose, oapi-codegen, golangci-lint, govulncheck y air fijados
- [x] `compose.dev.yaml`: `tools`, `postgres`, volúmenes de caché nombrados, `HOST_UID` y `HOST_GID`
- [x] Servicios `backend` (air) y `frontend` (Vite) con recarga en caliente, y servidores de prueba con toxiproxy (`compose.testenv.yaml`, perfil `testenv`)
- [ ] mailpit (se agrega cuando existan correos, fase 3)
- [ ] `.devcontainer/` conectado a `tools`
- [x] Verificado: las herramientas corren en el contenedor y los archivos generados quedan con el usuario del host

### 0.2 Repositorio

- [x] `git init`, `.gitignore`, `.dockerignore`, `.editorconfig`
- [x] Conventional Commits y plantilla de ADR
- [x] Licencia Apache-2.0 (`LICENSE`)
- [x] ADR-0001 web primero, listo para escritorio
- [x] ADR-0002 stack, ADR-0003 PostgreSQL, ADR-0004 diseño del vault
- [ ] `Makefile` opcional sobre `docker compose`; `README.md` con los dos flujos de uso

### 0.2b Guías de librerías

- [x] React, Zustand, TanStack Query, shadcn/ui, Playwright, layout de Go, pkg/sftp, sqlc y pgx
- [x] Guías de documentación, i18n, fechas y herramientas del frontend
- [x] i18next, Vitest y Playwright (usadas)
- [ ] TanStack Router, Table y Virtual; react-hook-form y zod; dnd-kit; react-arborist; MSW (se escriben antes de su primer uso, fase 1 en adelante)
- [x] chi, goose, oapi-codegen, jlaffaye/ftp (informe del spike)
- [ ] coder/websocket, `x/text` (antes de su primer uso)
- [x] Docker: multi-stage, distroless, Compose, Dev Containers

### 0.3 Esqueleto del backend

- [x] Módulo, `cmd/sentinel` (`serve`, `migrate`), configuración por entorno con validación
- [x] Layout hexagonal y regla depguard para `core`
- [x] Router chi, request id, logging estructurado, recovery
- [x] `/healthz`, `/readyz`, errores RFC 9457 con códigos estables
- [x] Migraciones goose embebidas, adaptador Postgres, apagado ordenado
- [x] `ports.Clock`, reloj UTC, `time/tzdata` embebido
- [x] `docscheck`: documentación obligatoria en todo y sin comentarios dentro de funciones
- [x] `api/openapi.yaml` (OpenAPI 3.1) con servidor Go y tipos TypeScript generados
- [x] Configuración de sqlc con la primera consulta (`Ping`)
- [x] Lógica de la CLI en `internal/app` con tests; `cmd/sentinel/main.go` reducido a una función

### 0.4 Esqueleto del frontend

- [x] Vite, React, TypeScript, Tailwind v4, `shadcn init` con Base UI y RTL (ejecutado en `tools`)
- [x] i18next (`en`, `es`) con claves tipadas
- [x] Zustand con preferencias persistentes (tema y cola) y temas claro, oscuro y del sistema
- [ ] TanStack Router y Query (se agregan al primer uso)
- [x] `lib/datetime`, interfaz `Transport` con `HttpTransport`, `usePlatform()`
- [x] Layout base: barra superior, sidebar, doble panel y cola, con regiones accesibles
- [x] oxlint (default de la plantilla): `no-restricted-imports` (las features usan solo `Transport`), `no-inline-comments`
- [x] Guards de documentación en `frontend/guards`: TSDoc obligatorio y sin comentarios dentro de funciones
- [x] Chequeo de recursos i18n (claves, vacíos y placeholders) como test
- [x] Prueba RTL: idioma de derecha a izquierda fija `dir` y un guard prohíbe utilidades físicas left/right

### 0.5 Entorno de pruebas (`testenv/`)

- [x] Servidor SFTP (password), FTP, FTPS explícito e implícito exigiendo reutilización de sesión TLS; verificados con login, subida y listado
- [ ] SFTP con autenticación por clave (fase 1: las pruebas del adaptador SFTP generan las claves)
- [x] toxiproxy delante de cada servidor (canal de control de FTP; el canal de datos pasivo aún no pasa por el proxy)
- [ ] Servidor FTP con `MLSD` (fase 2, con el adaptador FTP; vsftpd 3.0.3 no lo implementa)
- [x] Datos semilla (`make seed`, `scripts/seed-testenv.sh [--large]`): 50 000 archivos pequeños, archivos de 1 GB y 5 GB y nombres UTF-8 raros, en SFTP y FTP
- [x] Servidor FTP con hora local distinta de UTC (`America/Santiago`): `LIST` mostró 17:52 con la hora UTC en 20:52

### 0.6 Spikes

- [x] Throughput SFTP con y sin pipelining a 0 ms y 100 ms de latencia: cumple (86 % de un enlace de 100 Mbit a 100 ms); un canal SSH se limita a ~20 MB/s a 100 ms, se mitiga con transferencias paralelas ([informe](spikes/sftp-throughput.md))
- [x] FTPS con reutilización de sesión TLS usando `jlaffaye/ftp`: funciona sin fork con `ClientSessionCache` y `ServerName` ([informe](spikes/ftps-session-reuse.md))
- [x] Subida de 5 GB en streaming a SFTP: memoria máxima 14 MiB ([informe](spikes/streaming-upload.md))
- [x] Detección de zona horaria de `LIST`: `MDTM` no es fiable; se detecta comparando con el reloj del cliente ([informe](spikes/ftp-list-timezone.md))

### 0.7 Imagen de producción

- [x] `docker/app.Dockerfile` (build del frontend, build de Go con el frontend embebido, distroless sin root; imagen de 6 MB)
- [x] `compose.yaml` con `app`, `postgres`, perfil `https` (Caddy) opcional, healthchecks y contenedor endurecido
- [x] Clave de instancia generada en el primer arranque (`0600`) y reutilizada al reiniciar
- [x] Verificado: clon limpio, `docker compose up -d --build` sin `.env`, la app responde y queda healthy

### 0.8 Infraestructura de tests

- [x] Go: `testutil` (base de datos aislada por test, helper HTTP, reloj falso), `-race`
- [x] `compose.test.yaml`: `make test` corre las pruebas unitarias con cobertura y las E2E contra la imagen de producción
- [x] Puerto `RemoteFS`, suite de conformidad `remotefstest` y adaptador de referencia `memfs` (la suite se verificó con mutaciones)
- [ ] `storetest` y matriz de autorización (se crean con el primer repositorio y el primer endpoint protegido, fase 1)
- [x] Tests de contrato contra `openapi.yaml` (Go, con kin-openapi)
- [x] Vitest, Testing Library, cobertura v8
- [x] Playwright con page objects, líneas base visuales y axe (15 pruebas, contra la imagen de producción)
- [ ] MSW (se agrega con el primer componente que consuma la API)
- [ ] Reportes de cobertura comparados con `main`; líneas base de benchmarks
- [x] Hooks de git (`pre-commit`, `commit-msg`, `pre-push`)

### 0.9 CI

- [x] Workflow de GitHub Actions (`.github/workflows/ci.yml`) sobre los mismos contenedores: código generado al día, lint y tests con 100 % de cobertura, E2E contra la imagen, auditoría y Trivy, y publicación en GHCR desde `main`; acciones fijadas por SHA y validado con `actionlint`; sus pasos se ejecutaron localmente
- [ ] `main` protegida (ajuste del repositorio remoto: PR obligatorio y los cinco jobs de CI como requisito)

### 0.10 Revisión de seguridad

- [x] Informe [security/fase-0.md](security/fase-0.md) cerrado: 16 hallazgos, ninguno alto o crítico abierto

Aceptación: con solo Docker instalado, los stacks de desarrollo y producción arrancan desde un clon limpio; un comando corre todos los tests con todas las barreras; los spikes quedan documentados en `docs/spikes/`; informe de seguridad cerrado.

## Fase 1: MVP (usuario, vault, SFTP)

Bloques de trabajo (cada uno con tests al 100 %, guía de las librerías nuevas y commit propio):

- [x] 1.1 Criptografía del vault: Argon2id con tope de derivaciones concurrentes, AES-256-GCM, DEK, recovery kit, key ring con TTL
- [x] 1.2 Esquema (usuarios, vault, workspaces, sesiones, sitios, secretos, host keys, auditoría) con migración de subida y bajada
- [x] 1.3 Autenticación: hash Argon2id PHC, sesiones opacas con caducidad, CSRF firmado y guardas de origen, rate limits por IP y cuenta con IP de cliente detrás de proxies de confianza, asistente de primer arranque, endpoints `/setup`, `/auth/login`, `/auth/logout`, `/me`, protección derivada del contrato ([ADR-0005](adr/0005-authentication.md))
- [x] 1.4 Workspaces y permisos (`workspace.Can`: matriz rol × acción); espacio personal automático; matriz de autorización HTTP (se amplía con cada endpoint nuevo)
- [x] 1.5 API del vault: setup, unlock, lock, cambio de maestra, recovery y regeneración del kit; llaves en memoria por sesión con TTL, límite de intentos por cuenta y bloqueo al cerrar sesión
- [ ] 1.6 `netguard` y sitios: CRUD, secretos cifrados, host keys
- [ ] 1.7 Adaptador SFTP con TOFU y suite de conformidad (autenticación por clave incluida)
- [ ] 1.8 Session manager: pool, keepalive, reconexión con backoff
- [ ] 1.9 API de archivos, descarga en streaming y subida reanudable
- [ ] 1.10 Cola de transferencias persistente y eventos por WebSocket
- [ ] 1.11 Frontend: setup, login, unlock, sitios, explorador, transferencias, con E2E
- [ ] 1.12 Revisión de seguridad de la fase 1

- [ ] Usuarios: asistente de primer arranque, login y logout, sesiones, CSRF, rate limits, preferencias de idioma y zona horaria
- [ ] Workspace personal creado automáticamente
- [ ] Vault: setup, recovery kit, unlock y lock, bloqueo por inactividad, cambio de contraseña maestra
- [ ] Sitios: CRUD, carpetas, prueba de conexión, secretos cifrados
- [ ] `RemoteFS` y adaptador SFTP con suite de conformidad
- [ ] Session manager: keepalive, reconexión con backoff, confianza en el primer uso de host keys
- [ ] `netguard` activo desde el primer día
- [ ] Explorador (un panel): navegar, lista virtualizada, breadcrumbs, mkdir, renombrar, borrar, chmod, menú contextual
- [ ] Transferencias: subida por chunks reanudable, descarga en streaming, cola persistente, estado por WebSocket
- [ ] Imagen de producción y Compose con Caddy
- [ ] Revisión de seguridad de la fase: autenticación y sesiones, CSRF, CSWSH, vault (nonce GCM, DoS de Argon2, fuga de secretos), SSRF y DNS rebinding, MITM por host keys, suplantación de IP

Tests obligatorios: criptografía del vault (vectores conocidos, contraseña incorrecta), conformidad SFTP, reconexión con toxiproxy, matriz de autorización, E2E completo, sin secretos en claro en respuestas de la API, casos de `netguard` (loopback, metadata, DNS rebinding), render de zonas horarias.

Aceptación: desplegar con `docker compose up`, crear un vault, guardar un sitio SFTP, navegar 50k archivos con fluidez, subir 5 GB, cortar la red a mitad de subida y ver que se reanuda; informe de seguridad cerrado.

## Fase 2: Núcleo FileZilla

- [ ] Adaptadores FTP y FTPS (explícito, implícito, pasivo, activo, MLSD, LIST, charset)
- [ ] Zona horaria de `LIST` por sitio con autodetección
- [ ] Doble panel con drag and drop entre paneles
- [ ] Pool de conexiones por sitio
- [ ] Scheduler con límites global, por workspace, por usuario y por sitio; prioridades; pausar, reanudar, cancelar
- [ ] Transferencia de carpetas, descarga ZIP, políticas de sobrescritura, resume FTP, checksums
- [ ] Transferencias servidor a servidor
- [ ] Log de actividad por conexión sin secretos
- [ ] Caché de listados con refresco en segundo plano
- [ ] Importación de `sitemanager.xml` de FileZilla
- [ ] Revisión de seguridad de la fase: FTP bounce, secuestro de IP en PASV, inyección CRLF, path traversal y Zip Slip, symlinks, zip bombs, XSS por nombres remotos, sniffing de contenido, parser de XML de importación (XXE)

Tests obligatorios: conformidad FTP y FTPS con reutilización de sesión TLS, parsers de `LIST` con fixtures reales, scheduler con reloj falso, resume tras corte y reinicio, checksum por archivo, E2E del doble panel, fuzzing de rutas y de `LIST`.

Aceptación: 10 transferencias simultáneas entre 3 servidores dentro de los límites por sitio; cortes de red simulados no dejan archivos corruptos; informe de seguridad cerrado.

## Fase 3: Equipos multiusuario

- [ ] Workspaces de equipo, invitaciones, roles
- [ ] Claves X25519 por usuario y DEK de workspace compartida; rotación al quitar un miembro
- [ ] RBAC en API y UI
- [ ] Vista de auditoría
- [ ] TOTP y passkeys
- [ ] Panel de administración de la instancia y política de red
- [ ] Cuotas y límites de ancho de banda
- [ ] Revisión de seguridad de la fase: IDOR y acceso entre workspaces, escalada de privilegios entre roles, sellado y rotación de claves, abuso de invitaciones, bypass de 2FA, enumeración de usuarios

Tests obligatorios: aislamiento entre workspaces, sellado y rotación de DEK, matriz de autorización completa, TOTP y passkeys, cobertura de auditoría.

Aceptación: dos usuarios comparten un workspace de equipo con workspaces personales privados; un viewer no puede subir ni ver secretos; quitar a un miembro rota la clave; informe de seguridad cerrado.

## Fase 4: Funciones avanzadas

- [ ] Comparación de directorios y sincronización con simulación previa
- [ ] Editor remoto (CodeMirror) con detección de conflictos
- [ ] Búsqueda remota recursiva
- [ ] ProxyJump, proxy SOCKS5/HTTP, keyboard-interactive
- [ ] Límites de velocidad por sitio, subidas con `.part`
- [ ] Paleta de comandos, preferencias, marcadores, vistas previas
- [ ] Exportación cifrada del vault
- [ ] Mensajes de error guiados
- [ ] Más idiomas (traducciones de la comunidad)
- [ ] Revisión de seguridad de la fase: XSS en vistas previas y editor, conflictos y pérdida de datos en sync, fuga de secretos al exportar, abuso de proxies como pivote de red

## Fase 5: Escala e integraciones

- [ ] SSO con OIDC
- [ ] Tokens de API y documentación pública
- [ ] Adaptadores: S3, WebDAV, SMB, Google Drive, Azure Blob
- [ ] Multi nodo: sesiones pegajosas, eventos por `LISTEN/NOTIFY` o NATS, worker separado
- [ ] Transferencias programadas con cron consciente de zonas horarias, webhooks
- [ ] Dashboards de Grafana, trazas OpenTelemetry
- [ ] Pruebas de carga (k6) y de caos continuas
- [ ] Revisión de seguridad de la fase: validación de tokens OIDC, alcance y revocación de tokens de API, SSRF por webhooks, aislamiento entre nodos, secretos de los adaptadores en la nube

## Fase 6 (opcional): Sentinel Desktop

Ver [ADR-0001](adr/0001-web-first-desktop-ready.md).

- [ ] Store SQLite que pasa `storetest`; `JobQueue` y `EventBus` en memoria
- [ ] Adaptador `localfs`
- [ ] `transport/wails` y `WailsTransport`
- [ ] `cmd/sentinel-desktop`: usuario único, vault conservado, configuración en el directorio del usuario del SO
- [ ] Builds multiplataforma en contenedores; firma en macOS en un runner macOS
- [ ] Instaladores, firma de código y auto actualización
- [ ] Revisión de seguridad de la fase: superficie de los bindings de Wails, actualizaciones firmadas, almacenamiento local de la base y del vault

## Métricas

- Listado de 50k entradas: primer render bajo 1 s tras recibir los datos.
- Throughput SFTP a 100 ms de RTT: al menos 80 % del ancho de banda.
- Memoria del backend con 20 transferencias activas: bajo 300 MB.
- Reanudación tras recuperar la red: bajo 5 s.

## Riesgos

| Riesgo | Mitigación |
|---|---|
| `jlaffaye/ftp` sin reutilización de sesión TLS | Spike de la fase 0; fork o cliente propio |
| FTP activo detrás de NAT de Docker | Pasivo por defecto; documentar límites |
| Las DEK en memoria complican el multi nodo | Sesiones pegajosas, previstas en el diseño |
| Perder la contraseña maestra pierde los secretos | Recovery kit obligatorio |
| SSRF hacia redes internas | `netguard` valida IPs resueltas desde la fase 1 |
| Errores de zona horaria en listados `LIST` | Zona por sitio, autodetección, tests bajo varias zonas |
| El acoplamiento a la web bloquea el escritorio | Núcleo hexagonal y `Transport`, reglas verificadas en CI |
| Vulnerabilidades conocidas | Revisión de seguridad al cierre de cada fase |
| Alcance | Fases con criterios de aceptación |

## Decisiones abiertas

- Registro abierto o solo por invitación (recomendado: invitación).
- Contraseña maestra opcional (recomendado: obligatoria en v1).
