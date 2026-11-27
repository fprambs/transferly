# Revisión de seguridad: dependencias, noviembre de 2026

5 de noviembre de 2026 · Alcance: dependencias de Go y npm, imágenes Docker de producción, desarrollo y pruebas. No revisa el código propio.

Se repite con `make audit` (`govulncheck`, `pnpm audit`, gitleaks y Trivy sobre la imagen de producción).

## Hallazgos

| Severidad | Componente | Problema | Estado | Regresión |
|---|---|---|---|---|
| Alta | Go 1.27.2, `crypto/x509` | CVE-2026-111943: confía en 8 intermedios de eMudhra cuyas claves se filtraron; la imagen confía en 4 raíces emSign, así que aplica a las conexiones TLS validadas por CA | Corregido con Go 1.27.3; requiere publicar una imagen nueva | `govulncheck` |
| Media | go-chi 5.3.2 | CVE-2026-114437: quien llega directo al puerto 8080 elige su IP con `X-Forwarded-For` y evade el límite de intentos de login. Comprobado: con 5.3.2, 0 de 8 intentos limitados | Corregido con chi 5.5.0 | `TestClientAddress` |
| Media | postal-mime 3.0.0 | GHSA-f2hq-32vh-53cc: un `.eml` hostil congela la página; medido en 15,9 s con 1,7 MB. Sin fuga de datos | Corregido con 4.2.0 | `mail.test.ts` |
| Media | Caddy 2.11.4 | Compilado con Go 1.26.3: avisos altos en TLS y HTTP/2 en la puerta de entrada del perfil `https` | Corregido con 2.11.8; ver riesgos aceptados | Trivy |
| Baja | Copia de `pkg/sftp` 1.13.11 | Sin CVE: la última escritura rechazada o una fuente cortada se informaban como éxito. De punta a punta, una subida cortada no pierde datos: el contexto de la petición la detiene antes de reemplazar el archivo | Corregido: se elimina la copia y se usa 1.13.12 | `TestWrite_ReportsWhatDidNotArrive`, `TestFS_LyingServer` |

## Avisos que no aplican

| Aviso | Motivo |
|---|---|
| DOMPurify GHSA-cg2g-xh64-v4q7 (crítico), GHSA-mrv2-3pqr-j5jr, GHSA-c6gr-qvv4-5v3f | Requieren `SAFE_FOR_TEMPLATES`, `IN_PLACE` o `PARSER_MEDIA_TYPE`, que no se usan. Actualizado igual a 3.4.17 |
| seroval, fast-uri | No entran al bundle de producción, comprobado con sourcemaps. Actualizados igual |
| zod GHSA-mrhx-c7xc-m3w5 | Requiere esquemas de arrays sin límite; no hay ninguno. Actualizado igual |
| `x/net` (`http2`, `html`), `x/crypto/openpgp` | Esos paquetes no están en el binario de producción |
| `pkg/sftp` GHSA-xgcg-wgmj-5rw3 | `FS.RemoveAll` recorre el árbol con `Lstat` y no usa `Client.RemoveAll` |
| `pkg/sftp` GHSA-47hq-m6q9-3rm6, GHSA-h959-g3h6-w76c | Solo los servidores de prueba usan la API de servidor |
| vite, vitest, undici | Solo de desarrollo, con el puerto en `127.0.0.1`. Actualizados igual |
| xlsx CVE-2023-30533, CVE-2024-22363 | Falso positivo de OSV: 0.20.3 ya incluye los arreglos |

## Resultado (Trivy, severidad alta y crítica)

| Imagen | Uso | Antes | Después |
|---|---|---|---|
| `transferly` | Producción | 2 altas | 0 |
| `caddy` | Producción | 1 crítica, 28 altas | 2 altas |
| `postgres:17` | Producción | 16 críticas, 93 altas | 4 críticas, 72 altas |
| `atmoz/sftp`, `versitygw` | Pruebas | 7 altas en `versitygw` | 0 |
| `playwright` | Pruebas | 17 altas | 10 altas |

Conteo final: `govulncheck` sin vulnerabilidades alcanzables, `pnpm audit` limpio, gitleaks sin filtraciones, 1298 tests unitarios y 163 de navegador, dos veces seguidas y sin reintentos.

## Riesgos aceptados

