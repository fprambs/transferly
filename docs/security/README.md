# Revisión de seguridad por fase

Al cerrar **cada fase** del [roadmap](../ROADMAP.md) se analiza la seguridad de lo construido. Una fase no se da por cerrada con hallazgos altos o críticos abiertos. El informe se guarda como `docs/security/fase-N.md`.

## Proceso

1. **Modelo de amenazas (STRIDE)** de lo nuevo: activos, límites de confianza, flujos de datos y actores.
2. **Checklist OWASP** aplicable (Top 10 y ASVS): autenticación, sesiones, control de acceso, validación de entrada, criptografía, errores y logs, configuración.
3. **Catálogo de ataques conocidos** (abajo): marcar cuáles aplican a la fase y confirmar que existe un test que los cubre.
4. **Dependencias**: `govulncheck`, `pnpm audit`, escaneo de imagen con Trivy; búsqueda de CVEs recientes de las librerías clave (`x/crypto/ssh`, `pkg/sftp`, `jlaffaye/ftp`, `chi`, `pgx`, React y otras usadas en la fase).
5. **Pruebas ofensivas automatizadas**: fuzzing de parsers y rutas, casos maliciosos de entrada, matriz de autorización.
6. **Hallazgos**: cada uno con severidad, descripción, estado (corregido, aceptado con justificación) y un test de regresión si se corrigió.
7. **Cierre**: informe firmado en el repositorio y referenciado desde el roadmap.

## Plantilla del informe

```
# Revisión de seguridad: fase N
Fecha · Alcance (qué se construyó)
## Modelo de amenazas
## Ataques conocidos revisados (tabla: ataque, aplica, mitigación, test)
## Dependencias (resultado de govulncheck, pnpm audit, Trivy; CVEs revisados)
## Hallazgos (severidad, estado, test de regresión)
## Riesgos aceptados
## Conclusión
```

## Catálogo de ataques conocidos

| Ataque | Dónde aplica | Mitigación | Verificación |
|---|---|---|---|
| SSRF y DNS rebinding | conexión a hosts arbitrarios | `netguard` valida la IP resuelta al conectar | tests de loopback, link-local, metadata y rebinding |
| FTP bounce (PORT/EPRT hacia terceros) | FTP en modo activo | pasivo por defecto; activo solo opt-in y hacia la IP del cliente | test con servidor malicioso |
| Secuestro de IP en respuesta PASV | FTP pasivo | ignorar la IP anunciada y usar la del canal de control | test con servidor malicioso |
| Inyección de comandos FTP por CRLF | rutas y nombres | rechazar `\r` y `\n` en rutas | fuzzing de rutas |
| Path traversal y Zip Slip | descargas ZIP, subida de carpetas, sincronización | normalizar y validar rutas contra la raíz; rechazar `..` y rutas absolutas | fixtures maliciosos |
| Symlinks maliciosos | descargas y sincronización recursiva | no seguir symlinks por defecto | tests con enlaces |
| Zip bomb y agotamiento de recursos | ZIP en streaming, listados enormes | límites de tamaño, cantidad y tiempo | tests de carga |
| XSS por nombres remotos y errores | listados, mensajes | renderizar siempre como texto; CSP estricta | E2E con `<img onerror>` en nombres |
| CSRF | mutaciones REST | token de doble envío y `SameSite` | tests de rechazo sin token |
| CSWSH (secuestro de WebSocket entre sitios) | `/api/v1/ws` | validar `Origin` y autenticación | test con origen ajeno |
| Fijación y robo de sesión | login | rotar el id al autenticar, cookies `HttpOnly`/`Secure` | tests de sesión |
| Fuerza bruta, credential stuffing, enumeración de usuarios | login, unlock, TOTP | rate limit, respuestas y tiempos uniformes | tests de límite y de timing |
| DoS mediante Argon2 (unlock masivo) | vault | rate limit y concurrencia acotada del KDF | prueba de carga |
| Reutilización de nonce en AES-GCM | vault | nonce aleatorio de 96 bits por registro y rotación de DEK | tests de unicidad |
| Fuga de secretos (logs, errores, respuestas, volcados) | todo el sistema | redacción de logs, la API nunca devuelve secretos | tests que buscan secretos en logs y respuestas |
| IDOR y acceso entre workspaces | API | filtro por `workspace_id` y `authz.Can` | matriz de autorización |
| Terrapin (CVE-2023-48795) | SSH/SFTP | `x/crypto` con el parche (v0.17.0 o posterior) | `govulncheck` |
| MITM por confianza en el primer uso | host keys SSH | mostrar el fingerprint, bloquear si cambia | tests de cambio de clave |
| Algoritmos débiles | SSH y TLS | lista de cifrados moderna, TLS 1.2 o superior | tests de negociación |
| Sniffing de contenido y descargas activas | descargas | `Content-Disposition: attachment` y `nosniff` | tests de cabeceras |
| Inyección SQL | store | consultas parametrizadas con sqlc; nunca concatenar | revisión y linters |
| Suplantación de IP por `X-Forwarded-For` | rate limit y auditoría | lista de proxies de confianza; sin `RealIP` de chi | test con cabeceras falsas |
| Cadena de suministro | dependencias e imágenes | lockfiles, imágenes fijadas por digest, `govulncheck`, `pnpm audit`, Trivy | CI |
| Escape o abuso del contenedor | despliegue | sin root, `cap_drop: ALL`, filesystem de solo lectura | escaneo de configuración |
