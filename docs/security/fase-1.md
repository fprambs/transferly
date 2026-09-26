# Revisión de seguridad: fase 1 (cerrada)

Fecha: 2026-09-25. Proceso: [README](README.md).

Alcance: cuentas y sesiones (setup, login, CSRF, límites de intentos), vault (contraseña maestra, llaves por sesión, kit de recuperación), workspaces y permisos, `netguard`, sitios y colecciones con secretos sellados, host keys de confianza (TOFU), adaptador SFTP, gestor de conexiones, conexiones rápidas en memoria, API de archivos (listar, crear, renombrar, borrar, descargar, subir y reanudar), transferencias registradas, flujo de eventos por WebSocket y el frontend completo (setup, login, vault, explorador, transferencias, ajustes).

## Modelo de amenazas (STRIDE)

Activos: contraseñas de las cuentas, contraseña maestra y llaves del vault, credenciales de los sitios (contraseñas, claves privadas, frases), sesiones y tokens CSRF, el contenido de los servidores remotos que Sentinel toca y la red interna desde la que corre el servidor. Actores: un visitante anónimo, un usuario autenticado hostil (contra otros usuarios y contra la red interna), una página web maliciosa que el usuario visita mientras tiene sesión, un servidor remoto hostil (nombres y contenido) y un atacante en el camino contra la primera conexión.

Límites de confianza: navegador contra servidor; servidor contra Postgres; servidor contra los servidores remotos (no confiables); usuario contra usuario dentro de la misma instancia; interfaz contra nombres y errores que llegan del servidor remoto.

| Amenaza | Situación en la fase 1 |
|---|---|
| Suplantación | Sesiones opacas con id nuevo en cada login; cookie `HttpOnly`, `SameSite=Lax` y `Secure` con base URL https; login sin distinguir cuenta inexistente de contraseña errónea (trabajo constante con un hash de reemplazo); host keys de los servidores verificadas por callback obligatorio; IP del cliente solo desde proxies de confianza. |
| Manipulación | Token CSRF ligado a la sesión en todo cambio; guarda de `Origin` también en `/setup` y `/auth/login`; secretos sellados con AES-256-GCM con datos asociados (sitio, tipo, versión de llave); una clave de host que cambia bloquea la conexión. |
| Repudio | Registros estructurados con id de petición; las transferencias quedan registradas con usuario, sitio, ruta y estado. La tabla de auditoría existe; su explotación llega en la fase 3. |
| Fuga de información | La API nunca devuelve secretos (solo el tipo de secreto guardado); los registros no contienen contraseñas, claves ni tokens (test que lo comprueba en el nivel de depuración); las descargas son adjuntos inertes; los errores usan códigos estables sin detalles internos. |
| Denegación de servicio | Límites por IP y por cuenta en setup, login, desbloqueo del vault y conexión rápida; derivación Argon2 con concurrencia acotada; pool de 4 conexiones por usuario y sitio; el WebSocket descarta a los clientes que no leen. |
| Elevación de privilegios | Matriz de autorización rol × acción probada en cada endpoint; filtro por workspace en toda consulta; `netguard` impide alcanzar loopback, redes privadas y metadatos de nube desde el servidor. |

## Ataques conocidos revisados

