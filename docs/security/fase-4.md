# Revisión de seguridad: fase 4 (cerrada)

Fecha: 2026-09-29. Proceso: [README](README.md). Continúa el [informe de la fase 3](fase-3.md).

Alcance: comparación y sincronización de carpetas (4.1), búsqueda remota recursiva (4.2), subidas atómicas (4.3), salto SSH, proxy SOCKS5/HTTP y keyboard-interactive (4.4), editor remoto (4.5), el bloque de experiencia tipo FileZilla (4.6 a 4.14: formulario de sitio, modos de acceso preguntar, anónimo, cuenta e interactivo, cifrado de FTP, claves privadas como archivo, registro de la conexión en vivo, pestañas, cambio de clave de host, paleta de comandos, preferencias y marcadores, filtro instantáneo), exportación cifrada de los sitios (4.15), mensajes de error guiados (4.16) y la estructura de idiomas (4.17). Decisiones en los ADR 0008 a 0017.

## Modelo de amenazas (STRIDE)

Activos nuevos: el archivo exportado con todos los secretos de un workspace y su contraseña; las respuestas que la persona da a un servidor interactivo (contraseñas de un uso, códigos); las contraseñas escritas para los sitios que preguntan, retenidas en memoria de la sesión; los archivos de clave privada que se suben; el contenido de los archivos que se editan en el navegador; y las carpetas locales a las que el navegador da acceso para sincronizar.

Actores nuevos: el servidor remoto como adversario (responde listados, desafíos keyboard-interactive, respuestas FTP y mensajes que Transferly muestra); el proxy y el salto que la persona configura; el editor de un equipo, que usa los secretos pero nunca debió leerlos; y quien consigue un archivo exportado.

Límites de confianza nuevos: servidor remoto contra la interfaz (textos que llegan en vivo: registro, prompts, nombres); Transferly contra la red que hay detrás de un proxy o un salto; editor contra administrador en el acceso a secretos; archivo exportado contra quien no sabe su contraseña.

| Amenaza | Situación en la fase 4 |
|---|---|
| Suplantación | Una clave de host nueva o cambiada nunca se acepta sola: se muestra la huella y la persona decide agregar o reemplazar (ADR-0014). Los certificados FTPS se fijan por huella como una clave SSH. Un prompt interactivo pertenece a su usuario: otra cuenta recibe `prompt_not_found`. |
| Manipulación | El archivo exportado liga su cabecera (formato, versión, costo y sal) al sello AES-GCM como datos adicionales: cambiar el costo, la sal o un byte da `wrong_export_password`. Las subidas atómicas escriben a un temporal y renombran. El editor compara tamaño y fecha antes de guardar. |
| Repudio | Exportar (`sites.exported`, con el número de sitios), importar (`sites.imported`), confiar u olvidar una clave, mover sitios y los marcadores quedan en la auditoría. Las respuestas interactivas y las contraseñas escritas nunca se auditan ni se registran. |
| Fuga de información | El registro de la conexión nunca lleva contraseñas, claves, frases ni respuestas (solo el nombre del desafío). El archivo exportado no muestra ningún sitio ni secreto en claro. La API sigue sin devolver secretos; la exportación, que sí los incluye cifrados, queda para administradores y propietario (hallazgo 2). |
| Denegación de servicio | Todo cuerpo de petición tiene tope antes de leerse (hallazgo 3); un archivo de importación pide como máximo 256 MiB, 10 pasadas y 8 hilos de Argon2 y se rechaza antes de derivar; las derivaciones comparten el pool acotado del vault; como máximo 16 prompts esperan por usuario (hallazgo 5) y cuatro conexiones por usuario y sitio; la búsqueda recursiva y la sincronización tienen tope de archivos. |
| Elevación de privilegios | Exportar exige `export_secrets` (admin y propietario), importar exige gestionar sitios, y ambos pasan por `Authorize`. Una credencial FTP con saltos de línea no puede colar comandos en el inicio de sesión (hallazgo 4). |

