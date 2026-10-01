# Roadmap de Transferly

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

Hooks locales (`.githooks/, dentro de `tools`): `pre-commit` formatea y lintea; los tests con cobertura van en `./scripts/push.sh` (no en el hook `pre-push`, porque Git abre SSH antes del hook y GitHub cierra sesiones inactivas). `main` está protegida: solo PR con CI en verde.

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
- [x] coder/websocket (guía hecha con su primer uso) · [ ] `x/text`
- [x] Docker: multi-stage, distroless, Compose, Dev Containers

### 0.3 Esqueleto del backend

- [x] Módulo, `cmd/transferly` (`serve`, `migrate`), configuración por entorno con validación
- [x] Layout hexagonal y regla depguard para `core`
- [x] Router chi, request id, logging estructurado, recovery
- [x] `/healthz`, `/readyz`, errores RFC 9457 con códigos estables
- [x] Migraciones goose embebidas, adaptador Postgres, apagado ordenado
- [x] `ports.Clock`, reloj UTC, `time/tzdata` embebido
- [x] `docscheck`: documentación obligatoria en todo y sin comentarios dentro de funciones
- [x] `api/openapi.yaml` (OpenAPI 3.1) con servidor Go y tipos TypeScript generados
- [x] Configuración de sqlc con la primera consulta (`Ping`)
- [x] Lógica de la CLI en `internal/app` con tests; `cmd/transferly/main.go` reducido a una función

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
- [x] 1.6 `netguard` y sitios: CRUD, secretos cifrados sellados con la llave del vault, host keys de confianza (TOFU) con listado, confianza y olvido
- [x] 1.7 Adaptador SFTP (`sftpfs`) con verificación de host key por callback obligatorio, autenticación por contraseña y por clave, suite de conformidad contra un servidor en proceso y contra OpenSSH real
- [x] 1.8 Gestor de conexiones: pool por usuario y sitio (máximo 4), keepalive, validación antes de usar, reconexión con backoff solo ante fallos de red, cierre al bloquear el vault o cerrar sesión, conector con host key TOFU y `netguard`
- [x] 1.9 API de archivos: listar, describir, crear carpetas, renombrar, borrar (recursivo opcional), descarga en streaming con rangos y subida en streaming reanudable; adjuntos inertes; errores con códigos estables
- [x] 1.10 Transferencias registradas de forma persistente (estado, progreso, reanudación, cancelación, historial privado por usuario) y eventos por WebSocket con `Origin` validado, instantánea inicial y cierre al terminar la sesión. El scheduler con límites por sitio y la expansión de carpetas llegan en la fase 2
- [x] 1.11 Colecciones de sitios anidadas (árbol: crear, renombrar, mover, borrar; sitios dentro de colecciones) y conexión rápida sin guardar (credenciales solo en memoria, con opción de guardarla como sitio si funciona); preferencia de tema por usuario
- [x] 1.12 Frontend con el diseño aprobado ([maquetas](https://claude.ai/artifact/8MUc5nGuSLbMTVuph5sSpz)): sistema de temas por tokens y lista de 12 temas (Transferly por defecto oscuro y claro, Monokai, Dracula, Nord, Solarized, One Dark, Tokyo Night, Gruvbox, GitHub) guardada en la cuenta; enrutado con TanStack Router y guardas de sesión; setup, login, desbloqueo y creación del vault con kit de recuperación; barra superior con espacio de trabajo, vault y menú de cuenta; árbol de sitios con colecciones y subcolecciones, formularios de sitio con prueba de conexión y conexión rápida que se comprueba antes de ofrecer guardarla; explorador de una sola vista (tabla virtualizada, migas, panel de detalles, carpeta nueva, renombrar, borrar, menú contextual, teclado) con subida, arrastrar y soltar y descarga; dock de transferencias por WebSocket con progreso, velocidad, cancelar y reanudar subidas desde el navegador; servidores de confianza, ajustes y pantalla de ruta no encontrada; 100 % de cobertura en Vitest, E2E contra una imagen de producción con un SFTP real, escaneo de accesibilidad de los 12 temas y líneas base visuales. El doble panel llega en la fase 2
- [x] 1.13 Revisión de seguridad de la fase 1 ([informe](security/fase-1.md)): sin hallazgos altos ni críticos abiertos; dos correcciones con regresión (tamaños de subida negativos y SHA-1 en el intercambio de claves SSH)

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

- [x] Adaptador FTP y FTPS (`ftpfs`): pasivo, explícito e implícito, MLSD con respaldo a LIST, certificado fijado por huella (como una host key), reutilización de sesión TLS, rutas con CRLF rechazadas, IP de PASV ignorada; suite de conformidad contra un servidor en proceso y contra vsftpd real ([guía](guides/ftp.md)); nueva capacidad `Rewrite` del puerto
- [x] FTP y FTPS conectados a sitios y a la conexión rápida: el conector elige el adaptador por protocolo (FTPS implícito en el puerto 990, explícito en los demás), la huella del certificado se confirma como una host key, la zona horaria de LIST viene del sitio, una cuenta FTP puede tener contraseña vacía; formularios con las tres pestañas, aviso de texto claro para FTP y guion en lugar de permisos; E2E contra vsftpd real (FTP y FTPS)
- [x] Charset configurable por sitio FTP y FTPS (UTF-8 por defecto; ISO-8859-1, -2 y -15, Windows-1250, -1251 y -1252, KOI8-R, Shift_JIS, EUC-JP, GBK, Big5 y EUC-KR; los nombres se convierten en el borde del adaptador, [guía](guides/charset.md))
- [x] Modo activo opt-in de FTP: descartado tras la revisión de seguridad (exige puertos entrantes y reintroduce el rebote FTP); condiciones para reabrirlo en [security/fase-2.md](security/fase-2.md)
- [x] Zona horaria de `LIST` por sitio con autodetección (la zona se guarda por sitio; el botón del reloj en la barra del explorador de un sitio FTP escribe un archivo de prueba, lee la hora que muestra el listado, la compara con el reloj y ofrece la zona `Etc/GMT±N`; comprobado contra un vsftpd con hora de Santiago; al editar o borrar un sitio se cierran sus conexiones en pool para que los cambios apliquen al instante)
- [x] Doble panel con drag and drop entre paneles (el segundo panel se abre con `?split=` y elige su sitio; arrastrar un archivo de un panel al otro lo copia; las carpetas y la selección múltiple llegan con la transferencia de carpetas)
- [x] Pool de conexiones por sitio
- [x] Scheduler con límites global, por workspace, por usuario y por sitio; prioridades; pausar, reanudar, cancelar (las transferencias en cola se pausan, reanudan y priorizan; una en curso solo se cancela hasta que las transferencias las dirija el servidor)
- [x] Descarga de carpetas como ZIP en streaming (`GET /files/archive`, por defecto) o archivo por archivo desde el navegador (hasta 500 archivos)
- [x] Copia de carpetas entre sitios con políticas de sobrescritura (omitir, sobrescribir, solo si es más nuevo, conservar ambos, detenerse); arrastrar una carpeta de un panel al otro pregunta la política
- [x] Subida de carpetas desde el navegador (arrastrar una carpeta, botón «Subir carpeta» o desde la carpeta local) y descarga de carpetas a la carpeta local, con política para los archivos que existen (omitir, conservar ambos, sobrescribir, detenerse); cada archivo es una transferencia propia
- [x] Resume FTP (subidas con `REST`/`APPE`, comprobado con vsftpd en el test de conformidad y en un E2E de cancelar y reanudar) y checksums (MD5, SHA-1, SHA-256 y SHA-512 de un archivo remoto desde el inspector; las copias entre sitios pueden verificarse leyendo de vuelta cada archivo)
- [x] Panel de carpeta local: el segundo panel puede mostrar una carpeta del equipo del usuario con la File System Access API (navegadores Chromium, en `localhost` o HTTPS); arrastrar de un servidor a la carpeta guarda el archivo sin sobrescribir nada y arrastrar de la carpeta a un servidor lo sube. El acceso completo al disco queda para la versión de escritorio (Wails)
- [x] Transferencias servidor a servidor de archivos (`POST /workspaces/{id}/copies`, dirección `copy`; las carpetas llegan con la transferencia de carpetas)
- [x] Log de actividad por conexión sin secretos (carpetas creadas, renombrados, borrados, checksums y transferencias que terminan, con el sitio, la ruta y el código del resultado; privado por usuario, pestaña «Actividad» del panel de transferencias y `TRANSFERLY_ACTIVITY_RETENTION`, 30 días por defecto)
- [x] Caché de listados con refresco en segundo plano (en el navegador: un listado se considera fresco 15 segundos y se guarda 10 minutos; volver a una carpeta la muestra al instante, una carpeta pasada se refresca en segundo plano sin ocultar lo que se ve, apoyar el puntero sobre una carpeta precarga su listado y cualquier cambio en la conexión o volver a la pestaña marca los listados como caducados)
- [x] Importación de `sitemanager.xml` de FileZilla (botón «Importar de FileZilla» del árbol de sitios: vista previa y luego importación; conserva las carpetas, las contraseñas van al vault solo si se piden y está desbloqueado, los nombres repetidos se omiten o se renombran; el archivo es entrada no confiable: 4 MiB, sin DTD ni entidades, profundidad y cantidad acotadas; quedan fuera los protocolos no admitidos, FTPS implícito fuera del puerto 990 y los accesos con archivo de clave)
- [x] Revisión de seguridad de la fase 2 ([informe](security/fase-2.md)): sin hallazgos altos ni críticos abiertos; validación de nombres en el panel local, matriz de autorización de los endpoints nuevos y fuzz del convertidor de rutas y del importador XML

Tests obligatorios: conformidad FTP y FTPS con reutilización de sesión TLS, parsers de `LIST` con fixtures reales, scheduler con reloj falso, resume tras corte y reinicio, checksum por archivo, E2E del doble panel, fuzzing de rutas y de `LIST`.

Aceptación: 10 transferencias simultáneas entre 3 servidores dentro de los límites por sitio; cortes de red simulados no dejan archivos corruptos; informe de seguridad cerrado.

## Fase 3: Equipos multiusuario

Decisiones de diseño en [ADR-0006](adr/0006-teams-and-invitations.md).

- [x] 3.1 Workspaces de equipo: crear, listar miembros, cambiar rol, salir, transferir propiedad
- [x] 3.2 Invitaciones de un solo uso ligadas al email, registro solo por invitación (el token viaja en el fragmento de la URL y en cuerpos de petición, nunca en la ruta)
- [x] 3.3 Claves X25519 por usuario y clave de workspace compartida; rotación al quitar un miembro o salir (guía [workspace-keys](guides/workspace-keys.md))
- [x] 3.4 RBAC en API y UI (miembros, invitaciones, roles, transferencia, salida)
- [x] 3.5 Vista de auditoría (eventos de miembros, invitaciones, sitios, claves y servidores de confianza; solo la leen admins y propietario)
- [x] 3.6 Segundo factor: TOTP y passkeys (WebAuthn) con códigos de recuperación, pantallas de ajustes y segundo paso al iniciar sesión
- [x] 3.7 Panel de administración de la instancia (cuentas, roles, deshabilitar, quitar el segundo factor a quien lo perdió) y política de red editable en caliente; todo auditado en el registro de la instancia
- [x] 3.8 Cuotas y límites de ancho de banda (globales y por persona), transferencias simultáneas y cuotas de sitios, equipos, miembros y volumen diario, editables en caliente por un administrador
- [x] 3.9 Revisión de seguridad de la fase ([informe](security/fase-3.md)): IDOR y acceso entre workspaces, escalada de privilegios entre roles, sellado y rotación de claves, abuso de invitaciones, bypass de 2FA, enumeración de usuarios

Tests obligatorios: aislamiento entre workspaces, sellado y rotación de DEK, matriz de autorización completa, TOTP y passkeys, cobertura de auditoría.

Aceptación: dos usuarios comparten un workspace de equipo con workspaces personales privados; un viewer no puede subir ni ver secretos; quitar a un miembro rota la clave; informe de seguridad cerrado.

## Fase 4: Funciones avanzadas

Preparada al cerrar la fase 3 (2026-09-27). Orden propuesto: cada punto se apoya en el anterior y cierra con sus pruebas al 100 %, un E2E y capturas. Las **vistas previas** de archivos no van en esta fase: son el diferenciador D2, con su propio diseño de seguridad, y se hacen al final.

- [x] 4.1 Comparación de directorios (local-remoto y remoto-remoto) y sincronización con simulación previa ([ADR-0008](adr/0008-directory-compare-and-sync.md)): comparación por tamaño y fecha (60 s de tolerancia) con verificación manual por suma, dirección única elegida antes de correr, nada borra por defecto (casilla y confirmación aparte), ejecución reutilizando subida/bajada/copia entre sitios existentes
- [x] 4.2 Búsqueda remota recursiva: texto simple o patrón con `*`/`?`, resultados en vivo mientras recorre, cancelable, con tope de elementos revisados y de coincidencias para no colgarse en árboles enormes; sin cambios en el backend, reutiliza el listado existente
- [x] 4.3 Subidas atómicas con `.part` (subir a un nombre temporal y renombrar al terminar) y límites de velocidad por sitio, que se suman a los globales y por persona de la 3.8
- [x] 4.4 ProxyJump, proxy SOCKS5/HTTP y keyboard-interactive. Cada salto pasa por `netguard`; decidir si el proxy y el salto pueden apuntar a redes privadas (por defecto no) y cómo se guardan sus credenciales en el vault. Escribir ADR ([ADR-0009](adr/0009-proxyjump-proxy-and-keyboard-interactive.md))
- [x] 4.5 Editor remoto (CodeMirror) con detección de conflictos (comparar tamaño y fecha antes de guardar, guardar con `.part` de 4.3). Leer la guía de CodeMirror antes de usarlo

### Bloque de experiencia tipo FileZilla (anotado el 2026-09-28, va antes que el resto de la fase)

Pedido tras usar la app junto a FileZilla: no se sigue con la 4.13 y siguientes hasta cerrar este bloque. Cada punto termina con pruebas al 100 %, un E2E contra el stack aislado y capturas. El orden va de lo más chico a lo que más cambia la estructura; las pestañas van al final porque se apoyan en el estado de conexión y en los paneles.

- [x] 4.6 Doble clic en un archivo configurable. Por defecto **descarga**, como FileZilla, y Enter hace lo mismo; editar sigue en el menú contextual y en el panel de detalles (botón Editar). Preferencia "Doble clic en un archivo: descargarlo / abrirlo en el editor" en Ajustes, guardada por navegador por ahora (la 4.14 la pasa a las preferencias de la cuenta). De paso: la descarga muestra una tarjeta flotante (preparando → iniciada) y el proxy de desarrollo ya reenvía el WebSocket de eventos, que en local no llegaba
- [x] 4.7 Desbloqueo del vault al conectar. Al abrir un sitio con el vault bloqueado se abre de inmediato el diálogo de desbloqueo, con el nombre del sitio, el host y el usuario, como la contraseña maestra de FileZilla; al desbloquear se conecta solo y el vault sigue abierto el resto de la sesión (como hoy). Cancelar deja el aviso actual con su botón. Lo mismo al probar la conexión o guardar un sitio con secretos
- [x] 4.8 Formulario de sitio al estilo FileZilla. Etiquetas a la izquierda y campos a la derecha; protocolo con su nombre completo; puerto al lado del servidor; **modo de acceso** Normal, Archivo de clave (SFTP) y Anónimo (FTP/FTPS, sin contraseña ni vault; el FTP plano de pruebas acepta anónimos de solo lectura para probarlo de verdad); al editar, los secretos vacíos dicen "Dejar en blanco para mantener…". Pestañas General, Avanzado (zona horaria), Transferencia (límite de velocidad), Juego de caracteres (solo FTP/FTPS) y Proxy y salto
- [x] 4.8b Modo de acceso "Preguntar la contraseña" ([ADR-0010](adr/0010-ask-for-password-logon-type.md), aceptado): la contraseña se pide al conectar, con "Recordar hasta que cierre sesión", y vive solo en la memoria del servidor por sesión y sitio; se borra al cerrar sesión, al bloquear el vault, al deshabilitar la cuenta y al cambiar el sitio, y otra sesión no puede usar las conexiones que abrió. Una contraseña rechazada vuelve a preguntarse. De paso: con OpenSSH real, una contraseña equivocada ya no se reporta como "el servidor no respondió" ni se reintenta cuatro veces
- [x] 4.8c Cifrado de FTP como FileZilla ([ADR-0012](adr/0012-ftp-encryption-modes.md)): un solo protocolo FTP con "Cifrado": TLS explícito si está disponible, Requiere TLS explícito (el de los sitios nuevos), Requiere TLS implícito (en cualquier puerto, 990 por defecto) y Solo FTP plano. "Si está disponible" cae a texto plano cuando el servidor rechaza AUTH TLS, con advertencia en el formulario. `ftps` sigue aceptado como alias y una migración pasó los sitios existentes sin cambiar su comportamiento; el importador de FileZilla conserva su cifrado. La conexión rápida ofrece solo SFTP y FTP, y en FTP usa TLS si el servidor lo ofrece, como FileZilla. De paso: aplicar la zona horaria detectada ya no borra la codificación, el límite de velocidad, el proxy ni el salto del sitio
- [x] 4.8d Modo de acceso Anónimo también para SFTP (método "none" de SSH). "anonymous" pasa a ser un tipo de inicio de sesión propio para SFTP y FTP (migración 00022, que convierte los sitios FTP anónimos que ya existían); el sitio no guarda ningún secreto y siempre entra como `anonymous`. En SFTP se ofrece primero "none" (RFC 4252 §5.2) y luego la contraseña `anonymous@example.com`, como FileZilla, que en FTP también se envía. El importador lleva el tipo 0 a anónimo y el 2 a pedir contraseña. La conexión rápida no lo ofrece. Probado contra OpenSSH real en el stack aislado
- [x] 4.8g Clave privada como archivo: se elige o arrastra el archivo en vez de pegar el texto, y se reconoce por su contenido y no por la extensión: OpenSSH, PEM (RSA, ECDSA, PKCS#8) y PuTTY .ppk v2 y v3, con o sin frase. El servidor la valida al guardarla, muestra el tipo y la huella, y avisa si se subió una clave pública. Hecho: paquete `sshkey` (OpenSSH, PEM, PKCS#8 plano y cifrado con PBES2, RFC 4716, PPK 2 y 3 con Argon2id/Argon2i); las claves PuTTY y PKCS#8 cifradas se guardan convertidas a OpenSSH con la misma frase; `POST /ssh-keys/inspect` describe el archivo sin guardarlo y el formulario lo muestra al elegir o arrastrar; guardar rechaza clave pública, frase faltante o incorrecta y tipos no usados (DSA, Ed448, Argon2d), también para la clave del salto y las conexiones rápidas; los costos de bcrypt, Argon2 y PBKDF2 que pide un archivo tienen tope y corren de a dos (ADR-0013). Probado con una PPK real contra OpenSSH en el stack aislado. Hueco anotado para la 4.18: la API no limita el tamaño de los cuerpos JSON en general
- [x] 4.9 Registro de la conexión en vivo. El backend publica por el WebSocket cada paso mientras conecta y trabaja: resolviendo el nombre, conectando a host:puerto (directo, por proxy o por salto), huella del servidor verificada, autenticado (método, nunca el secreto), listando la ruta, listado correcto con cantidad de elementos, y cada error con su código estable. Panel "Registro" arriba, como FileZilla: por pestaña/sitio, con hora, copiable y con un tope de líneas en memoria. Prueba que ningún mensaje contiene contraseñas, claves ni frases de paso. ADR-0011. Hecho: paquete `connlog` y evento `connection.log`; el conector cuenta conectando (directo, proxy o salto), la dirección alcanzada, la clave o certificado verificado, desconocido o cambiado, FTP sin TLS en modo opcional y la cuenta que inició sesión; el listado cuenta "listando" ya conectado, el resultado y los fallos con su código. Panel "Registro" arriba del explorador con hora, colores por nivel, copiar, limpiar, plegar y 500 líneas por sitio. De paso: el pool ya no reutiliza la conexión de una operación cancelada, que en FTP queda cortada (causa de fallos intermitentes en los e2e de FTP). Ajuste del 2026-09-29: al volver a abrir un sitio que se había desconectado (botón, cerrar la pestaña o abrir otro sitio), el registro empieza de cero; un reintento tras un fallo mantiene el fallo arriba
- [x] 4.9b Barra de herramientas y conexión en foco (pedido urgente del 2026-09-28). Una barra arriba de la vista, a lo ancho, pensada para crecer (después: mostrar u ocultar paneles, la 4.10). Acciones sobre la conexión en foco, que es el panel donde el usuario hizo clic o puso el foco por última vez (el principal al abrir un sitio): Actualizar (vuelve a listar la carpeta en vista), Cancelar la operación en curso (aborta el listado o la conexión que se está haciendo; el servidor corta la operación y el registro dice "cancelado", no un error) y Desconectar (cierra las conexiones del usuario con ese sitio en el pool, lo dice en el registro y deja el explorador en estado "Desconectado" con un botón para reconectar; volver a abrir el sitio también reconecta). Primer interruptor de panel: mostrar u ocultar el Registro. Endpoint `DELETE /workspaces/{ws}/sites/{id}/connection`. De paso, arreglo: pasar el mouse sobre una carpeta ya no la lista por adelantado (usaba conexiones y recursos del servidor sin que el usuario lo pidiera); se lista solo al abrirla. Hecho: la barra está sobre el explorador (empieza donde termina la barra de sitios, solo en el inicio y los sitios), con íconos de color y tooltips del tema; desconectar además saca el sitio de la URL (el panel principal vuelve al inicio y el segundo panel se cierra), así recargar no reconecta. Regla nueva para todo el proyecto: los estados de un panel (desconectado, cancelado, error, confiar la clave, pedir la contraseña, vault, carpeta vacía) son una tarjeta centrada en ambos ejes (`PanelState`)
- [x] 4.10 Paneles redimensionables. Separadores arrastrables entre la barra de sitios, el explorador y el panel de detalles, y entre el registro (arriba), el explorador y las transferencias (abajo); los tamaños se guardan por navegador y doble clic en un separador los restablece. Accesible con teclado (separadores con flechas) y cada panel se puede ocultar desde el menú. Leer la guía de la librería elegida antes de usarla (candidata: `react-resizable-panels`). Hecho con `react-resizable-panels` 4.9 (guía en `docs/guides/resizable-panels.md`): separadores entre sitios y explorador, registro y archivos, archivos y detalles, los dos paneles de la vista dividida, y el contenido y las transferencias; tamaños por navegador, doble clic restablece, teclado (flechas, Enter pliega, F6 salta). Sitios, registro, detalles y transferencias se ocultan desde la barra; el panel de sitios oculto deja un riel (mostrar, explorar los sitios en un cajón lateral que se cierra al elegir, conexión rápida) y Ctrl+B lo alterna. Los paneles angostos muestran solo íconos en la cabecera y los breadcrumbs quedan en una línea
- [x] 4.11 Estado de conexión en la barra de sitios. Punto verde en los sitios y conexiones rápidas con una conexión abierta en el pool, y ninguno cuando no; el backend expone las conexiones abiertas del usuario (endpoint y evento `connection.state` al abrirse o cerrarse, apoyado en `connections.Manager`). Acción "Desconectar" en el menú del sitio, que cierra su pool. Al pasar de un sitio a otro en la misma vista, la conexión anterior se cierra en el acto (pedido del 2026-09-28) en vez de esperar los 5 minutos de inactividad del pool, salvo que la use otra pestaña, el otro panel de la vista dividida o una transferencia en curso: esas conexiones se cierran solas al quedar libres. Hecho: el pool avisa cuando un sitio queda con o sin conexiones (`connection.state`) y `GET /workspaces/{ws}/connections` da las abiertas; punto verde en sitios y conexiones rápidas, con texto para lectores de pantalla; "Desconectar" en el menú del sitio conectado (y en la barra desde la 4.9b). Al abrir otro sitio desde la barra de sitios se cierran las conexiones del anterior; una transferencia en curso termina antes de que su conexión se cierre. De paso: abrir el menú de un sitio o elegir en él ya no abre ese sitio
- [x] 4.12 Pestañas de conexión: varias conexiones a la vez. Cada pestaña guarda su sitio, su ruta, su selección y su vista dividida; abrir un sitio lo muestra en la pestaña activa, y en una nueva con clic medio, Ctrl+clic o "Abrir en pestaña nueva" (si el sitio ya tiene pestaña, la activa). La pestaña muestra el punto verde de la 4.11 y el registro de la 4.9 sigue a la pestaña activa. Cerrar una pestaña no corta en el acto (el pool se cierra por inactividad) salvo con "Desconectar". Las pestañas abiertas se recuerdan por navegador; atajos Ctrl+Tab y Ctrl+W. La URL refleja la pestaña activa, así que recargar vuelve a lo mismo. Hecho: pestañas por navegador con su sitio, carpeta, vista dividida y selección; clic abre en la pestaña activa y cierra las conexiones del sitio que reemplaza (salvo que el segundo panel de otra pestaña lo muestre), Ctrl+clic, clic medio o "Abrir en una pestaña nueva" abren otra, y un sitio con pestaña la activa; punto verde en la pestaña; cerrar una pestaña desconecta los sitios de sus dos paneles que ninguna otra pestaña muestre (corregido el 2026-09-29, antes no cortaba la conexión); la URL sigue a la pestaña activa y recargar vuelve a lo mismo. Atajos: Ctrl+Tab, Ctrl+Mayús+Tab y Ctrl+W donde el navegador los deja (en el escritorio siempre), y Alt+AvPág, Alt+RePág y Alt+W en todas partes

### Resto de la fase

- [x] 4.13 Velocidad de descarga por SFTP. Reportado el 2026-09-28: un archivo grande que FileZilla baja casi al instante tarda mucho en Transferly desde el mismo servidor. Medir antes de tocar nada: comparar contra FileZilla y `sftp` con el mismo archivo, separar el tramo servidor SFTP → Transferly del tramo Transferly → navegador, y revisar las sospechas: lecturas SFTP en serie en vez de concurrentes (el spike de la fase 0 ya mostró que las lecturas en paralelo importan con latencia), el tamaño de bloque, los límites de velocidad y el shaper de la 4.3, el contador de progreso y el proxy de desarrollo. Arreglar lo que la medición señale y dejar una prueba de rendimiento que evite volver atrás. Hecho: medido en el stack aislado con 20 ms de latencia agregada (`scripts/bench-sftp-download.sh`): OpenSSH 31 MB/s, Transferly 1,5 MB/s. Causa: `io.LimitReader` y el envoltorio del tracker ocultaban el `WriteTo` de `pkg/sftp`, así que la descarga pedía un bloque de 32 KB por ida y vuelta. Ahora ambos conservan `WriteTo` (con progreso, límite de velocidad y cancelación por bloque) y Transferly baja a 39-57 MB/s con la misma latencia, por encima de OpenSSH. `TestRemoteFiles_DownloadReadsAhead` evita volver atrás. Las subidas ya usaban `ReadFrom` concurrente
- [x] 4.13b Servidores que cambian de clave de host. Reportado el 2026-09-28: un servidor SFTP presenta una clave distinta en cada conexión (probablemente varios nodos detrás de un balanceador, cada uno con su clave). WinSCP lo resuelve: avisa y ofrece "Actualizar" (reemplazar), "Agregar" (confiar en varias claves para el mismo host:puerto) o "Conectar una vez". FileZilla falla ("socket unexpectedly closed") y Transferly lo muestra como clave cambiada, con solo "Reintentar". Plan: reproducirlo en el stack aislado con un SFTP que alterne claves; permitir varias claves de confianza por host:puerto (hostkey ya guarda por tipo); en el aviso de clave cambiada, mostrar la huella nueva y la anterior y ofrecer reemplazar, agregar o cancelar, sin una opción "conectar igual" silenciosa; auditar cada decisión. No probar contra el servidor real del usuario sin su permiso explícito. Hecho: `known_hosts` guarda varias claves por host, puerto y tipo (la huella entra en la clave primaria); la clave cambiada devuelve las huellas de confianza en `params.trusted`; `POST /known-hosts` pide `mode` `add` o `replace` para una clave cambiada y sin él la sigue rechazando; `DELETE /known-hosts` acepta `fingerprint` para olvidar una sola; cada decisión queda auditada con huella y modo. El explorador muestra un aviso de peligro con las huellas de confianza y la nueva, y ofrece "Confiar también en esta clave", "Reemplazar las claves de confianza" o "Cancelar y desconectar", sin "conectar una vez" (ADR-0014). Servidores de confianza lista y olvida cada clave por separado. Reproducido en el stack aislado con un segundo nombre (`sftp-rotating`) para el mismo SFTP y una huella ajena de confianza; no se probó contra el servidor real del usuario. Pendiente menor: la conexión rápida y "Probar conexión" siguen mostrando la clave cambiada solo como error
- [x] 4.14 Paleta de comandos, preferencias y marcadores. Dividida el 2026-09-29:
- [x] 4.14a Preferencias en la cuenta: lo que hoy se guarda por navegador y es un gusto de la persona (doble clic en un archivo) pasa a las preferencias de la cuenta, con migración desde el valor del navegador; lo que depende de la pantalla (tamaños y paneles abiertos) sigue en el navegador. Hecho: columna `file_double_click` en `users` (download o edit), `fileDoubleClick` en `PATCH /me` y en el usuario, validado (`invalid_file_double_click`); Ajustes lo guarda en la cuenta y avisa si no pudo; al conocer la sesión manda la cuenta, y la primera vez un navegador que había elegido abrir en el editor sube esa elección en vez de perderla. El idioma de la interfaz también pasó a la cuenta (pedido del 2026-09-29): manda el de la cuenta, y la primera vez un navegador en otro idioma que el inglés con que nace toda cuenta sube el suyo, en la misma petición que el doble clic. Seguir el sistema y los tamaños de paneles quedan en el navegador
- [x] 4.14b Marcadores por sitio, como FileZilla: guardar la carpeta en vista con un nombre, listarlos y abrirlos desde el explorador, renombrarlos y borrarlos; guardados en el servidor por sitio y auditados. Hecho: paquete `bookmark` y tabla `site_bookmarks` (se borran con el sitio); `GET/POST /sites/{id}/bookmarks` y `PATCH/DELETE /sites/{id}/bookmarks/{id}`; nombres únicos por sitio sin distinguir mayúsculas, rutas absolutas y normalizadas, hasta 200 por sitio; quien puede ver el espacio los lista y abre, quien gestiona sitios los crea, cambia y borra, y cada cambio queda en la auditoría (`bookmark.created/updated/deleted`, con su propia categoría). En el explorador, un botón de marcadores junto a la ruta: se rellena si la carpeta en vista está marcada, y su menú abre un marcador, marca la carpeta sugiriendo su nombre y abre "Administrar marcadores" para renombrar, cambiar la carpeta o eliminar. Las conexiones rápidas no tienen marcadores. La paleta (4.14c) los va a listar
- [x] 4.14e Tipo de archivo, como FileZilla: columna "Tipo" en la tabla y fila en el detalle, deducido de la extensión en el navegador (sin leer el archivo): unas 150 extensiones comunes, extensiones dobles (.tar.gz), nombres conocidos sin extensión (Dockerfile, Makefile, .htaccess), "Archivo XYZ" para las desconocidas; ícono por categoría (imagen, código, comprimido, documento, audio, video). Pedido del 2026-09-29; la misma tabla servirá a los visores. Hecho: `file-types.ts` (tabla de extensiones y nombres, sin leer el archivo), columna "Tipo" que se oculta en paneles angostos, "Tipo" en el detalle e íconos por categoría
- [x] 4.14f Doble clic para conectar desde el árbol de sitios: un clic selecciona, doble clic o Enter conecta; clic medio y Ctrl+doble clic abren en pestaña nueva; el riel de íconos sigue con un clic. Pedido del 2026-09-29. Hecho: Espacio selecciona, las carpetas del árbol siguen abriéndose con un clic y el texto de inicio lo explica
- [x] 4.14g Barra de marcadores tipo Chrome: en la barra principal del explorador, los marcadores del sitio como chips mientras quepan y un botón "»" con el resto; solo si el sitio tiene marcadores. Pedido del 2026-09-29. Hecho: a pedido del usuario los chips quedaron en la misma barra que la ruta, alineados a la derecha junto a la estrella (no en una barra aparte); el chip de la carpeta en vista se marca; los que no caben se miden y pasan al botón "»"
- [x] 4.14h Mover sitios y carpetas arrastrando en el árbol: de una carpeta a otra o a la raíz, sin permitir meter una carpeta dentro de sí misma; "Mover a…" en el menú para teclado; el árbol sigue en orden alfabético (un orden manual sería otro punto). Pedido del 2026-09-29. Hecho: `POST /sites/{id}/move` (no vuelve a revisar el host, auditado como cambio del sitio); soltar sobre una carpeta, o sobre un sitio dentro de ella, mueve ahí y la abre; soltar en el espacio libre del árbol mueve a la raíz; se marca el destino mientras es válido; quien solo ve no puede arrastrar. Para teclado basta "Editar" del sitio o de la carpeta, que ya permitían elegir la carpeta, así que no hizo falta un "Mover a…"
- [x] 4.14i Reconectar en la carpeta de inicio y carpeta remota inicial por sitio. Pedido del 2026-09-29: tras desconectar, volver a abrir el sitio retomaba la carpeta donde se había quedado. Hecho: desconectar devuelve las pestañas del sitio (en cualquiera de sus paneles) a su carpeta de inicio y olvida la selección; esa carpeta es la raíz o la "Carpeta remota inicial" que ahora se configura en Avanzado (columna `remote_dir`, ruta absoluta normalizada, `invalid_remote_dir`), como el "Directorio remoto predeterminado" de FileZilla, que también se lee al importar su Site Manager. Las conexiones rápidas empiezan en la raíz
- [x] 4.14j Pasada de interacción en todo el sistema (pedido del 2026-09-29): todo lo clickeable muestra la manito (regla base, Tailwind v4 deja la flecha en los botones), los botones "ghost" y "outline" cambian al color `accent` de cada tema bajo el puntero (antes en tema oscuro casi no se notaba), las pestañas reaccionan, y las acciones con texto de la barra del explorador se ven como botones con borde. Un e2e recorre lo clickeable del explorador y exige la manito y el cambio de color
- [x] 4.14k Selectores propios en todo el sistema (pedido del 2026-09-29): las listas nativas del navegador no siguen los temas; un componente Select sobre Base UI, con la misma apariencia que los menús, reemplaza cada NativeSelect y los selectores de mes y año del calendario, con teclado y lector de pantalla. Hecho: `components/ui/select.tsx` y `form-select.tsx`; se reemplazaron los 20 selectores (diálogo de sitios, colecciones, conexión rápida, miembros, auditoría, importación, transferencias, panel dividido, detalle, copia, ajustes) y los de mes y año del calendario, que además no abrían porque la barra de flechas los tapaba; se borró `native-select.tsx`; helpers `choose` y `optionsOf` para tests y e2e
- [x] 4.14c Paleta de comandos (Ctrl+K): buscar y abrir sitios, pestañas y marcadores, y ejecutar las acciones de la barra de herramientas (actualizar, cancelar, desconectar, mostrar u ocultar paneles), ir a ajustes; accesible con teclado y lector de pantalla. Hecho: cmdk (guía `docs/guides/cmdk.md`) dentro del Dialog de Base UI; Ctrl+K o ⌘K y un botón en la barra superior; grupos Acciones (actualizar, cancelar, desconectar, filtrar la carpeta, conexión rápida), Pestañas, Marcadores del sitio en foco (se abren en su panel, también el segundo), Sitios (buscables por host y usuario, con el punto verde), Paneles e Ir a (según rol y tipo de espacio); cada apertura empieza con la búsqueda vacía. Los atajos se escriben según el sistema (`lib/shortcuts.ts`: Ctrl+K en Windows y Linux, ⌘K en Mac), también los que ya había en la interfaz
- [x] 4.14d Filtro instantáneo sobre la carpeta ya listada (nombre o patrón, extensión, rango de fechas), sin ir al servidor. Diseño del 2026-09-29:
  - Caja "Filtrar esta carpeta" en la barra del explorador; `/` o Ctrl+F (con el foco en el explorador) la enfocan, Esc la limpia y luego devuelve el foco a la tabla, flecha abajo pasa a la tabla
  - Texto: sin distinguir mayúsculas ni tildes; varias palabras deben aparecer todas; `*` y `?` son comodines sobre el nombre completo (`*.log`, `informe-202?.pdf`); `-palabra` excluye; lo que coincide se resalta en el nombre
  - Botón "Filtros" con contador: Tipo (carpetas, imágenes, audio y video, documentos, código, comprimidos, otros, de la tabla de la 4.14e), Modificado (hoy, 7 días, 30 días, este año, rango de fechas en la zona horaria de la cuenta) y Tamaño (menos de 1 MB, 1 a 100 MB, más de 100 MB; solo archivos)
  - Con un filtro activo, una fila bajo la barra dice "12 de 340", muestra cada filtro como chip que se quita, cuánto hace que se listó la carpeta con un enlace para actualizar, y "Quitar filtros"; si nada coincide, un aviso centrado con "Quitar filtros"
  - El filtro vive en el panel y se reinicia al cambiar de carpeta; la selección y el teclado recorren solo lo visible
  - Frescura: filtrar no pide nada al servidor, pero ninguna acción usa la caché para el contenido: descargar, editar, renombrar o borrar van al servidor por la ruta, así que siempre actúan sobre el archivo actual. El listado filtrado es el mismo que se ve sin filtro (se refresca solo tras 15 s al volver a la pestaña o entrar a la carpeta, con Actualizar y tras cada cambio propio), y la fila del filtro dice su antigüedad
  - Hecho: `entry-filter.ts` (texto, comodines, exclusión, grupos de tipos, fechas en la zona de la cuenta, tamaños y rangos para resaltar), caja con atajos (`/` también sin foco en el panel principal), menú de filtros con un calendario propio sobre DayPicker (guía `docs/guides/daypicker.md`; el rango se elige en un solo calendario), fila de resumen con antigüedad del listado, aviso de nada coincide y e2e que comprueba que filtrar no vuelve a listar. De paso: las cabeceras de la tabla del panel local quedaban en dos filas
- Movidas al final (pedido del 2026-09-28): 4.8e y 4.8f van después de la 4.14, antes de la 4.15
- [x] 4.8e Modo de acceso "Cuenta" para FTP: un tercer dato (comando ACCT) que algunos servidores antiguos piden además de usuario y contraseña, sellado en el vault. Hecho: copia parcheada de jlaffaye/ftp en `backend/third_party/ftp` que responde al 332 con ACCT tras USER o PASS (la librería no permite comandos propios y envolver la conexión no sirve con FTPS explícito; ADR-0015); modo de acceso `account` solo para FTP, cuenta sellada en el vault (tipo `account`), otros modos la borran; el importador lee el tipo 4 de FileZilla; el diálogo de sitios ofrece "Cuenta" con contraseña y cuenta. Probado con un servidor FTP guionado que pide la cuenta; el stack de pruebas no tiene uno, así que el e2e solo guarda y conecta un sitio así
- [x] 4.8f Modo de acceso "Interactivo" para SFTP y FTP: las preguntas del servidor (por ejemplo un código de un solo uso) se muestran en vivo y se responden desde el navegador mientras se conecta. Hecho (ADR-0016): broker `core/prompts` que publica `connection.prompt` por el WebSocket y espera la respuesta (`POST /prompts/{id}`), el rechazo (`DELETE`), el fin de la petición o 3 minutos, y siempre avisa `connection.prompt_closed` a todas las pestañas; un prompt es de su usuario (otro recibe `prompt_not_found`) y las respuestas nunca se guardan ni se registran. SFTP reenvía cada desafío keyboard-interactive y pide la contraseña a servidores que solo aceptan `password`; FTP pide la contraseña tras USER mostrando la respuesta del servidor (nuevo `LoginInteractive` en la copia parcheada), con la ventana de respuesta sumada a su timeout. Sin secretos guardados; el importador lee el tipo 3 de FileZilla; el log de conexión dice que el servidor preguntó; diálogo global que muestra un prompt a la vez, en cola, con respuestas ocultas salvo que el servidor diga lo contrario. e2e contra el OpenSSH y el vsftpd del stack aislado
- [x] 4.15 Exportación cifrada del vault (formato con contraseña propia, Argon2id y AES-GCM; nunca incluye la clave del workspace en claro). Hecho (ADR-0017): paquete `core/backup`; el archivo es un JSON con cabecera en claro (formato, versión, costo Argon2id y sal, ligados al sello como datos adicionales) y el contenido sellado con AES-256-GCM: ajustes de cada sitio, ruta de nombres de su colección, colecciones vacías y secretos abiertos con la clave de la sesión y vueltos a sellar; la clave del workspace, la DEK y los blobs de la base nunca salen. Exportar pide gestionar sitios y el vault desbloqueado si hay secretos, se audita (`sites.exported`) y responde como descarga; la contraseña tiene al menos 12 caracteres y no se guarda. Importar reutiliza el motor del import de FileZilla (generalizado a entradas completas: vista previa, política de nombres, colección destino, secretos opcionales); contraseña incorrecta o archivo alterado es `wrong_export_password`, archivo ajeno `invalid_export`, más de 4 MiB `import_file_too_large`, y un costo sobre 256 MiB, 10 pasadas u 8 hilos se rechaza antes de derivar. En la interfaz: botón "Exportar sitios" con contraseña y confirmación, y el diálogo de importación reconoce por contenido una exportación de Transferly y pide su contraseña. e2e: exportar, descargar y volver a abrir con su contraseña
- [x] 4.16 Mensajes de error guiados (cada código estable del servidor con una causa probable y un siguiente paso). Hecho: catálogo `errors` (inglés y español) con causa probable y qué hacer para cada uno de los 123 códigos que responde la API, más `network_error` del cliente; componente `ErrorGuide` que lo muestra bajo el mensaje, con el código para citar al pedir ayuda, en el explorador, la conexión rápida, la prueba de conexión, los diálogos de sitio, credenciales, vault, importación y exportación; los códigos derivados del salto (`jump_*`) usan la guía de la clave del sitio. Una prueba en Go lee los códigos de las fuentes de la API y falla si alguno no tiene guía en todos los idiomas, así un código nuevo no llega sin explicación
- [x] 4.17 Más idiomas (estructura para traducciones de la comunidad y prueba que exige las mismas claves en todos los idiomas). Hecho, sin idiomas nuevos (pedido del 2026-09-29: solo la estructura): los idiomas se registran en una línea de `i18n/languages.ts` con su nombre escrito en sí mismo, y las carpetas de `locales/` se cargan solas; el menú de idioma muestra esos nombres, así un idioma nuevo no obliga a traducir el nombre de los demás. Las pruebas de traducción fallan con una carpeta sin registrar, un idioma registrado sin carpeta, claves faltantes o sobrantes, mensajes vacíos o variables distintas del inglés. Guía para traductores en `docs/guides/i18n.md` (qué no traducir, variables, RTL) enlazada desde CONTRIBUTING
- [x] 4.18 Revisión de seguridad de la fase: XSS en el editor, conflictos y pérdida de datos en sync, fuga de secretos al exportar, abuso de proxies como pivote de red, secretos que se filtren en el registro de conexión, y abuso de conexiones simultáneas desde varias pestañas. Hecho: informe en [docs/security/fase-4.md](security/fase-4.md). Tres hallazgos medios corregidos con regresión (la sincronización marcaba por defecto actualizaciones que pisaban un archivo más nuevo del destino; un editor podía exportar los secretos del equipo, ahora solo administradores y propietario; los cuerpos de petición no tenían tope, ahora 1 MiB salvo importaciones y subidas) y dos endurecimientos (credenciales FTP con saltos de línea y tope de 16 prompts por usuario). govulncheck, pnpm audit y Trivy sin vulnerabilidades. Fase 4 cerrada

Pendiente de fases anteriores que conviene retomar aquí: la opción por sitio para algoritmos SSH heredados, la importación del `EncodingType` de FileZilla y el juego de caracteres de la conexión rápida.

Anotado el 2026-09-27, pendiente de decidir en qué punto entra: filtro instantáneo sobre la carpeta ya listada (distinto de la búsqueda recursiva remota de la 4.2), aprovechando que el listado ya vive en la caché de TanStack Query — sin ida y vuelta al servidor. Permitiría filtrar por nombre/patrón, tipo de archivo (extensión) y rango de fecha de modificación sobre las filas ya renderizadas. Encaja bien como parte de la 4.14 (paleta de comandos y preferencias) o como su propio punto pequeño justo después; no se mete en la 4.3 porque no tiene relación con subidas atómicas ni límites de velocidad.

## Bloque D2: visualizador de archivos (pedido del 2026-09-29, antes de la fase 5)

El diferenciador D2 se adelanta a la fase 5. Abrir cualquier archivo y verlo dentro de Transferly sin descargarlo: a pantalla completa al estilo Quick Look (espacio para abrir, flechas para el anterior y el siguiente de la carpeta, Esc para cerrar), para archivos remotos, locales y dentro de comprimidos. Office se muestra en el navegador (sin contenedor de LibreOffice). Decisiones en [ADR-0018](adr/0018-file-preview.md).

- [x] D2.0 Base: visor a pantalla completa, detección del tipo por contenido y extensión, lectura por rangos de archivos remotos y locales, página aislada (`iframe` con `sandbox`, origen opaco, sin cookies, API ni red) para los formatos riesgosos, visor hexadecimal para cualquier binario, y la carpeta `preview-samples/` sembrada en el SFTP de pruebas con una muestra por formato y muestras hostiles. Hecho: `QuickLook` a pantalla completa (Espacio, menú "Vista previa" o doble clic si se elige en Ajustes; flechas, Esc, descargar, abrir en el editor), para el explorador remoto y el panel local; fuentes que leen por rangos (`Range` sobre el endpoint de contenido, `File.slice` en local); detección por firmas de bytes y extensión (`detectKind`); visor hexadecimal virtualizado que lee páginas de 4 KiB; página `/sandbox/viewer.html` con CSP `sandbox allow-scripts`, sin red y enmarcable solo por Transferly, con su protocolo de mensajes (e2e: origen `null`, sin cookies, sin acceso al padre, red bloqueada); muestras en `testenv/preview-samples`, montadas en solo lectura como `/preview-samples` en el SFTP de los stacks de prueba y desarrollo
- [x] D2.1 Texto y código con resaltado, Markdown sanitizado, JSON, YAML y XML en árbol, CSV y TSV como tabla virtualizada. Hecho: texto y código en CodeMirror de solo lectura (resaltado por lenguaje, búsqueda, ajuste de líneas; hasta 5 MiB, más grande ofrece los bytes); Markdown con marked y DOMPurify dentro de la página aislada (sin scripts, manejadores, marcos, formularios ni enlaces `javascript:`, los enlaces no navegan y las imágenes remotas no cargan), con la fuente a un clic; JSON, JSON Lines, YAML (con el tope de alias contra la bomba de alias) y XML como árbol plegable por páginas de 200, con los errores de lectura explicados; CSV y TSV con Papa Parse como tabla virtualizada (separador detectado, encabezado opcional, orden por columna; hasta 20 MiB). De paso se corrigió un fallo de producción: la política de seguridad bloqueaba los estilos de CodeMirror y el editor remoto se veía sin formato; ahora la página recibe un nonce por respuesta para esos estilos. Guía en `docs/guides/preview-libraries.md`
- [x] D2.2 Imágenes: png, jpg, gif, webp, avif, bmp, ico, svg (como imagen, sin scripts) y tiff, con zoom, rotación y datos EXIF. Hecho: el navegador dibuja png, jpg, gif animado, webp, avif, bmp e ico desde una URL `blob:` (la política de la página ahora permite `blob:` solo para imágenes); SVG como `<img>` con su tipo, así sus scripts, manejadores, enlaces e imágenes remotas nunca corren ni cargan; TIFF decodificado con UTIF.js en un canvas, con un tope de 100 megapíxeles antes de decodificar; zoom por pasos, ajustar, 100% y rotación (también con + - 0 R), dimensiones, y panel de información con la EXIF leída por exifr (cámara, fecha, exposición, apertura, ISO, distancia focal, programa y ubicación); una imagen que no se puede decodificar lo dice y ofrece sus bytes. Muestras regenerables con `generate-images.sh`
- [x] D2.3 Audio y video por rangos con el reproductor del navegador; códecs no soportados avisan y ofrecen descargar. Hecho: el reproductor del navegador pide el archivo por rangos a la dirección de descarga (las peticiones con rango no crean transferencias ni pasan por la cola) o, en local, a una URL `blob:`; la política de la página permite `media-src` `self` y `blob:`; velocidades de 0.5× a 2×, duración y tamaño del video; un formato o códec que el navegador no reproduce (como un AVI con MPEG-4 Parte 2) lo dice y ofrece los bytes; el AVI se reconoce por su firma. ffmpeg entra a la imagen de herramientas para regenerar las muestras (`generate-media.sh`)
- [x] D2.4 PDF con pdf.js: páginas, miniaturas, búsqueda y carga por rangos, sin JavaScript del documento. Hecho: pdf.js 6 dentro de la página aislada, con su worker empaquetado y levantado desde `blob:`; la página recibe el primer MiB y pide el resto por mensajes en trozos de 64 KiB, que el explorador lee del archivo (un PDF de 3,7 MB abre su primera página leyendo menos de la mitad); páginas dibujadas al verse, miniaturas, zoom, ajustar al ancho y búsqueda en el texto; el JavaScript del documento nunca corre (e2e con un PDF con `OpenAction`), XFA y WASM apagados; los PDF con contraseña y los dañados lo dicen y ofrecen los bytes. La página aislada reenvía Esc y las flechas para que el visor responda aunque el foco esté dentro, y cada renderizador se carga solo cuando hace falta (pdf.js no pesa en el Markdown). Muestras con `generate-pdfs.py`
- [x] D2.5 Comprimidos (zip, jar, tar, tar.gz, gz, bz2, xz, zst, 7z, rar): árbol navegable leído en el servidor sin traer el archivo entero, vista previa de sus archivos con los mismos visores y descarga de una entrada, con topes contra bombas de descompresión y rutas hostiles. Hecho: paquete Go `internal/core/archives` que lee ZIP y 7z por su índice al final del archivo (lecturas por bloques de 64 KiB con caché, que reusan el flujo abierto al leer en orden), y TAR, TAR con gzip/bzip2/xz/zstd, comprimidos sueltos y RAR como flujos; topes de 10 000 entradas, 512 MiB por entrada, 1 GiB descomprimido al recorrer, razón 200:1 como bomba y ventanas de xz, zstd y RAR a 64 MiB; archivos anidados hasta cuatro niveles (en memoria, hasta 64 MiB); los fallos del almacenamiento se informan como tales y no como archivo dañado. Endpoints `files/archive-entries` y `files/archive-entry` con errores guiados (`archive_unsupported`, `archive_entry_not_found`, `archive_too_large`, `archive_bomb`, `archive_encrypted`). En el explorador, un árbol virtualizado con formato, totales, filtro, tamaño, compresión y fecha; los nombres peligrosos (`../`, absolutos, unidades de Windows) quedan arriba con su nombre completo y marcados, y las razones de bomba se marcan; Enter, Espacio o doble clic abren una entrada con su visor (un ZIP dentro de un ZIP incluido) y Retroceso vuelve. Los archivos del equipo se leen en el navegador sin enviarlos: ZIP (con ZIP64), TAR, TAR.GZ y GZ con el `DecompressionStream` del navegador y los mismos topes; 7z, RAR, bzip2, xz y zstd locales remiten al servidor. Muestras con `generate-archives.py` (RAR5 escrito a mano, bomba, rutas hostiles, 12 000 entradas, ZIP cortado, ZIP y 7z con contraseña)
- [x] D2.6 Office en el navegador: docx y odt como documento, xlsx, xls y ods como planilla con hojas, pptx como diapositivas. Hecho: todo se lee y dibuja dentro de la página aislada, cargado solo cuando se abre un documento (hasta 50 MiB). Word con docx-preview página por página (estilos, tablas, imágenes, encabezados y pies; viñetas de Symbol/Wingdings convertidas a Unicode; sin el HTML embebido `altChunk`); planillas con SheetJS 0.20.3 desde su CDN oficial (la copia de npm tiene CVE conocidos), una pestaña por hoja con letras de columnas y números de fila, celdas combinadas y formatos, sin calcular ni ejecutar nada (fórmulas con su resultado guardado, sin macros ni HTML de celdas), hasta 10 000 filas y 200 columnas avisando si hay más; odt y pptx leídos por código propio desde su XML (solo elementos conocidos, texto siempre como texto, colores hexadecimales y estilos de una lista permitida), las diapositivas en proporción con fondo, cuadros de texto, formas, imágenes, tablas y marcadores heredados del diseño y el maestro. Los protegidos con contraseña y los dañados lo dicen y ofrecen los bytes; `.doc` y `.ppt` antiguos van a hexadecimal. Además, arreglos del modo desarrollo (imágenes que se revocaban por el doble montaje de React y el worker de pdf.js) botón de vista previa en el panel de detalles, y PDF con contraseña: se pide dentro de la página aislada, nunca sale de ella y se olvida al cerrar. Muestras con `generate-office.py` (python-docx, python-pptx, XlsxWriter, xlwt y odfpy en la imagen de herramientas)
- [x] D2.7 SQLite como navegador de tablas de solo lectura, fuentes, certificados y claves públicas, correo eml. Hecho: SQLite con sql.js en su versión asm.js dentro de la página aislada (sin relajar su política con WebAssembly), tablas y vistas, filas por páginas de 200, estructura y consultas con `PRAGMA query_only` (a lo más 1000 filas); fuentes cargadas con `FontFace` (el navegador las valida) con nombre, formato, glifos, muestras en seis tamaños con texto propio y la grilla de sus caracteres leída de `cmap`; certificados y claves leídos por un lector DER propio y estricto en la página principal: cadenas PEM, DER, claves públicas, solicitudes y claves OpenSSH, con vigencia, sujeto, emisor, nombres alternativos, usos y huellas SHA-256/SHA-1 (@noble/hashes), y las claves privadas nunca se muestran; correos con postal-mime: encabezados como texto, cuerpo HTML saneado con DOMPurify en un shadow root dentro de la página aislada, imágenes `cid:` en su lugar, sin pedir nada remoto (también en desarrollo; el mismo resguardo se aplicó al Markdown), texto plano, encabezados crudos y adjuntos que se abren con su visor o se descargan. Además, los archivos sin visor dicen que no hay visor en vez de abrir el hexadecimal. Muestras con `generate-misc.py` (fontTools, openssl y ssh-keygen en la imagen de herramientas)
- [x] D2.8 Pasada e2e con todas las muestras, revisión de seguridad del visor (`docs/security/d2.md`) y cierre. Hecho: e2e del visor, el panel local y el explorador en verde contra la imagen de producción; revisión STRIDE, ataques conocidos, dependencias (`govulncheck` y `pnpm audit` limpios) y cinco hallazgos corregidos, sin hallazgos abiertos. El bloque D2 queda cerrado

## Marca Transferly (pedido del 2026-10-01)

El producto se publica como Transferly: un solo nombre en código, operación e interfaz, y la marca diseñada en `branding/`. Decisiones en [ADR-0020](adr/0020-transferly-name-and-brand.md).

- [x] Kit de marca en `branding/`: símbolo, logos horizontal, vertical y monocromo, íconos de aplicación y favicon, paleta (Core Blue, Action Orange, Signal Green), tipografía e íconos, todo en SVG y generado por `branding/scripts/build.py`
- [x] Renombre: módulo Go `github.com/fprambs/transferly`, binario `cmd/transferly`, variables `TRANSFERLY_*`, stacks de Docker, rol y bases de Postgres, usuarios de los servidores de prueba, cookie de sesión, exportación cifrada `transferly-export` y su importación, claves del navegador y mensajes de la página aislada; las etiquetas de cifrado del vault y del segundo factor se conservan
- [x] Temas Transferly Dark y Transferly Light con la paleta de la marca, migración 30 de los temas guardados, logo en la barra superior, el inicio de sesión y las invitaciones, y favicon nuevo
- [x] Muestras de `testenv/preview-samples` regeneradas con el nombre nuevo
- [ ] Líneas base visuales de Playwright regeneradas y E2E en verde con la marca nueva

## Color con función (pedido del 2026-10-01)

La interfaz usaba casi solo grises: el color pasa a decir algo, siempre desde los tokens del tema. Decisiones en [ADR-0021](adr/0021-site-color-labels.md).

- [x] Color por estado: barras de transferencia con franjas de marca mientras avanzan y colores de éxito, error y pausa; contadores de color en el panel; pasos del registro que salen bien en verde; «Subir» como acción principal; «Conectado» junto a la ruta; ícono del protocolo de cada sitio, que avisa cuando FTP puede ir o va sin cifrar; bóveda abierta en verde
- [x] Marca en pantallas clave: inicio de sesión, primer arranque e invitaciones junto a un panel de marca; ilustraciones en los estados vacíos; el símbolo se dibuja mientras se abre una conexión; íconos propios en los encabezados de sitios, registro y transferencias
- [x] Etiqueta de color por sitio (ocho colores): barra en la lista de sitios, borde de sus pestañas y línea sobre la ruta del explorador; el importador de FileZilla lee su `Colour` y la exportación cifrada la conserva
- [ ] Líneas base visuales de Playwright regeneradas y E2E en verde con el color nuevo

## Fase 5: Escala e integraciones

- [ ] 5.1 Catálogo de conectores ("Transferly Store", ADR-0019). Avance: paquete `internal/core/connectors` con descriptores, registro y validación por esquema, SFTP y FTP descritos, y `GET /api/v1/connectors` con su cliente en el frontend; falta la habilitación por instalación y workspace, el formulario generado desde el esquema, la pantalla de la tienda y conectar el catálogo con el flujo de conexión. Alcance: conectores incluidos en el binario que se registran con un descriptor (tipo, nombre, ícono, madurez, capacidades, esquema de configuración y autenticación); formulario de sitio y validación de la API generados desde el esquema; pantalla de administración para habilitarlos por instalación o workspace; SFTP y FTP migrados al catálogo; secretos en el vault; suite de conformidad común
- [ ] 5.2 Conector S3 y compatibles (MinIO, R2, Wasabi), con MinIO en el stack de pruebas; amplía las capacidades (prefijos en vez de carpetas, sin permisos, sin escritura parcial)
- [ ] 5.3 Conector WebDAV
- [ ] 5.4 Conector Google Drive con OAuth en el navegador y renovación de tokens
- [ ] 5.5 Conectores SMB y Azure Blob, según se necesiten
- [ ] SSO con OIDC
- [ ] Tokens de API y documentación pública
- [ ] Multi nodo: sesiones pegajosas, eventos por `LISTEN/NOTIFY` o NATS, worker separado
- [ ] Transferencias programadas con cron consciente de zonas horarias, webhooks
- [ ] Dashboards de Grafana, trazas OpenTelemetry
- [ ] Pruebas de carga (k6) y de caos continuas
- [ ] Revisión de seguridad de la fase: validación de tokens OIDC, alcance y revocación de tokens de API, SSRF por webhooks, aislamiento entre nodos, secretos de los adaptadores en la nube

## Fase 6 (opcional): Transferly Desktop

Ver [ADR-0001](adr/0001-web-first-desktop-ready.md).

- [ ] Store SQLite que pasa `storetest`; `JobQueue` y `EventBus` en memoria
- [ ] Adaptador `localfs`
- [ ] `transport/wails` y `WailsTransport`
- [ ] `cmd/transferly-desktop`: usuario único, vault conservado, configuración en el directorio del usuario del SO
- [ ] Builds multiplataforma en contenedores; firma en macOS en un runner macOS
- [ ] Instaladores, firma de código y auto actualización
- [ ] Revisión de seguridad de la fase: superficie de los bindings de Wails, actualizaciones firmadas, almacenamiento local de la base y del vault

## Pendientes al final: diferenciadores (anotados el 2026-09-26)

Tres funciones que distinguen a Transferly de un cliente FTP clásico. Se hacen después de las fases 3 a 5, porque se apoyan en ellas (permisos por rol, auditoría, tokens de API) y no deben adelantarse.

### D1. Auditoría de acceso a los sitios

Un admin puede ver qué hizo cada usuario en cada servidor: quién se conectó a qué sitio y cuándo, quién descargó, subió, borró o renombró y qué archivo. Es distinta de la auditoría de administración de la fase 3 (miembros, claves, sitios).

- [ ] Registrar por workspace los eventos de acceso: conexión abierta y cerrada, listado, descarga, subida, borrado, renombrado, suma de comprobación, ZIP y copia entre sitios, con usuario, sitio, ruta, tamaño, resultado y dirección IP. Parte del registro de actividad que ya existe (hoy privado por usuario)
- [ ] Vista para admins y propietario con filtros por usuario, sitio, acción y fechas; exportación CSV
- [ ] Retención configurable y purga; límite de volumen (las listados son ruidosos: agrupar o muestrear)
- [ ] Decisión de privacidad: qué ve cada miembro de su propia actividad frente a lo que ve el admin, y aviso visible a los miembros de que el equipo audita el acceso
- [ ] Sin contenidos ni credenciales en el registro; solo metadatos

### D2. Vista previa de archivos sin descargarlos

Adelantado al [bloque D2](#bloque-d2-visualizador-de-archivos-pedido-del-2026-09-29-antes-de-la-fase-5), antes de la fase 5.

Abrir el archivo desde el explorador y verlo en Transferly, sin bajarlo al equipo del usuario.

- [ ] Imágenes, texto y código con resaltado, Markdown, JSON, CSV y TSV como tabla, PDF, audio y vídeo con reproducción por rangos (`Range`), ZIP/TAR/GZ como árbol navegable y con vista previa de sus archivos, hojas de cálculo y documentos de oficina en modo lectura
- [ ] Lectura por rangos y en flujo desde el servidor remoto: nunca traer el archivo entero para mostrar el principio; límite de tamaño por tipo
- [ ] Seguridad: renderizar contenido remoto es la superficie más peligrosa del producto; visores en `iframe` con `sandbox` sin permisos, origen distinto y CSP propia, sin ejecutar scripts de PDF ni HTML ni SVG del servidor remoto; límites contra bombas de descompresión y archivos malformados
- [ ] Respeta permisos: previsualizar equivale a descargar (un viewer puede) y se audita como tal (D1)
- [ ] Abrir con un atajo (barra espaciadora) y navegar entre archivos de la carpeta desde el visor

### D3. Conexiones y archivos por API

Usar Transferly desde cualquier parte y con cualquier herramienta: con tu usuario y los mismos permisos, abrir conexiones a los sitios del workspace y operar sobre ellos por API.

- [ ] Tokens de API personales con alcance (workspace, sitios, lectura o escritura), caducidad y revocación; heredan el rol del usuario y nunca lo superan; se listan y se auditan (fase 5: tokens de API)
- [ ] Contrato público documentado (OpenAPI ya existe) con ejemplos y una CLI de referencia
- [ ] Operaciones con token: listar, descargar, subir con reanudación, copiar entre sitios, transferencias en cola y consulta de su estado
- [ ] Los secretos de los sitios siguen sin salir nunca por la API; conectar con un token necesita el vault del usuario desbloqueado o una clave de servicio del workspace, a decidir en un ADR (hoy las claves viven solo en la sesión desbloqueada)
- [ ] Límites por token y por usuario, detección de abuso y todo el acceso auditado (D1)
- [ ] Revisión de seguridad: alcance y revocación de tokens, fuga de tokens en registros, CSRF no aplicable a tokens, SSRF por sitios

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

- Registro solo por invitación (decidido en ADR-0006).
- Contraseña maestra opcional (recomendado: obligatoria en v1).
