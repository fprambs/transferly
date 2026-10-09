# Revisión previa a una versión estable

Fecha: 8 de octubre de 2026. Revisión base: `ba2e4db`. Versión del frontend: `0.9.0`.

Este documento conserva los hallazgos de la revisión base. Las correcciones y sus resultados están en la [validación final del candidato local](2026-10-09-validation-results.md).

Seguimiento: [plan de mitigación por hallazgo, entregas y criterios de aceptación](2026-10-08-mitigation-plan.md).

**Dictamen: todavía no recomiendo declarar esta revisión estable.** Hay defectos reproducidos de revocación de acceso, aislamiento de datos en el cliente y conservación de archivos, además de problemas en las verificaciones de lanzamiento. La arquitectura existente permite corregirlos sin reescribir el proyecto.

La revisión comprende arquitectura, servicios del núcleo, permisos, autenticación, vault, conexiones, operaciones de archivos, vistas previas, estado del cliente, SQL, dependencias, Docker, CI y releases. Incluye lectura dirigida del código y comprobaciones ejecutadas. No equivale a una certificación, a leer exhaustivamente cada línea ni a una medición de carga de producción. Las conclusiones diferencian pruebas ejecutadas de análisis estático.

## Comprobaciones

| Comprobación | Resultado |
| --- | --- |
| `golangci-lint run ./...` | Pasa: 0 observaciones. |
| `go mod tidy -diff` | Pasa, sin diferencias. |
| Suite Go oficial con `-race -count=1` | Todos los paquetes ejecutados pasan; falla la exigencia de cobertura por `azblobfs.copy`: 90,9 %. El total redondeado muestra 100,0 %. |
| `govulncheck -show verbose ./...` | 0 vulnerabilidades en funciones llamadas y paquetes importados; 1 aviso en un módulo requerido, referido a `openpgp`, que el proyecto no importa. |
| `pnpm audit --prod` | Falla: 3 avisos de severidad alta. Ver R05. |
| `pnpm typecheck` y `pnpm lint` | Pasan: 0 errores y 0 advertencias de lint. |
| `pnpm build` | Pasa, con advertencia de tamaño del bundle y módulos Node externalizados de `sql.js`. |
| Imagen Docker de producción | Compilada y levantada en el stack de pruebas; healthcheck pasa. |
| Suite Vitest con cobertura | Pasa con exit 0; LCOV: 7.895/7.895 líneas, 3.848/3.848 funciones y 4.865/4.865 ramas cubiertas (100 %). |
| Suite Playwright | Termina con exit 0: 160 pasadas, 19 omitidas deliberadamente (generación de screenshots) y 1 inestable que pasa al reintentar; 180 casos Chromium, 4 workers, 5,2 minutos. La inestabilidad afecta al indicador de desconexión al cerrar una pestaña. |
| Regeneración de OpenAPI y SQL | Pasa: `scripts/generate.sh` termina correctamente y no cambia archivos rastreados. |
| Reproducciones adicionales | Confirmadas: terminal tras logout y expiración; pérdida del original ante fallo de rename; bloqueo de copia con una conexión; aceptación de alias de rutas; datos anteriores tras expiración/logout; preview obsoleto; recorrido sin límite de directorios. |

La suite Go omite por configuración algunas pruebas contra servidores externos reales cuando no están definidas sus variables `TRANSFERLY_TEST_*`. El stack de navegador sí se levantó con todos los perfiles de FTP, S3, Google, Azure, WebDAV y SSH.

No se ejecutó aquí Trivy de imagen/configuración, un escáner exhaustivo de secretos del historial, fuzzing prolongado ni benchmarks de carga o redes de alta latencia. El examen de archivos rastreados e historial de `.env`, `.env.local` y `.env.production` no encontró esos archivos de entorno versionados. Las claves privadas encontradas en fuentes corresponden a fixtures y muestras de prueba; esto no sustituye un escaneo de todo el historial.

Las reproducciones se conservan en [reproductions/frontend.cjs](reproductions/frontend.cjs), [reproductions/files_test.go.txt](reproductions/files_test.go.txt) y [reproductions/terminal_test.go.txt](reproductions/terminal_test.go.txt). Son pruebas diagnósticas que **afirman el comportamiento defectuoso observado**, no tests de aceptación para un fix. Los archivos Go temporales se retiraron de los paquetes de producción después de ejecutarlos; las copias `.txt` aprovechan las fixtures de tests existentes.