## Ataques conocidos revisados

| Ataque | Aplica | Resultado | Verificación |
|---|---|---|---|
| XSS en el editor remoto | sí | CodeMirror pinta el contenido como texto; ninguna parte del frontend usa `dangerouslySetInnerHTML`, `innerHTML`, `eval` ni enlaces con URL dinámica (búsqueda en todo `src`); la CSP no permite scripts en línea ni de otro origen | `RemoteEditor.test.tsx`, CSP en `headers.go`, búsqueda en el código |
| XSS por textos del servidor mostrados en vivo | sí | Las entradas del registro, el nombre y la instrucción de un prompt, los nombres remotos y los mensajes guiados se pintan como texto (la instrucción con `whitespace-pre-wrap`) | `ConnectionLog.test.tsx`, `PromptDialog.test.tsx`, E2E de nombres hostiles de la fase 2 |
| Pérdida de datos en la sincronización | sí | Borrar nunca va marcado y pide confirmación; una actualización que reemplazaría un archivo que el destino cambió después ahora empieza desmarcada, lo dice y pide confirmación (hallazgo 1). Los conflictos de dos vías siguen sin detectarse (ADR-0008) | `compare.test.ts`, `plan.test.ts`, `CompareDialog.test.tsx` |
| Fuga de secretos al exportar | sí | Contraseña propia de 12 caracteres o más, Argon2id con el costo del vault, AES-256-GCM con la cabecera como datos adicionales; la clave del workspace, la DEK y los blobs sellados nunca salen; la contraseña no se guarda; solo administradores y propietario exportan (hallazgo 2) | `TestSealer`, `TestService_Export`, `TestExport_Transferly` (el archivo no muestra sitios ni secretos), `TestPhaseFour_ExportNeedsAdmin`, E2E de exportar y reabrir |
| Archivo de importación preparado para agotar el servidor | sí | Más de 4 MiB es `import_file_too_large`; un costo sobre los topes o un KDF desconocido es `invalid_export` antes de derivar; un secreto de tipo desconocido rechaza el archivo | `TestSealer_InvalidFiles`, `TestService_Import` |
| Abuso de proxies y saltos como pivote de red | sí | El host del proxy y el del salto pasan por `netguard` al guardar y al marcar (resiste DNS rebinding); lo que el proxy o el salto alcanzan más allá no se ve desde Transferly (riesgo aceptado, hallazgo 7) | `TestPolicy_*`, `TestConnector_Proxy*`, ADR-0009 |
| Secretos en el registro de la conexión | sí | Ninguna entrada lleva secretos; las respuestas interactivas tampoco; el paso `prompted` solo lleva el nombre del desafío | `TestConnector_ConnectionLog`, `TestConnector_Interactive` |
| Abuso de conexiones simultáneas desde varias pestañas | sí | Cuatro conexiones por usuario y sitio, conexiones rápidas con tope, contraseñas escritas atadas a la sesión que las dio (`Admit`), y como máximo 16 prompts esperando por usuario (hallazgo 5) | `TestConnector_AskForPassword`, `TestBroker_Ask_Ends` |
| Inyección de comandos FTP por CRLF | sí | Las rutas ya se rechazaban; ahora también el usuario, la contraseña, la cuenta y la respuesta interactiva con `\r`, `\n` o NUL (hallazgo 4) | `TestDial_Prompt` de `ftpfs` |
| Phishing desde un servidor interactivo | sí | Un servidor puede escribir cualquier pregunta; el diálogo nombra el sitio, el host y la cuenta y dice que la respuesta va solo a esa conexión (riesgo aceptado, hallazgo 9) | `PromptDialog.test.tsx` |
| Claves privadas preparadas (costos de KDF, formatos raros) | sí | Revisado en la 4.8g (ADR-0013): costos con tope, derivaciones de a dos, tipos no usados rechazados | `sshkey` y `TestService_PrivateKeys` |
| Cuerpos de petición enormes | sí | Tope de 1 MiB para todo cuerpo sin límite propio, sea cual sea el tipo declarado; las subidas siguen en flujo (hallazgo 3) | `TestPhaseFour_BodyLimits` |
| MITM al cambiar la clave de host | sí | Nunca se acepta sola; agregar o reemplazar es una decisión explícita y auditada | `TestKnownHosts_Lifecycle`, `TestKnownHosts_SeveralKeys`, E2E `host-keys.spec` |
| CSRF en los endpoints nuevos | sí | Exportar, importar, responder o rechazar un prompt, marcadores y mover sitios exigen sesión y token por el contrato OpenAPI | `TestPrompts`, `TestExport_*`, matriz de rutas protegidas |