| Ataque | Aplica | Resultado | Verificación |
|---|---|---|---|
| SSRF y DNS rebinding | sí | `netguard` valida la IP resuelta al conectar (no solo al guardar el host) y bloquea loopback, link-local, privadas, CGNAT, metadatos e IPv4 mapeada en IPv6; `SENTINEL_ALLOWED_NETWORKS` es explícito | `TestPolicy_CheckIP`, `TestPolicy_CheckHost`, `TestPolicy_Dialer`; E2E `does not let sites point to internal destinations` (10 destinos) |
| XSS por nombres remotos y errores | sí | Todo se renderiza como texto; sin `innerHTML`, `dangerouslySetInnerHTML`, `eval` ni `new Function` en el código; CSP `script-src 'self'` sin `unsafe-inline` | E2E `shows hostile names as text and runs nothing`: carpeta y archivo con `<img onerror>` en el nombre, en la tabla, el panel de detalles y el dock |
| CSRF | sí | Token ligado a la sesión en todo cambio; ausencia o token ajeno son 403 `csrf_invalid`; `SameSite=Lax` | `TestCSRFTokensAreBoundToTheirSession`, matrices de autorización, E2E `refuses anonymous, token-less and cross-site calls` |
| Login CSRF | sí | `/auth/login` y `/setup` rechazan un `Origin` ajeno (403 `cross_site_request`) | sondeo contra la imagen y E2E |
| CSWSH | sí | El upgrade exige sesión y `Origin` propio; sin `Origin` (cliente que no es navegador) se acepta, porque no hay cookie ambiente | `TestEvents_Access`, E2E `refuses a WebSocket from a foreign origin` |
| Fijación y robo de sesión | sí | Token aleatorio nuevo por login, guardado solo como hash, caducidad por inactividad y absoluta, logout invalida en servidor | `TestSessionLifecycle`, `TestSessionsExpire`, `TestCookieSecureFlag` |
| Fuerza bruta, credential stuffing, enumeración | sí | Límites por IP y por cuenta con `Retry-After`; respuesta idéntica para cuenta inexistente y contraseña errónea | `TestRateLimits`, E2E `does not tell unknown accounts from wrong passwords` |
| DoS por Argon2 | sí | Concurrencia del KDF acotada y límite de intentos de desbloqueo por cuenta | `TestKDF`, `TestRateLimits` |
| Reutilización de nonce en AES-GCM | sí | Nonce aleatorio de 96 bits por sellado; los nonces no se repiten en muchos sellados | `TestSecrets` (vault) |
| Fuga de secretos | sí | Ninguna respuesta ni registro contiene contraseñas de sitio o de conexión rápida, claves privadas, frases, contraseña maestra, contraseña de la cuenta, token de sesión ni token CSRF; una mutación que registra una contraseña hace fallar el test | `TestSecrets_NeverLeave` |
| IDOR y acceso entre workspaces | sí | Filtro por workspace y `workspace.Can` en cada caso de uso; las transferencias y conexiones rápidas son privadas por usuario | `TestAuthorizationMatrix`, `TestSites_`, `TestCollections_`, `TestKnownHosts_`, `TestRemoteFiles_AuthorizationMatrix`, `TestQuickConnections_AuthorizationMatrix`, `TestTransfers_ListingAndAccess` |
| Terrapin (CVE-2023-48795) | sí | `x/crypto` v0.57.0 incluye la corrección | `govulncheck` |
| MITM por confianza en el primer uso | sí | La huella se muestra antes de confiar, el cambio de clave se bloquea con `host_key_changed` y se restringen los algoritmos de clave de host a los ya confiados | `TestDial_HostKeys`, `TestService_TrustCheckForget`, pruebas del conector, unitarias de la interfaz |
| Algoritmos débiles | sí | La biblioteca rechaza CBC, 3DES y RC4; se descubrió que aún ofrecía `diffie-hellman-group14-sha1` y se fijó una lista de intercambio de claves con SHA-2 y curvas elípticas (hallazgo 2) | `TestDial_RefusesLegacyCryptography` |
| Sniffing de contenido y descargas activas | sí | Toda descarga es `attachment` con `application/octet-stream` y `nosniff`; los nombres con comillas, punto y coma, HTML, CR/LF o Unicode no inyectan cabeceras | `TestRemoteFiles_Download`, E2E `serves downloads as inert attachments whatever their name and content` |
| Inyección de comandos por CRLF y NUL en rutas | sí (rutas de SFTP; el FTP llega en la fase 2) | Las rutas con caracteres de control, vacías o de más de 4096 caracteres son 422 `invalid_path` | `TestRemoteFiles_Lifecycle`, E2E `refuses hostile paths and upload parameters` |
| Path traversal | parcial | Las rutas se normalizan léxicamente y `..` nunca sale de la raíz; lo que un usuario alcanza en el servidor remoto lo decide ese servidor con sus credenciales. Zip Slip y sincronización llegan en la fase 2 | pruebas de rutas, sondeo de `/upload/../..` |
| Parámetros de subida absurdos | sí | Tamaño o desplazamiento negativos, modo desconocido e identificador que no es UUID se rechazan (hallazgo 1) | `TestRemoteFiles_Lifecycle`, E2E |
| Inyección SQL | sí | Solo consultas parametrizadas de sqlc; la migración y los tests de la fase 0 siguen vigentes | linters, revisión |
| Suplantación de IP por `X-Forwarded-For` | sí | Sin proxies de confianza la cabecera se ignora | `TestRateLimits` (variante con cabeceras falsas) |
| Cabeceras de seguridad y clickjacking | sí | CSP estricta con `frame-ancestors 'none'`, `X-Frame-Options`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`, COOP y CORP; API con `no-store` | E2E `sends security headers and keeps the API out of caches` |
| Cadena de suministro | sí | Ver Dependencias | CI |
| Escape o abuso del contenedor | sí | Imagen sin root, sistema de archivos de solo lectura, `cap_drop: ALL`; Trivy sin hallazgos | Trivy `image` y `config` |
| FTP bounce, PASV, ZIP bomb, symlinks en sincronización | no todavía | Se revisan al cerrar la fase 2 | |

## Dependencias

| Herramienta | Objetivo | Resultado |
|---|---|---|
| `govulncheck ./...` | módulo Go | 0 vulnerabilidades alcanzables; avisa de GO-2026-5932 (`x/crypto/openpgp`, sin corrección) en un módulo requerido, sin código que la llame |
| `pnpm audit` | frontend, con dependencias de desarrollo | sin vulnerabilidades conocidas |
| Trivy `image` | `sentinel:test` (Debian 12.15 y binario Go) | 0 altas o críticas |
| Trivy `config` | Dockerfiles | 0, con la excepción documentada de vsftpd (`.trivyignore.yaml`) |

Librerías clave: `x/crypto` v0.57.0 (Terrapin corregido), `pkg/sftp`, `coder/websocket` v1.8.15 (comprobación de `Origin` propia más la de la biblioteca), chi sin `RealIP`, pgx v5, React 19, TanStack Router y Query, react-hook-form y zod.

## Pruebas ofensivas realizadas

Contra la imagen de producción en el stack de pruebas aislado (su propio SFTP y su propia base de datos; nunca contra una instancia de desarrollo): cabeceras de seguridad en tres rutas; accesos sin sesión; cambios sin token, con token ajeno y con `Origin` ajeno; upgrade de WebSocket con y sin `Origin`; login con `Origin` ajeno; 11 destinos internos o ambiguos al crear un sitio (127.0.0.1, localhost, 169.254.169.254, 0.0.0.0, `[::1]`, `::ffff:127.0.0.1`, decimal y hexadecimal, redes privadas, dominio de metadatos); rutas con CR/LF, NUL, 5000 caracteres, vacía y con `..`; parámetros de subida negativos, modo inválido e identificador con `../`; archivos con comillas, HTML, Unicode y contenido `<script>`; el mismo intento con cuenta inexistente y contraseña errónea. Los que dejaron una garantía útil se convirtieron en regresiones automáticas (`e2e/specs/security.spec.ts`, `secrets_test.go`, `sftpfs_test.go`, `remotefiles_test.go`).

## Hallazgos

| # | Severidad | Hallazgo | Estado |
|---|---|---|---|
| 1 | Baja | El parámetro `size` de la subida aceptaba valores negativos (y `offset` no se comprobaba en el borde HTTP): un tamaño negativo quedaba registrado en la transferencia | Corregido: 422 `invalid_size` y `invalid_offset`; regresión en `TestRemoteFiles_Lifecycle` y en el E2E |
| 2 | Media | El cliente SSH aceptaba `diffie-hellman-group14-sha1` (la biblioteca lo ofrece por defecto), de modo que un servidor que solo hablara intercambios con SHA-1 negociaba una sesión más débil | Corregido: lista explícita de intercambios con SHA-2 y curvas elípticas; un servidor que solo ofrezca SHA-1 se rechaza. Regresión `TestDial_RefusesLegacyCryptography` (5 casos y uno moderno que sí conecta) y E2E contra OpenSSH real |
| 3 | Informativa | Al pedir `offset` sin `resume`, o reanudar más allá del tamaño del archivo, la API responde 422 con el código `invalid_path`, poco descriptivo | Aceptado: se rechaza correctamente; el código estable ya lo usa el cliente |
| 4 | Informativa | El WebSocket acepta un upgrade sin cabecera `Origin` | Aceptado: solo lo envían clientes que no son navegadores, que no llevan cookie ambiente; documentado en la guía |
| 5 | Baja | Un usuario autenticado puede usar el servidor para sondear puertos de hosts públicos (conexión rápida o prueba de un sitio) | Aceptado: es inherente a un cliente SFTP; `netguard` impide alcanzar lo interno y la conexión rápida tiene límite por usuario |
| 6 | Baja | Un usuario autenticado puede lanzar subidas concurrentes sin tope global | Aceptado hasta la fase 2: el pool limita las conexiones (4 por sitio) y el planificador con límites por sitio y por usuario está previsto en esa fase |
| 7 | Informativa | La configuración de Playwright apuntaba por defecto a `localhost:8080`, es decir, a una instancia de desarrollo con datos reales si se lanzaba fuera del stack de pruebas | Corregido: la configuración se niega a arrancar sin `SENTINEL_E2E_URL` y `make e2e` recrea el stack aislado en cada ejecución |
| 8 | Informativa | Con base URL `http` las cookies no llevan `Secure` | Aceptado: es la señal para saber que no hay TLS; con `https://` se marcan. Cubierto por `TestCookieSecureFlag` |
| 9 | Informativa | `govulncheck` avisa de `x/crypto/openpgp` sin corrección | Aceptado: paquete que Sentinel no importa ni llama |

## Riesgos aceptados

Los hallazgos 3 a 6, 8 y 9. Además: la seguridad del servidor remoto y de lo que un usuario legítimo hace con sus propias credenciales queda fuera de Sentinel; un administrador de la instancia con acceso al proceso puede leer las llaves en memoria de las sesiones abiertas (el vault protege los datos en reposo y entre usuarios, no frente a quien controla el servidor); la confianza en el primer uso (TOFU) depende de que el usuario compare la huella cuando el servidor es nuevo.

## Conclusión

No quedan hallazgos altos ni críticos abiertos. Dos correcciones (subida con tamaño negativo y SHA-1 en el intercambio de claves) nacieron de las pruebas ofensivas y tienen regresión automática. La fase 1 queda cerrada desde el punto de vista de seguridad; la fase 2 debe revisar además FTP (bounce, PASV, inyección por CRLF), Zip Slip, ZIP bombs y symlinks en sincronización.