La reproducción del frontend puede repetirse con:

```sh
docker compose -f compose.dev.yaml run --rm --no-deps -T tools node /workspace/docs/reviews/reproductions/frontend.cjs
```

## Prioridades

P1: corregir antes de publicar como estable. P2: corregir o acotar explícitamente antes de garantizar el comportamiento afectado. P3: mejora de mantenibilidad/operación.

| ID | Prioridad | Hallazgo | Evidencia |
| --- | --- | --- | --- |
| R01 | P1 | Terminales conservan acceso después de logout o expiración | Reproducido con router real y WebSocket |
| R02 | P1 | Sobrescritura puede perder el archivo original; staging compartido | Pérdida reproducida; concurrencia por análisis estático |
| R03 | P1 | Cambio de usuario conserva datos y recursos del anterior | Reproducido con funciones reales, QueryClient y Zustand |
| R04 | P1 | Instrucción HTTPS deja un origen HTTP incorrecto | Configuración Compose reproducida |
| R05 | P1 | Auditoría de dependencias falla | Escáner ejecutado y avisos contrastados |
| R06 | P1 | CI de navegador no levanta servidores que sus pruebas necesitan | Análisis de workflow, Compose y pruebas |
| R07 | P2 | Copia por streaming puede bloquearse por falta de conexiones | Reproducido con pool real de una conexión |
| R08 | P2 | Comparaciones de rutas permiten copiar una carpeta dentro de sí misma | Reproducido en `CheckCopy` |
| R09 | P2 | Preview conserva contenido antiguo o de otra carpeta local | Reproducido con QueryClient real |
| R10 | P2 | Recorridos frontend no limitan todos los directorios ni detectan ciclos | Reproducido sin red |
| R11 | P2 | Editor y respuestas remotas tienen lecturas sin límite efectivo | Análisis estático |
| R12 | P2 | Índices y documentos comprimidos se parsean antes de aplicar límites útiles | Análisis estático; no prueba de agotamiento |
| R13 | P2 | Publicación de imágenes no produce una versión estable identificable | Análisis de release, workflow y Dockerfile |
| R14 | P2 | Bundle inicial excesivo | Medición del build |
| R15 | P3 | Errores relevantes desaparecen y dificultan diagnóstico | Análisis de frontend y tracker |
| R16 | P2 | Cierre de pestaña tiene un resultado E2E inestable | Fallo observado, pasa al reintentar; causa pendiente |

### R01. Revocar las terminales de forma efectiva

Referencias: [terminal.go](../../backend/internal/transport/httpapi/terminal.go), líneas 54–94; [connections.go](../../backend/internal/core/connections/connections.go), líneas 655–711; [commands.go](../../backend/internal/core/files/commands.go), líneas 179–194.

El handler autentica al abrir el WebSocket, pero `relayTerminal` no vuelve a comprobar sesión ni permisos. `CloseUser` solo cierra conexiones ociosas; las prestadas permanecen abiertas hasta liberarse. Una terminal mantiene su lease mientras siga funcionando.

**Reproducción:** abrir una terminal con la sesión de la fixture, realizar logout y escribir por el WebSocket existente. La shell devuelve `echo: after logout`. En otra prueba, avanzar el reloj nueve horas, comprobar que `Auth.Verify` rechaza el token y escribir por la terminal: sigue respondiendo. Ambas pruebas pasan con `-race`, confirmando el comportamiento defectuoso.

Corrección: registrar terminales por sesión y cerrarlas inmediatamente al revocar acceso; comprobar periódicamente con `Auth.Verify`, sin renovar la sesión por esa comprobación. Revalidar pertenencia/rol del workspace y la habilitación de comandos. Reutilizar las reglas de expiración del stream de eventos, que ya verifica su sesión. Probar logout, expiración, cuenta deshabilitada y pérdida de permisos sobre una terminal abierta.

### R02. Garantizar conservación del original y separar staging por transferencia