## Dependencias

| Herramienta | Objetivo | Resultado |
|---|---|---|
| `govulncheck ./...` | módulo Go | 0 vulnerabilidades alcanzables; queda el mismo aviso de un módulo requerido sin código que lo llame |
| `pnpm audit` | frontend | sin vulnerabilidades conocidas |
| Trivy `fs` (vuln + misconfig, altas y críticas) | repositorio | 0 vulnerabilidades; un aviso de configuración en `testenv/vsftpd/Dockerfile` (hallazgo 6) |
| Trivy `image` (altas y críticas) | `transferly:test` | 0 vulnerabilidades en el sistema base y en el binario |

Librerías clave: `golang.org/x/crypto` v0.57.0 (incluye el parche de Terrapin), `pkg/sftp` v1.13.11, `jlaffaye/ftp` v0.2.4 como copia parcheada en `backend/third_party/ftp` (ADR-0015; el parche agrega ACCT y la contraseña pedida en vivo, sin tocar el resto), `go-chi/chi` v5.3.2, `jackc/pgx` v5.11.0, `coder/websocket` v1.8.15, `go-webauthn/webauthn` v0.18.2. En el frontend, librerías nuevas de la fase: CodeMirror 6, `cmdk` 1.1.1 y `@daypicker/react` 10, con guía propia y sin avisos conocidos. Ninguna tiene CVEs recientes que apliquen a las versiones usadas.

## Pruebas ofensivas realizadas

Contra el servidor en proceso y contra el stack aislado (`compose.test.yaml`, nunca contra los sitios del usuario): cuerpos JSON de 2 MiB y el mismo cuerpo declarado como texto; importaciones de Transferly con contraseña errónea, con un byte cambiado, con el costo o la sal alterados, con costos sobre los topes, con un KDF desconocido, con un secreto de tipo desconocido y con contenido que no es una exportación; exportaciones pedidas por editores, lectores y extraños de un equipo; prompts respondidos y rechazados por otra cuenta, con menos respuestas que preguntas, con respuestas de más de 1 KiB y más de 16 prompts abiertos a la vez; credenciales FTP y respuestas interactivas con `\r\n`, `\n` y NUL; sincronizaciones donde el destino es más nuevo que el origen; y búsqueda de inserción de HTML y de secretos en el registro de la conexión.

## Hallazgos