- **Caddy:** 2.11.9 salió el 5 de noviembre y aún no está en Docker Hub. Quedan CVE-2026-116587 en go-jose, una denegación de servicio con claves RSA manipuladas. Subir a 2.11.9 cuando se publique.
- **Postgres en Debian 12:** cambiar el sistema bajo una base con datos puede alterar sus collations y exige un reindexado planificado. Sus avisos están en paquetes que el servidor no expone y en `gosu`, que solo corre al arrancar (fase 0, hallazgo 12). No publica puertos.
- **Azurite (2 críticas), Toxiproxy, Playwright y servidores de prueba:** solo existen en pruebas y desarrollo, en su red interna. Son las últimas versiones publicadas.
- **Imagen de desarrollo:** `sqlc`, `oapi-codegen` y `air` están en su última versión y solo procesan archivos propios. Los paquetes de Debian no tienen parche publicado.
- **TypeScript 6.0 y `@types/node` 24:** TypeScript 7 quita la API del compilador que usan `openapi-typescript` y `guards/docs-rules.ts`, y `@types/node` sigue a Node 24 LTS. No tienen avisos.
- **xlsx CVE-2026-38358 y CVE-2026-38359:** sin registro en NVD y sin aclarar si afectan a 0.20.3, la última publicada. Un archivo con 200.000 bloques CDATA no la hizo fallar, y corre en la página aislada con un límite de 10 MB.

## Otros arreglos

- La frase de una clave privada ya no desaparece mientras el servidor la valida (`KeyFileField`).
- Los botones de cuenta y espacio de trabajo incluyen en su nombre accesible lo que muestran (WCAG 2.5.3).

## Actualización del 13 de noviembre

Go 1.27.4 (10 de noviembre) corrige GO-2026-6859 / CVE-2026-97054: una petición que supera el límite de `MaxBytesReader` podía tumbar el servidor HTTP/1. `govulncheck` encontraba la ruta desde nuestro código, que limita los cuerpos a 1 MiB, así que la imagen `v1.0.0-rc.2` quedó afectada. Se publica `v1.0.0-rc.3` con Go 1.27.4 y Caddy 2.11.9, que ya está en Docker Hub. Caddy 2.11.9 actualiza go-jose v4 pero sigue trayendo go-jose v3, sin parche para CVE-2026-116587 (denegación de servicio con claves RSA manipuladas, en la emisión de certificados ACME), y CVE-2026-78661 en `x/net/html`, que Caddy usa en sus plantillas; ambos quedan como riesgo aceptado hasta una versión de Caddy que los resuelva. La imagen de Transferly queda sin hallazgos altos ni críticos.

## Actualización del 27 de noviembre

El perfil HTTPS compila Caddy 2.11.9 con la revisión de Smallstep integrada en [Caddy #8368](https://github.com/caddyserver/caddy/pull/8368), que elimina go-jose v3, y con `golang.org/x/net` 0.62.0. Trivy confirmó cero hallazgos altos o críticos en la imagen resultante. La prueba aislada verificó TLS con la CA local, autenticación HTTPS, atributos de cookies, HSTS y rechazo de un origen ajeno. La emisión y renovación con una CA pública quedan pendientes de comprobar en un despliegue con dominio.

PostgreSQL se mantiene en 17.12 sobre Bookworm. La imagen derivada instala PAM `1.5.2-6+deb12u3` o posterior y recompila `gosu` 1.19 con Go 1.27.4. Trivy pasó de 4 hallazgos críticos y 141 altos a 3 críticos y 112 altos, contados por paquete y binario. Desaparecieron los cuatro hallazgos de PAM y los 26 de la biblioteca estándar de Go en `gosu`. [Aviso de PAM en Debian](https://security-tracker.debian.org/tracker/CVE-2025-6020)

Trivy no indica una versión corregida en Bookworm para los hallazgos restantes. Debian confirma que los avisos de SQLite y libxml2 siguen abiertos en Bookworm. El tercer hallazgo crítico corresponde a MiniZip, que Debian no compila dentro de `zlib1g` para Bookworm, y el informe de la imagen no incluye un paquete MiniZip instalado. [SQLite](https://security-tracker.debian.org/tracker/CVE-2025-7458) · [libxml2](https://security-tracker.debian.org/tracker/CVE-2026-6653) · [MiniZip](https://security-tracker.debian.org/tracker/CVE-2023-45853)

En pruebas aisladas se verificaron la inicialización desde cero, el arranque de Transferly con un volumen creado por la imagen anterior, la conservación de los datos y de la versión de collation 2.36, y la restauración de un respaldo en la instancia nueva.

