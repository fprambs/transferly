# Revisión de seguridad: fase 0 (cerrada)

Fecha: 2026-09-24. Proceso: [README](README.md).

Alcance: imagen de herramientas, esqueleto del backend (configuración, router chi generado desde OpenAPI, sondas de salud, errores RFC 9457, migraciones con candado, apagado ordenado, reloj UTC, servidor de la SPA embebida con cabeceras de seguridad, clave de instancia, `healthcheck`), puerto `RemoteFS` con su suite, esqueleto del frontend, imagen y stack de producción, entorno de pruebas, spikes de protocolos y CI.

## Modelo de amenazas (STRIDE)

Activos: el proceso HTTP y sus datos, la clave de instancia, las imágenes de contenedor y el pipeline de CI. Límites de confianza: navegador o internet contra el contenedor `app`; `app` contra Postgres (red interna de Compose); el pipeline contra el registro; el desarrollo contra sus dependencias.

| Amenaza | Situación en la fase 0 |
|---|---|
| Suplantación | Aún no hay autenticación (fase 1). La IP del cliente no se usa: se eliminó `RealIP`, que era suplantable. |
| Manipulación | Imágenes de ejecución fijadas por digest, lockfiles de Go y pnpm, acciones de CI fijadas por SHA, migraciones embebidas en el binario. |
| Repudio | Logs estructurados con id de petición; la auditoría llega en la fase 1. |
| Fuga de información | Las sondas y errores no exponen detalles internos (con test); sin cabecera `Server`; CSP estricta; la clave de instancia es `0600` y nunca se registra. |
| Denegación de servicio | `ReadHeaderTimeout` de 10 s verificado; cabeceras enormes rechazadas; el servidor sigue respondiendo con 100 conexiones inactivas. |
| Elevación de privilegios | Usuario 65532, sistema de archivos de solo lectura, `cap_drop: ALL`, `no-new-privileges`; base de datos sin puertos publicados. |

## Ataques conocidos revisados