Referencia: [files.go](../../backend/internal/core/files/files.go), líneas 493–569.

`finish` elimina el destino antes de renombrar el `.part`. Si el rename falla, el original ya desapareció. La secuencia tampoco constituye una sustitución atómica aunque el comentario del servicio y el editor la describan así.

**Reproducción:** crear `/important.txt` con contenido original, hacer un upload overwrite y usar un adaptador cuya operación Rename devuelve `ErrPermission`. Upload falla y Stat del original devuelve `ErrNotFound`; la pérdida está comprobada.

Además, todas las subidas a un mismo destino usan exactamente `<path>.part` y las nuevas lo abren en modo overwrite. No hay exclusión por destino ni identidad del temporal por transferencia. Dos usuarios o dos peticiones concurrentes pueden operar sobre el mismo staging. Esta segunda conclusión surge del código; no se ejecutó una prueba destructiva sobre servidores reales.

Corrección: introducir una operación de reemplazo con garantías explícitas por adaptador —por ejemplo, rename atómico cuando el protocolo/servidor lo admite— y una estrategia recuperable cuando no lo admite. Temporales identificados por transferencia, propiedad verificada para reanudar y exclusión por destino. No borrar el original hasta contar con una sustitución segura; si no se puede garantizar atomicidad, documentar esa limitación. Incluir carreras entre create/overwrite/resume, fallos antes/durante el commit y desconexiones.

### R03. Unificar el ciclo de vida del estado privado del cliente

Referencias: [session-expired.ts](../../frontend/src/app/session-expired.ts), líneas 15–21; [UserMenu.tsx](../../frontend/src/features/topbar/UserMenu.tsx), líneas 45–50; [AuthForm.tsx](../../frontend/src/features/auth/AuthForm.tsx), función `start`; [transfers-store.ts](../../frontend/src/features/transfers/transfers-store.ts); [local-root-store.ts](../../frontend/src/features/local/local-root-store.ts); [upload-sources.ts](../../frontend/src/features/transfers/upload-sources.ts).

Al expirar, `endSession` solo cambia `['session']` y navega. El siguiente login no vacía el resto de QueryClient. Sus claves privadas no están separadas por identidad de usuario: `['workspaces']`, `['preview', ...]`, etc. El usuario siguiente puede ver datos del anterior mientras se refrescan; las consultas con `staleTime: Infinity` pueden conservarlos más tiempo.

El logout explícito sí hace `queryClient.clear()`, pero eso no limpia Zustand ni los mapas de archivos de subida ni el handle de la carpeta local. `TransfersDock` presenta directamente el store global. `reset` de transferencias y `clearUploadSources` existen, pero no se invocan desde el cierre de sesión en producción.

**Reproducción:** sembrar caché y store con datos A, invocar `endSession` y registrar sesión B con la misma secuencia de escrituras de AuthForm. El workspace A y la transferencia A siguen presentes. Hacer `queryClient.clear()` tampoco borra la transferencia.

Corrección: un único coordinador para logout, expiración y reemplazo de usuario: cancelar peticiones/subscripciones, limpiar caché privada, transferencias, tabs, prompts, logs, fuentes de subida y handles locales; conservar solo preferencias públicas deliberadas. Añadir una generación de sesión o identidad de usuario a claves privadas para impedir que respuestas tardías del usuario A repueblen estado de B. Prueba real A → logout/expiración → B sin recargar la página.

### R04. Hacer funcionar el HTTPS documentado

Referencias: [README.md](../../README.md), línea 81; [compose.yaml](../../compose.yaml), líneas 18–19; [config.go](../../backend/internal/config/config.go), `CookieSecure` y `AllowedOrigins`; [session.go](../../backend/internal/transport/httpapi/session.go), `originGuard`.

El comando documentado solo configura `TRANSFERLY_DOMAIN`. Al resolver Compose con `review.example.com`, Caddy recibe ese dominio pero el backend sigue con `TRANSFERLY_BASE_URL=http://localhost:8080`. Consecuencias por el código: los POST del navegador desde el dominio HTTPS son rechazados por el origen, la cookie no se marca Secure y WebAuthn/OAuth usan una base incorrecta.

