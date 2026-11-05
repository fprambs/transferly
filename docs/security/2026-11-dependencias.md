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