| Ataque | Aplica | Resultado |
|---|---|---|
| Path traversal en el servidor estático | sí | Sin fuga en 12 variantes (planas, codificadas, doble codificación, `....//`, `\`, `%00`, dotfiles); tests unitarios y pruebas en vivo |
| Slowloris | sí | El servidor cierra la conexión incompleta a los 10 s |
| Cabeceras y URLs enormes | sí | Cabecera de 1,5 MiB rechazada; el servidor sigue sano |
| Métodos no previstos (`TRACE`, `PUT`, `DELETE`, `PATCH`, `OPTIONS`) | sí | 404, sin manejadores |
| Suplantación de IP por `X-Forwarded-For` | sí | `RealIP` eliminado; se usará `ClientIPFrom*` con lista de proxies de confianza |
| XSS | parcial | CSP `script-src 'self'` sin `unsafe-inline`; sin `innerHTML`; los nombres remotos llegan en la fase 1 |
| Inyección SQL | sí | Consultas parametrizadas con sqlc; el único identificador dinámico, en los tests, pasa por `pgx.Identifier.Sanitize` |
| Migraciones concurrentes | sí | Candado asesor de Postgres; probado con 6 ejecuciones simultáneas |
| Cadena de suministro | sí | SHA en acciones, digest en imágenes, `govulncheck`, `pnpm audit`, Trivy |
| Escape o abuso del contenedor | sí | Endurecimiento verificado con `docker inspect` |
| Fuga de secretos (logs, respuestas, volcados) | sí | La clave se crea `0600` y el log solo dice su origen |
| SSRF, FTP bounce, Zip Slip, CSRF, CSWSH, sesiones, IDOR, Terrapin | no todavía | Se revisan al cerrar las fases 1 y 2 |

## Dependencias

| Herramienta | Objetivo | Resultado |
|---|---|---|
| `govulncheck ./...` | módulo Go | sin vulnerabilidades |
| `pnpm audit --prod` | frontend | sin vulnerabilidades conocidas |
| Trivy `image` | `transferly:local` (sistema y binario Go) | 0 altas o críticas |
| Trivy `config` | Dockerfiles, Compose y workflow | 0, con una excepción documentada (vsftpd) |
| Trivy `image` | `postgres:17-bookworm` (upstream) | 22 en `gosu` (Go 1.24.6), ver hallazgo 12 |
| Trivy `image` | `caddy` (upstream, perfil opcional) | 17 en `x/crypto` 0.52, ver hallazgo 13 |

Librerías clave: `x/crypto` v0.57.0 incluye la corrección de Terrapin (CVE-2023-48795, corregida desde v0.17.0); chi se usa sin `RealIP` (avisos GHSA-3fxj-6jh8-hvhx, GHSA-rjr7-jggh-pgcp, GHSA-9g5q-2w5x-hmxf). No se revisaron avisos específicos de `jlaffaye/ftp` más allá de `govulncheck`.

## Pruebas ofensivas realizadas

Contra la imagen de producción en ejecución: 12 variantes de recorrido de rutas, 5 métodos no previstos, URL de 16 KiB, cabecera de 1,5 MiB, conexión slowloris, 100 conexiones inactivas simultáneas y comprobación de que Postgres no es alcanzable desde el host. Los tests del handler estático (`webui_test.go`) fijan las variantes de traversal como regresión.

## Hallazgos

| # | Severidad | Hallazgo | Estado |
|---|---|---|---|
| 1 | Media | `middleware.RealIP` de chi permite suplantar la IP del cliente | Corregido: eliminado |
| 2 | Baja | Riesgo de que `readyz` exponga el error de la dependencia | Mitigado y cubierto por `TestReadyz_DoesNotLeakInternalErrors` |
| 3 | Baja | El apagado ordenado cancelaba las peticiones en curso (contexto base) | Corregido: se eliminó `BaseContext` |
| 4 | Informativa | Dependencia `cn` agregada por `shadcn init`, publicada 2 días antes | Aceptada: es del mismo mantenedor y reemplaza a `clsx` y `tailwind-merge`; versión fijada por lockfile |
| 5 | Baja | pnpm creó su store dentro del repositorio | Corregido en `tools.Dockerfile` |
| 6 | Media | Descargas de paquetes por HTTP llegaron alteradas (`Hash Sum mismatch`) en la red de desarrollo | Mitigado: repositorios HTTPS en la imagen de prueba; conviene revisar la red o el proxy |
| 7 | Informativa | Servidores de prueba con credenciales fijas y certificado autofirmado | Aceptado: red interna, sin puertos publicados |
| 8 | Baja | Contraseña de Postgres por defecto `transferly` en `compose.yaml` | Aceptado: la base solo es alcanzable en la red interna; la guía indica fijar otra en producción |
| 9 | Informativa | Postgres de desarrollo publica `127.0.0.1:5432` con credenciales por defecto | Aceptado: solo desarrollo y solo loopback |
| 10 | Informativa | Imagen de producción endurecida y verificada | Cumplido |
| 11 | Alta (Trivy DS-0002) | `tools.Dockerfile` corría como root | Corregido: `USER node` (Compose lo sobreescribe con el UID del host) |
| 12 | Baja | `postgres:17-bookworm` incluye `gosu` con CVE de la biblioteca estándar de Go | Aceptado: código de upstream que solo se ejecuta al arrancar y no usa TLS; imagen fijada por digest y actualizada con Renovate |
| 13 | Baja | `caddy` incluye `x/crypto` 0.52 con CVE en `ssh` | Aceptado: perfil opcional; Caddy no expone SSH; imagen fijada por digest |
| 14 | Informativa | El servidor FTP de prueba corre como root (Trivy DS-0002) | Aceptado con excepción acotada en `.trivyignore.yaml`: vsftpd necesita root para puertos privilegiados, chroot y PAM |
| 15 | Media | Migraciones concurrentes entre instancias | Corregido: candado asesor de Postgres con test |
| 16 | Informativa | La detección de zona horaria de FTP por `MDTM` no es fiable (vsftpd devuelve hora local) | Documentado en el spike; se detecta con el reloj del cliente |

## Riesgos aceptados y trabajo para la fase 1

- Configurar Renovate o Dependabot para imágenes y dependencias (requiere el repositorio remoto).
- Proteger la rama `main` con PR y CI en verde (ajuste del repositorio remoto).
- HSTS lo aplica Caddy (perfil `https`); documentado.
- Limitación de tasa y lista de proxies de confianza llegan con la autenticación.

## Conclusión

Sin hallazgos altos o críticos abiertos. Los abiertos son de severidad baja o informativa, con justificación. Fase 0 cerrada desde el punto de vista de seguridad.