Corrección inmediata de documentación: configurar también `TRANSFERLY_BASE_URL=https://transferly.tuempresa.com`. Después, vincular los valores o validar su coherencia para evitar instalaciones aparentemente saludables pero inutilizables. Configurar confianza limitada en el proxy Caddy si se necesita identificar IPs reales: hoy `TRUSTED_PROXIES` vacío concentra los límites por IP en la IP del proxy. No habilitar confianza global en cualquier proxy.

### R05. Resolver el árbol de dependencias y separar herramientas de runtime

Referencia: [package.json](../../frontend/package.json) y [pnpm-lock.yaml](../../frontend/pnpm-lock.yaml).

El audit ejecutado el 8 de octubre arroja tres avisos altos:

| Paquete | Ruta local | Aviso |
| --- | --- | --- |
| `braces` | `shadcn` → `fast-glob`/`ts-morph` → `micromatch` | [GHSA-vfj7-8cjw-p6xm](https://github.com/advisories/GHSA-vfj7-8cjw-p6xm); sin versión corregida indicada al revisar |
| `source-map-js` | `shadcn` → `postcss`; `@tailwindcss/vite` → `vite`/`@tailwindcss/node` | [GHSA-68fv-2mgg-jv7q](https://github.com/advisories/GHSA-68fv-2mgg-jv7q); corregido desde 1.2.2 |
| `@modelcontextprotocol/sdk` | `shadcn` → SDK | [GHSA-6qxp-vccf-f47h](https://github.com/advisories/GHSA-6qxp-vccf-f47h); corregido desde 1.31.0 |

Los avisos se contrastaron con GitHub Advisory Database. No se demostró explotación desde el navegador ni presencia de esas herramientas en la imagen final, que distribuye assets y binario Go. Sí se comprobó que el gate actual de seguridad falla.

`shadcn`, `@tailwindcss/vite` y `tailwindcss` pertenecen al proceso de desarrollo/build. Revisar su ubicación en devDependencies, retirar herramientas no usadas y actualizar los paquetes corregidos. Cambiar de sección no resuelve por sí solo una vulnerabilidad: mantener auditoría de herramientas de build y cualquier excepción precisa, documentada y revisable, especialmente para `braces` sin parche.

Go no presenta vulnerabilidades alcanzables detectadas. El aviso [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) pertenece a `golang.org/x/crypto/openpgp`, que no se importa. No atribuirlo al uso de SSH/Argon2 de este proyecto.

### R06. Alinear CI con la suite de protocolos

Referencias: [ci.yml](../../.github/workflows/ci.yml), línea 63; [compose.test.yaml](../../compose.test.yaml), servicios/perfiles y dependencias de e2e; [Makefile](../../Makefile), `TEST_STACK`.

CI ejecuta únicamente `docker compose -f compose.test.yaml run --rm e2e`. Sus dependencias levantan `transferly`, PostgreSQL y SFTP. Las pruebas de FTP, S3, Google, Azure, WebDAV y herramientas SSH se ejecutan igualmente y referencian servicios que quedan desactivados por sus perfiles. Es un defecto de configuración; no se ejecutó el workflow remoto de GitHub en esta revisión.

Corrección: hacer que CI invoque el mismo inicio completo que `make e2e`, o un script compartido, antes de Playwright. La cobertura de Go ya presenta un fallo independiente en `azblobfs.copy`; resolverlo con una prueba significativa del caso no cubierto, sin rebajar el gate para ocultar la diferencia. Antes de publicar una imagen estable, exigir un CI verde del commit exacto.

### R07. Evitar espera circular en copias

Referencias: [copy.go](../../backend/internal/core/files/copy.go), `copyFile`; [files.go](../../backend/internal/core/files/files.go), `Download` y `Upload`; [connections.go](../../backend/internal/core/connections/connections.go), `Acquire`; [transfers.go](../../backend/internal/transport/httpapi/transfers.go), `CreateCopy`.

Una copia que no usa comando remoto mantiene la conexión de lectura hasta finalizar y adquiere otra para escribir. Si el sitio permite una sola conexión, una descarga por streaming mantiene la única y Upload espera que se libere; la liberación espera terminar Upload. El caso de archivos pequeños puede quedar oculto porque `buffered` libera la conexión antes de responder.

**Reproducción:** pool real con `MaxPerSite: 1`, buffering desactivado para forzar streaming, copia `/src.txt` → `/dst.txt` en el mismo sitio y contexto de 200 ms. Termina por `DeadlineExceeded`, no por completar la copia. Para una copia HTTP de fondo, `WithoutCancel` hace que cerrar el navegador no cancele esa espera.

Corrección: planificar/reservar recursos de ambos extremos con un orden común, o rechazar antes de iniciar una copia por streaming que necesita más conexiones de las permitidas. Ofrecer copia remota cuando exista o staging temporal acotado si se desea soportar una conexión. Probar también copias concurrentes y A → B / B → A.

### R08. Normalizar rutas antes de aplicar invariantes

Referencia: [copy.go](../../backend/internal/core/files/copy.go), `sameFile`, `intoItself`, `CheckCopy`.

Las comparaciones usan strings originales. `SourcePath=/src/../src` y `DestinationPath=/src/child` pasan `CheckCopy` pese a que la carpeta destino está dentro del origen. Los adaptadores y `path.Join` normalizan después; el recorrido puede empezar a copiar su salida hasta agotar los límites. La comparación de igualdad también admite alias equivalentes.

**Reproducción:** `CheckCopy` devuelve nil para ese par con el mismo sitio. No se realizó una expansión destructiva del árbol.

Corrección: centralizar rutas absolutas normalizadas antes de comparar y antes de operar, y comprobar identidad de ambos extremos. Las rutas que atraviesan symlinks necesitan un tratamiento adicional según capacidades del servidor; `path.Clean` por sí solo no resuelve esa identidad física.

### R09. Versionar la identidad de previews

Referencias: [source.ts](../../frontend/src/features/preview/source.ts), `remoteSource`, `localSource`; [WholeFile.tsx](../../frontend/src/features/preview/viewers/WholeFile.tsx); [PreviewBody.tsx](../../frontend/src/features/preview/PreviewBody.tsx); [Explorer.tsx](../../frontend/src/features/explorer/Explorer.tsx), `reload` y `onSaved`.

La clave remota contiene workspace, sitio y ruta, pero no tamaño/fecha/versión. Las consultas de preview mantienen `staleTime: Infinity`; guardar o subir un archivo solo invalida listings. Reabrir la ruta puede mostrar sus bytes anteriores. La clave local es solo `local:<path>`: dos carpetas locales diferentes con el mismo nombre de archivo comparten identidad incluso después de cambiar el root.

**Reproducción:** dos fuentes remotas con tamaño y mtime distintos producen la misma clave; QueryClient devuelve `old` para la segunda consulta sin ejecutar su lector. Dos fuentes locales de raíces diferentes producen la misma clave.

Corrección: clave de preview común con identidad de sesión, root local/version y versión de archivo; invalidación explícita después de upload/save/rename/delete. Limitar el tiempo y memoria retenidos por vistas grandes. Reutilizar la misma política en todas las vistas, incluyendo hex y archivos anidados.

### R10. Un recorrido acotado y cancelable para todos los usos

Referencias: [download.ts](../../frontend/src/features/explorer/download.ts), `collectFiles`; [collect.ts](../../frontend/src/features/sync/collect.ts); [walk.ts](../../frontend/src/features/search/walk.ts); [tree.ts](../../frontend/src/features/local/tree.ts).

`collectFiles` limita archivos, no directorios visitados, profundidad, duración ni rutas repetidas. Comparación/sincronización reutilizan esa función. No recibe AbortSignal. Un árbol muy grande de carpetas vacías puede seguir indefinidamente sin alcanzar el límite de archivos.

**Reproducción:** con `limit=1`, visita 2.000 directorios sin error y encuentra cero archivos. Un listado que devuelve reiteradamente el mismo directorio vuelve a visitarlo veinte veces; la reproducción se detiene externamente para evitar un bucle infinito.

Corrección: extraer un walker compartido con contador de todas las entradas, profundidad, conjunto de visitados, señal de cancelación propagada al listado, validación de nombres y límite temporal. Usar índice de cola en vez de `shift()` para árboles amplios. Aplicar límites durante la lectura de batches: `tree.readAll` acumula una carpeta completa antes de verificar `maxTreeEntries`, y `treeFromInput` no aplica ese límite.

### R11. Acotar los bytes reales leídos

Referencias: [RemoteEditor.tsx](../../frontend/src/features/editor/RemoteEditor.tsx), líneas 43–44 y 60–77; [azure.go](../../backend/internal/adapters/remotefs/azure/azure.go), líneas 189–197; [connect/azure.go](../../backend/internal/adapters/remotefs/connect/azure.go); [azblobfs.go](../../backend/internal/adapters/remotefs/azblobfs/azblobfs.go), `list`.

El editor confía en `entry.size` del listado para su límite de 2 MiB, luego convierte todo el stream en texto. Un archivo que creció o un servidor que informa tamaños incorrectos evade ese límite. El `stat` nuevo no impide leerlo: se ejecuta en paralelo y se usa como baseline. La cancelación del effect solo descarta el resultado, no aborta la transferencia.

Azure usa `io.ReadAll(resp.Body)` ante errores sin código de cabecera, sin tope de bytes. Los timeouts de conexión/cabeceras no acotan un body que sigue llegando; los requests ordinarios tampoco tienen aquí una fecha límite global. Los decoders XML y las páginas de listings necesitan límites de bytes y de entradas efectivos frente a endpoints personalizados: pedir `maxresults` no obliga a un servidor hostil a respetarlo.

Corrección: reutilizar un lector acotado —en frontend ya existe `readCapped`— con AbortSignal y comprobar longitud real; para errores remotos, limitar cuerpo y devolver un error estable si se excede. Añadir presupuestos de operaciones cortas sin imponer un timeout global a transferencias legítimas largas. Limitar acumulación/páginas repetidas de listings.

### R12. Limitar parsing y descompresión antes de materializar

Referencias: [indexed.go](../../backend/internal/core/archives/indexed.go), `listZip`, `listSevenZip`, `openZip`; [local-archive.ts](../../frontend/src/features/preview/local-archive.ts), `readZip`; [office/document.ts](../../frontend/src/sandbox/office/document.ts), `renderOdt` y `renderDocument`.

El límite de 10.000 entradas del listado ZIP se aplica después de `zip.NewReader`, que ya construye su índice completo. El frontend lee `directorySize` de un ZIP local y materializa el rango antes de limitar la cantidad mostrada. Las vistas Office usan JSZip para descomprimir XML/imágenes y docx-preview; el límite al archivo comprimido no limita los bytes descomprimidos.

Se trata de una debilidad inferida del orden de procesamiento, no de una prueba de agotamiento realizada. El sandbox protege origen/red, pero no constituye una cuota de CPU/memoria.

Corrección: revisar tamaño y número de entradas del índice antes de materializarlo cuando el formato lo permita, acotar partes y suma descomprimida y presupuestos de decodificación. Parsers cancelables o workers terminables, teniendo en cuenta las restricciones CSP/origen opaco. Añadir archivos adversarios pequeños que declaran índices/partes desproporcionados; no hace falta provocar un OOM para comprobar que se rechazan.

### R13. Publicar imágenes de release identificables e inmutables

Referencias: [ci.yml](../../.github/workflows/ci.yml), líneas 104–129; [app.Dockerfile](../../docker/app.Dockerfile), `ARG VERSION=dev`; [release.sh](../../scripts/release.sh); [releases.md](../guides/releases.md).

El workflow se activa en main y PR; la publicación etiqueta SHA y `latest`, pero no pasa `VERSION`. Por tanto las imágenes publicadas mediante ese job reportan `dev`, aunque el commit sea de release. El tag `vX.Y.Z` no dispara un flujo que publique `vX.Y.Z`. La guía indica que no se publica nada fuera de git, pero CI ya publica GHCR.

Corrección: un flujo de tags que pruebe el commit y publique la imagen con versión real, etiqueta SemVer inmutable y digest; separar la etiqueta de main de la estable. Alinear README/Compose con el mecanismo elegido y documentar actualización, backup y rollback de migraciones. Validar el endpoint `/api/v1/version` contra el tag antes de release.

### R14. Reducir el código que se carga al inicio

Referencias: [router.tsx](../../frontend/src/app/router.tsx), imports de pantallas; [Explorer.tsx](../../frontend/src/features/explorer/Explorer.tsx); [RemoteEditor.tsx](../../frontend/src/features/editor/RemoteEditor.tsx); [registry.ts](../../frontend/src/features/preview/viewers/registry.ts).

Build medido: `main` = **2.385,05 kB minificado / 722,73 kB gzip**, CSS principal 120,86 kB / 19,76 kB gzip. PDF y SQLite están en chunks grandes separados; eso no implica que todo el tamaño del dist se descargue al abrir la app. Vite advierte por chunks de más de 500 kB.

Router, editor, terminal y pantallas secundarias llegan por imports estáticos del árbol de la aplicación. Corrección: cargar bajo demanda editor/terminal/configuración y visores costosos, conservar skeletons y mensajes de error, medir requests iniciales y tiempo hasta interacción. Definir un presupuesto de bundle en CI. No ocultar la advertencia aumentando su umbral como única solución.

### R15. Dar visibilidad a fallos significativos

Referencias: [TransfersDock.tsx](../../frontend/src/features/transfers/TransfersDock.tsx), `cancel`, `pause`, `proceed`, `remove`; [tracker.go](../../backend/internal/core/transfer/tracker.go), `save`; [remotefiles.go](../../backend/internal/transport/httpapi/remotefiles.go), `audit`; [RemoteEditor.tsx](../../frontend/src/features/editor/RemoteEditor.tsx), `reload`.

Varias acciones convierten cualquier fallo en `undefined`. Un usuario puede pulsar cancelar y no recibir explicación si el servidor falla. `Tracker.save` descarta fallos de persistencia y posteriormente publica estados como si se hubiesen guardado. `reload` del editor se invoca con `void` sin manejar rechazo. Estas decisiones ocultan fallos operativos o generan promesas rechazadas sin una respuesta de UI.

Corrección: una política compartida que distinga cancelación esperada, error visible de usuario y fallo operativo. Reutilizar traducciones/códigos del API; registrar fallos de persistencia y definir reconciliación. Los logs pueden seguir siendo best effort, pero sus pérdidas deben poder diagnosticarse sin registrar secretos.

### R16. Investigar la desconexión inestable al cerrar pestañas

Referencias: [tabs.spec.ts](../../frontend/e2e/specs/tabs.spec.ts), línea 59; [use-close-unused.ts](../../frontend/src/features/tabs/use-close-unused.ts); [use-open-connections.ts](../../frontend/src/features/sites/use-open-connections.ts).

En la suite completa, `opens, switches, remembers and closes tabs` falla porque el sitio de la pestaña cerrada sigue mostrando `Connected` durante los cinco segundos de espera. El retry pasa. No se determinó si se trata de una carrera de reapertura, estado del evento/caché o sincronización del test; no atribuir una causa definitiva sin la traza.

Comprobación posterior: se repitió exclusivamente ese caso tres veces, con un worker y sin retries. Las tres pasaron en 21,4 segundos. El fallo observado bajo la suite completa queda registrado como intermitente; la ejecución aislada no permite concluir que haya desaparecido.

El flujo de cierre borra queries y lanza disconnect sin esperar su resultado, mientras otros flujos de desconexión sí cancelan queries antes de desconectar. Revisar ese orden y reconciliar el estado tras el disconnect, además de errores hoy descartados. Repetir este caso de manera aislada y bajo carga; no resolverlo únicamente con un timeout mayor o retries adicionales. El reporte y la traza quedaron en `frontend/playwright-report` y `frontend/test-results` (artefactos ignorados por git).

## Calidad de código y reutilización

La estructura ya es bastante más cuidada que un prototipo: core/ports/adapters/transport, SQL generado, contrato OpenAPI, matriz de permisos, servicios con dependencias inyectadas y clock controlable, frontend por funcionalidades, Transport intercambiable, TypeScript estricto y límites en varias operaciones. El lint verifica parte de esa separación. Mantendría esa base.

Las mejoras con mayor retorno son concretas:

1. **Ciclo de sesión compartido:** concentrar revocación y limpieza; actualmente eventos, terminal, caché, stores y recursos locales tienen comportamientos diferentes.
2. **Contrato de archivos más preciso:** distinguir rename, replace/commit, capacidad de sustitución atómica y staging/reanudación. La presencia de `Resume` no debería ser la única señal para decidir garantías de escritura. Añadir los escenarios de R02/R07 a la conformance suite de adaptadores.
3. **Identidad de recursos:** constructores únicos de claves Query que incluyan sesión, versión del archivo y raíz local, con invalidación centralizada.
4. **Recorridos reutilizables:** un walker por entorno con la misma política de límites y cancelación; evitar mantener variantes parciales en download, sync, search y uploads.
5. **Separación de responsabilidades en React:** `Explorer.tsx` tiene 814 líneas y `SitesSidebar.tsx` 574. Extraer controladores/hooks de operaciones, previews, subidas y terminales, con componentes que solo presenten estado. No fragmentar solo para disminuir el número de líneas: cada extracción debe tener una responsabilidad y una interfaz comprobables.
6. **Límites comunes por tipo de operación:** streaming sin buffering innecesario para transferencias; lecturas acotadas y deadlines para metadatos, errores y previews. Evitar copiar configuraciones HTTP/decoders con garantías distintas sin intención explícita.
7. **Estándares útiles y automatizados:** conservar gofmt/goimports, lint, separación de core, TS estricto y guards del contrato. Adoptar `noUncheckedIndexedAccess` gradualmente y revisar casts en fronteras. Los comentarios deberían explicar invariantes y decisiones: exigir documentación trivial a toda declaración mientras se prohíben comentarios dentro de funciones puede generar ruido y dificultar explicar lógica delicada; valorar una regla menos mecánica.
8. **Pruebas orientadas a garantías:** cobertura de líneas no prueba conservación de datos, revocación ni ausencia de deadlocks. Priorizar pruebas de carreras, fallos parciales, cambios de identidad y comportamiento común entre adaptadores; mantener las verificaciones de cobertura existentes como señal complementaria.

## Publicación pública y operación

Antes de abrir el repositorio, ejecutar un escáner de secretos sobre todo el historial y revisar cualquier resultado sin publicar valores. Añadir `SECURITY.md` con canal privado de reporte y versiones soportadas. La licencia Apache y las licencias de los forks locales existen; documentar la procedencia y mantenimiento de ambos forks, cuya copia local no incluye toda la suite/fuzzing upstream.

Reducir el contexto Docker: `.dockerignore` excluye `.env` pero no `.env.*`, ni `backend/coverage.out`. La compilación de esta revisión transfirió aproximadamente 71 MB de contexto después de generar cobertura. Esto no demuestra que `.env.local` termine en la imagen final, pero es innecesario enviar secretos locales y artefactos al builder. Excluirlos explícitamente.

En producción, definir backup probado de PostgreSQL y `/data`, recuperación de la clave de instancia, procedimiento de migración/rollback y retención del historial de transferencias/audit. La actividad sí tiene limpieza; los registros de transferencias solo cuentan con borrado explícito por usuario. No sostener una promesa de múltiples réplicas sin diseñar el estado en memoria de pools, vault, scheduler y eventos para ese modo.

## Orden recomendado de trabajo

1. Resolver R01–R03 con pruebas de regresión de seguridad/integridad. Incluir acceso revocado durante operaciones abiertas y escrituras concurrentes.
2. Resolver R04–R06 y la cobertura de Azure; recuperar CI verde del commit candidato y comprobar una instalación HTTPS limpia siguiendo solo README.
3. Resolver R07–R12 para los protocolos y features ofrecidos como estables. Establecer garantías y límites documentados cuando un protocolo no permita la misma semántica.
4. Resolver versión/publicación y reducir carga inicial (R13–R14); aplicar las extracciones reutilizables en cambios pequeños con validación específica.
5. Cerrar R15 y las tareas de operación; probar backup/restauración, actualización desde una versión anterior y release desde cero.

No convertir el candidato actual a `1.0.0` solo porque la cobertura redondeada sea 100 %. El criterio de salida debe ser: sin defectos P1 abiertos, CI real verde, garantías de archivos/sesión comprobadas, instalación documentada funcional y una imagen versionada que corresponda al commit probado.
