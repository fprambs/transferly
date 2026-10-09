# Validación del candidato local

Base revisada: `ba2e4db` / frontend 0.9.0. Validación terminada el 9 de octubre de 2026 sobre los cambios locales del [plan de mitigación](2026-10-08-mitigation-plan.md). No se ha ejecutado CI remoto ni publicado una versión.

## Calidad y comportamiento

| Gate | Resultado |
| --- | --- |
| Go completo, `-race -count=1` | Pasa; 100 % en todas las funciones incluidas por el gate oficial. |
| Frontend completo | 1.293 pruebas pasan; statements/branches/functions/lines al 100 %. |
| Types y lint frontend | Pasan sin errores ni avisos. |
| golangci-lint y `go mod tidy -diff` | Pasan; sin cambios de dependencias pendientes. |
| Parser 7z acotado | Tests del módulo parcheado y archivos normales/cifrados pasan; parche/licencia documentados. |
| Navegador, primera ejecución final | 163 pasan; 19 capturas de documentación omitidas por configuración; cero fallos/retries. |
| Navegador, segunda ejecución final | 163 pasan; mismas 19 omisiones; cero fallos/retries. |
| Pestañas | 20 repeticiones pasan, sin retries. |
| Revocación SSH | 20 repeticiones del lector bloqueado pasan; expiración durante apertura e idle probadas. |
| Copias y cancelación | Reservas ordenadas, cancelación y liberación probadas; 20 repeticiones. Límite de una conexión con código específico. |
| Integridad de subida | Staging por transferencia, alias concurrentes, preservación del original ante commit fallido y extensión SFTP probados. FTP conserva el original si no ofrece reemplazo seguro. |
| Rutas y archivos anidados | Rutas remotas absolutas y nombres virtuales relativos tienen validaciones separadas; previews anidados pasan. |
| Imports diferidos | Error contenido dentro del feature; shell/dialog conservados; recarga recupera el chunk en navegador. |

## Seguridad y operación

- Gitleaks: 614 commits, 16,73 MB de historial; sin filtraciones detectadas tras revisar los fixtures y emuladores. El OpenAPI comprimido tiene una excepción restringida por ruta y literales base64 consecutivos; asignaciones en ese archivo y los mismos fragmentos en el YAML original siguen siendo detectados, comprobado con dos controles sintéticos.
- `pnpm audit`, incluyendo herramientas de build: sin vulnerabilidades conocidas.
- `govulncheck`: cero vulnerabilidades llamadas o importadas; un aviso en un módulo requerido sin uso vulnerable.
- Trivy de la imagen final: cero vulnerabilidades HIGH/CRITICAL corregibles. Trivy configuración: cero hallazgos HIGH/CRITICAL pendientes. Las únicas excepciones de root son servidores de pruebas justificados.
- Backup/restauración en frío: base PostgreSQL y clave de instancia recuperadas; login, vault, clave confiada y lectura SFTP mediante credencial cifrada verificados. Restauración conserva UID/GID 65532 y permisos de la clave.
- Actualización y rollback desde la revisión base 0.9.0: cuenta, vault, credencial, bytes y temporal anterior conservados. El candidato rechaza adoptar automáticamente el temporal antiguo; la UI explica la recuperación mediante una nueva subida.
- Retención SQL: preview no borra; aplicación por lotes preserva uploads recuperables, copias no resueltas y transferencias activas. No se instala un job automático ni se eliminan archivos remotos sin propiedad comprobada.
- HTTPS/Caddy aislado: certificado validado, setup/login, cookies Secure/HttpOnly/SameSite=Lax, HSTS y rechazo de origen ajeno verificados. Proxies IP se normalizan a CIDR; el origen público debe coincidir con el dominio HTTPS.

## Rendimiento reproducible

Se alternaron dos veces 0.9.0 y el candidato, con el mismo servidor SFTP, archivo aleatorio de 64 MiB, cliente y datos. Cada fase descarta calentamiento y mide diez descargas API, diez descargas OpenSSH y diez lotes de veinte listados. Cada descarga API se compara por SHA-256 con OpenSSH. No había suites ni escáneres en ejecución durante estas mediciones.

| Fase | OpenSSH MiB/s | API MiB/s | Mediana de lotes de listado, ms/request |
| --- | ---: | ---: | ---: |
| 0.9.0, primera | 69,95 | 141,59 | 3,62 |
| Candidato, primera | 76,54 | 157,73 | 3,59 |
| 0.9.0, segunda | 77,58 | 159,52 | 3,81 |
| Candidato, segunda | 74,76 | 152,07 | 3,64 |

La mediana combinada de las veinte descargas API es **152,25 → 155,93 MiB/s**, +2,41 %. El promedio de las dos medianas de lotes de listado es **3,715 → 3,615 ms**, −2,69 %. No se observa la regresión máxima del 10 % definida en el plan en este escenario local. Estas cifras no prueban carga multiusuario ni latencia WAN; se conservan muestras y hashes verificados fuera del repositorio.

JavaScript inicial completo servido por las imágenes, gzip nivel 6: **830.385 → 487.824 bytes**, reducción del **41,25 %**. El gate del build mide 488.302 bytes en 59 archivos, bajo el presupuesto de 500.000; las pequeñas diferencias corresponden al método de compresión. Editor, terminal y visores costosos quedan diferidos.