| # | Severidad | Hallazgo | Estado |
|---|---|---|---|
| 1 | Media | La sincronización marcaba por defecto una actualización que reemplazaba un archivo que el destino había cambiado después que el origen: correr el plan sin mirar perdía el cambio más reciente | Corregido: esas filas empiezan desmarcadas, dicen "más nuevo en el destino" y, si se marcan, piden la misma confirmación que un borrado; regresión en `compare.test.ts`, `plan.test.ts` y `CompareDialog.test.tsx` |
| 2 | Media | Exportar pedía solo gestionar sitios: un editor de un equipo, que usa los secretos para conectar pero la API nunca le muestra, podía sacarlos todos en claro dentro de un archivo con su propia contraseña | Corregido: acción nueva `export_secrets` para administradores y propietario; el botón se oculta a los demás roles; regresión en `TestPhaseFour_ExportNeedsAdmin` y `TestCan` |
| 3 | Media | La API no limitaba el tamaño de los cuerpos de las peticiones (anotado en la 4.8g): un cuerpo enorme ocupaba memoria y el decodificador | Corregido: `limitBodies` pone 1 MiB a todo cuerpo sin límite propio, sea cual sea el tipo declarado, con topes propios para las importaciones; las subidas siguen en flujo; regresión en `TestPhaseFour_BodyLimits` |
| 4 | Baja | Un usuario, contraseña, cuenta o respuesta interactiva con salto de línea colaba otro comando FTP durante el inicio de sesión | Corregido: `ftpfs` rechaza esos valores como credenciales rechazadas antes de enviarlos; regresión en `TestDial_Prompt` |
| 5 | Baja | Sin tope de prompts por usuario: muchas conexiones interactivas desde varias pestañas mantenían otras tantas conexiones abiertas hasta 3 minutos | Corregido: como máximo 16 prompts esperan por usuario (ADR-0016); regresión en `TestBroker_Ask_Ends` |
| 6 | Informativa | La imagen de prueba de vsftpd corre como root | Aceptado: solo existe en el stack de pruebas y vsftpd necesita root para aislar cada sesión; nunca se publica |
| 7 | Baja | Lo que un proxy o un salto alcanzan detrás de sí no se ve desde Transferly | Aceptado (ADR-0009): el proxy y el salto pasan por el guard, y llegar a una red interna exige que la persona ya confíe en un equipo que la alcanza |
| 8 | Baja | El editor detecta cambios por tamaño y fecha: dos cambios del mismo tamaño en el mismo segundo, o uno entre la comprobación y la escritura, no se notan | Aceptado: es la resolución que dan SFTP y FTP; la ventana es de milisegundos y el archivo se sube de forma atómica |
| 9 | Baja | Un servidor interactivo malicioso puede pedir cualquier cosa, por ejemplo la contraseña maestra | Aceptado: el diálogo nombra el sitio, el host y la cuenta y dice que la respuesta va solo a esa conexión; es el mismo riesgo que en FileZilla o en un cliente SSH |
| 10 | Baja | Quien tiene un archivo exportado puede probar contraseñas sin conexión | Aceptado: Argon2id con 64 MiB y 3 pasadas por intento y contraseña de 12 caracteres o más; la guía pide guardar el archivo separado de su contraseña |
| 11 | Informativa | Al exportar e importar, los secretos pasan por la memoria del servidor como cadenas de Go, que no se pueden borrar | Aceptado: mismo límite de confianza que las sesiones con el vault abierto (fases 1 y 2) |
| 12 | Informativa | La sincronización no detecta conflictos de dos vías | Aceptado (ADR-0008): no hay estado común guardado; el hallazgo 1 cubre el caso que sí se puede ver |

## Riesgos aceptados

Los hallazgos 6 a 12. Además: que la contraseña de un archivo exportado no se puede recuperar (perderla pierde el archivo); que un administrador de un equipo puede exportar sus secretos mientras es administrador (queda auditado, y es el mismo rol que gestiona los miembros); y que una transferencia que necesita una conexión nueva a un sitio interactivo falla si nadie responde a tiempo.

## Conclusión

No quedan hallazgos altos ni críticos abiertos. La revisión encontró tres fallos medios reales, todos corregidos con regresión: la sincronización que pisaba cambios más nuevos, la exportación abierta a editores y los cuerpos de petición sin límite. También dos endurecimientos menores: CRLF en las credenciales FTP y el tope de prompts por usuario. La fase 4 queda cerrada desde el punto de vista de seguridad. La fase 5 debe revisar la validación de tokens OIDC, el alcance y la revocación de los tokens de API, SSRF por webhooks, el aislamiento entre nodos (incluidos los prompts y las ceremonias de passkey, que hoy viven en memoria del proceso) y los secretos de los adaptadores en la nube.
