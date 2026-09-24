# Revisión de seguridad: fase 0 (en curso)

Fecha de corte: 2026-09-24. Alcance construido hasta ahora: contenedor `tools`, esqueleto del backend (configuración, router, `/healthz`, `/readyz`, errores RFC 9457, migraciones, apagado ordenado, reloj UTC) y esqueleto del frontend (Vite, Tailwind, shadcn, i18n, fechas, `Transport`, guards de documentación).

## Dependencias

- `govulncheck ./...`: sin vulnerabilidades.
- `pnpm audit --prod` del frontend: sin vulnerabilidades conocidas.
- Pendiente al cerrar la fase: Trivy sobre las imágenes.

## Hallazgos

| # | Severidad | Hallazgo | Estado |
|---|---|---|---|
| 1 | Media | `middleware.RealIP` de chi permite suplantar la IP del cliente con `X-Forwarded-For` (staticcheck SA1019, avisos GHSA-3fxj-6jh8-hvhx, GHSA-rjr7-jggh-pgcp, GHSA-9g5q-2w5x-hmxf) | Corregido: se eliminó. Volverá con una lista de proxies de confianza |
| 2 | Baja | Riesgo: `readyz` podría exponer el texto del error de la dependencia (hosts, credenciales) | Mitigado desde el diseño y cubierto por `TestReadyz_DoesNotLeakInternalErrors` |
| 3 | Baja | El apagado ordenado cancelaba las peticiones en curso por usar el contexto de arranque como contexto base | Corregido: se eliminó `BaseContext` |
| 4 | Informativa | `shadcn init` agregó la dependencia `cn` (mantenedor `shadcn`, repositorio `shadcn-ui/cn`), publicada el 2026-09-22, dos días antes de adoptarla | Aceptada: es el reemplazo oficial de `clsx` y `tailwind-merge`; versión fijada por lockfile; revisar antes de actualizar |
| 5 | Baja | pnpm creó su store dentro del repositorio al ignorar `npm_config_store_dir` (pnpm 12 usa `pnpm_config_store_dir`) | Corregido en `tools.Dockerfile`; el store vive en el volumen de caché |
| 6 | Media | Las descargas de paquetes por HTTP plano desde `deb.debian.org` llegaron alteradas (`Hash Sum mismatch`) en la red de desarrollo: algo intermedio modifica el tráfico | Mitigado: la imagen de prueba usa repositorios HTTPS; apt verifica además las firmas. Conviene revisar la red o el proxy |
| 7 | Informativa | Servidores de prueba con credenciales fijas (`sentinel`/`sentinel`) y certificado autofirmado | Aceptado: solo en la red interna de Compose, sin puertos publicados al host |
| 8 | Informativa | La imagen `atmoz/sftp:alpine` se fija por etiqueta, no por digest | Pendiente al cerrar la fase: fijar por digest |
| 9 | Baja | La contraseña de Postgres de `compose.yaml` es `sentinel` por defecto (`SENTINEL_DB_PASSWORD`) | Aceptado: la base solo es alcanzable en la red interna de Compose y no publica puertos; la guía indica fijar otra en producción |
| 10 | Informativa | Imagen de producción verificada: usuario 65532, sistema de archivos de solo lectura, `cap_drop: ALL`, `no-new-privileges`, cabeceras OWASP, clave de instancia `0600` | Cumplido |
| 11 | Informativa | Postgres de desarrollo publica el puerto 5432 en `127.0.0.1` con credenciales por defecto | Aceptado: solo desarrollo y solo loopback; producción no publica el puerto |

## Pendiente para cerrar la fase 0

- Escaneo Trivy de las imágenes y revisión de las imágenes base.
- Fijar por digest `atmoz/sftp` y las imágenes de compilación.