## Límites del resultado

El candidato conserva soporte de una sola instancia. Locks y propiedad de staging no coordinan réplicas ni otros clientes remotos; no resuelven symlinks o dos configuraciones que apuntan al mismo archivo físico. Reemplazo FTP sin garantía falla deliberadamente; operaciones ambiguas se verifican antes de repetirse. La reanudación de conexiones rápidas depende de conservar su fuente y conexión temporal en la página.

Los fallos de escritura final en DB tienen diagnóstico y hasta tres intentos dentro de un timeout compartido; una indisponibilidad persistente requiere reconciliación de restart y verificar los bytes remotos. La retención y la limpieza de temporales son decisiones del operador.

Las correcciones compartidas se extrajeron en coordinador de estado privado, recorrido acotado, límite Office, reservas de copia y boundary de carga/error. Las descargas de Explorer y el movimiento del árbol de SitesSidebar se extraen en `useRemoteDownloads` y `useSiteTreeDrag`, con sus estados y reconciliación; los componentes conservan navegación, selección y rendering. La extracción conserva el 100 % de cobertura y pasa las 33 regresiones de navegador afectadas.

Para publicar aún corresponde ejecutar CI sobre el commit definitivo, habilitar/verificar reporte privado de seguridad y protecciones del repositorio, elegir la versión de release y aprobar la publicación. No se ha cambiado visibilidad ni enviado código a servicios externos.

## Separación de controladores

`useRemoteDownloads` concentra descargas simples, ZIP y directorios por archivo con su progreso/error. `useSiteTreeDrag` concentra validación de destinos, movimiento de sitios/colecciones, feedback y reconciliación de caché. Explorer y SitesSidebar conservan presentación, selección y diálogos. Las 169 regresiones existentes de explorador y sidebar pasan después de la extracción; tipos y lint pasan. La cobertura completa termina con 1.293 pruebas aprobadas y 100 % en las cuatro métricas; las 33 regresiones de navegador afectadas pasan sin retries.

Tras la extracción, las **33 regresiones de navegador afectadas** pasan sin retries: descargas, operaciones del explorador, gestión de sitios, sync, pestañas, subida de carpetas y recuperación de chunks. Se añadió al test existente de arrastre la transición entre filas, que conserva el destino; salir del árbol lo limpia.

Cierre de la extracción: tipos y lint sin observaciones, `git diff --check` limpio, 1.293 pruebas frontend al 100 % y grafo inicial de **488.302 bytes gzip**. Las dos ejecuciones completas de navegador y veinte repeticiones preceden esta extracción sin cambios en el API; las 33 pruebas afectadas la revalidan.

Estado final local: **todos los gates pasan**. La publicación y la verificación de CI/configuración de GitHub se mantienen como tareas externas pendientes; el árbol de trabajo contiene las mitigaciones y su documentación.


## Revalidación tras los cambios SSH y timeout

El gate se repitió sobre el workspace completo, incluida la negociación SSH segura y la detención del ciclo de conexión ante timeout. Se completaron las regresiones de firmantes sin restricciones, claves obsoletas rechazadas antes de abrir la red, anuncios remotos acotados y diagnósticos incompletos en la interfaz. También se corrigieron los comentarios para cumplir el guard de documentación, sin cambiar umbrales ni exclusiones.

| Gate | Resultado actual |
| --- | --- |
| Go oficial completo, detector de carreras y cobertura | Pasa; 100 % por función. |
| Frontend completo | 1.296 pruebas pasan; 100 % en sentencias, ramas, funciones y líneas. |
| Tipos y lint frontend | Pasan; cero errores/avisos de lint. |
| Go lint y `go mod tidy -diff` | Pasan; cero problemas y sin diferencias de módulos. |
| Contratos OpenAPI y consultas regenerados | Sin cambios de contenido respecto al workspace. |
| Build frontend y presupuesto | Pasan: 489.273 bytes gzip en 59 archivos; límite 500.000. |
| Imagen y fixtures de todos los protocolos | Reconstruidos y saludables. |
| Navegador sobre la imagen reconstruida | 163 pasan en 4 minutos; 19 capturas deliberadamente omitidas; cero fallos y cero retries. |

Registros de esta ronda: `/tmp/transferly-candidate-go-gate.log`, `/tmp/transferly-candidate-lint.log`, `/tmp/transferly-candidate-frontend-final.log`, `/tmp/transferly-candidate-generated.log`, `/tmp/transferly-candidate-image.log` y `/tmp/transferly-candidate-browser.log`.

Las [notas de 1.0.0-rc.1](../releases/1.0.0-rc.1.md) son un borrador. La versión del workspace continúa en 0.9.0 y la imagen de prueba informa `dev`; no se creó un commit de release ni tag. La evidencia corresponde al workspace, no a un commit definitivo. Las auditorías de seguridad, HTTPS, restore, upgrade y benchmarks anteriores no se volvieron a ejecutar en esta ronda; los cambios no alteran sus dependencias ni configuración. CI remoto y publicación siguen pendientes.


La revalidación local de esta ronda terminó correctamente. Los registros de Go, frontend y navegador finalizan con código de salida 0. Los siguientes pasos son fijar la versión/commit del candidato, validar ese commit en CI remoto y verificar la configuración del repositorio antes de publicar.
